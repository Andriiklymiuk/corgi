---
name: mobile
description: Use when verifying a mobile (Expo / React Native) change on a real device — "test on the emulator", "run it on the simulator", "screenshot the app", "drive it with Maestro", "does this screen render", "check it on Android/iOS", "tap through the app", "is the animation right" — or when a local iOS/Android build + TestFlight/Play ship needs driving. Covers the device-driving loop (deep links, Maestro flows, screenshots, sips crops) AND the gotchas that actually bite — Maestro ASCII-only input, non-login-shell pod builds, Metro `--clear` redbox desync, native-needs-rebuild, SceneKit magenta-at-runtime, square particles. NOT for writing the app code (normal edits) or authoring corgi-compose (corgi skill).
---

# Verify mobile change on device

## Overview
Change NOT done till you DROVE on device + READ screenshot. Green build still render
magenta. Pick surface → navigate → drive+assert Maestro → screenshot → look. Evidence
before "works".

## Pick surface first
- **JS / TS / Skia / RN styles / shaders** → hot-reload over Metro. Use **Android
  emulator** — fastest, no rebuild. Edit, save, live.
- **Native (Swift / Kotlin / SceneKit / new native dep / config plugin)** → NO hot-reload.
  Rebuild (`expo run:ios` / `expo run:android`) to see. JS reload won't.
- One Metro serves both Android + iOS; `expo run:*` reuses a running one.

## Drive loop
1. **Navigate** — deep link beats menu-tapping:
   - Android `adb shell am start -a android.intent.action.VIEW -d "<scheme>://<route>" <pkg>`
   - iOS `xcrun simctl openurl booted "<scheme>://<route>"`
   - or Maestro `scrollUntilVisible` + `tapOn`.
2. **Drive+assert Maestro** — flow MUST be a FILE (no stdin `-`). Two devices attached
   (emulator + sim) → pass `--device <udid | emulator-5554>`. Tools: `tapOn:` text or
   `point: "50%,40%"`, `scrollUntilVisible`, `waitForAnimationToEnd`, `takeScreenshot`.
3. **Screenshot** — `adb exec-out screencap -p > f.png` / `xcrun simctl io booted
   screenshot f.png`. Zoom detail: `sips -c <H> <W> --cropOffset <top> <left> f.png --out
   crop.png`.
4. **READ it.** Never assert "renders fine" on a frame you didn't open.

## Gotchas (each bit a real session)
- **Maestro `inputText` ASCII-only** — no Cyrillic / non-Latin. Use ASCII query, or text
  via `adb`. Prove cross-locale: type a Latin word matching only via another locale's
  string.
- **Local iOS build MUST be a NON-LOGIN shell — which then MUST re-export `LANG`.**
  `nohup bash -c 'export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8; …; make <prod-target>'` — NOT
  `bash -lc`. Login profile puts a broken Ruby on PATH → `pod install` dies with a
  misleading `visionos` CocoaPods error at prebuild. BUT non-login shell drops the
  profile's locale → without the explicit `LANG`, `pod install` dies with
  `Encoding::CompatibilityError` ("CocoaPods requires UTF-8"). Need BOTH.
- **Long builds → background + poll log.** `nohup … > build/log 2>&1 &`, then `sleep N;
  grep -iE "error:|BUILD FAILED|Installing on|Submitting|successfully uploaded" build/log`.
  No foreground tool for 20 min.
- **Metro `--clear` while a dev client is connected** → `Requiring unknown module N` redbox
  on a lazy `import()` (async-chunk id desync). Usually STALE — cold relaunch + one-two
  Maestro `tapOn: "Dismiss"` clears it to a healthy screen. Not a code bug.
- **SceneKit / Metal shader-modifier failures render MAGENTA at RUNTIME, not xcodebuild.** A
  clean prod build BUILDS + SHIPS a magenta board uncaught. Verify a native shader on a
  sim/device BEFORE the store submit. Classic trigger: `#pragma arguments float3` + a KVC
  uniform binding — hardcode colour literals instead.
- **Programmatic `SCNParticleSystem` with no `particleImage` draws hard SQUARES.** Set a
  soft radial (white→transparent) puff texture → smoke/fire/splash read as round puffs.
- **`expo-doctor` non-zero during a build usually benign** (peer-dep + RN-directory-metadata
  warnings) — doesn't fail the build or the submit.
- **System dialog over the app blocks Maestro** — iCloud "verify password" re-auth, a push
  / ATT / location permission — reads as `element not found` (UI occluded, not gone).
  Dismiss step taps the SYSTEM button ("Not Now" / "Allow"), not the app's "Cancel" /
  "Skip". A change newly hitting a platform service (a sync that now actually queries)
  surfaces a prompt older runs never saw — whole suite suddenly fails on the home screen →
  screenshot before assuming a regression.

## Ship (local build → store)
- iOS: repo's prod build-and-submit target (`make`/script step) = clean prebuild → IPA →
  `eas submit` to TestFlight, in the non-login shell. Bump the marketing version first if
  the last already shipped. Apple processes ~5–10 min after the upload.
- **Verify-before-ship:** native visual changes (shaders, particles, a 3D scene) invisible
  to the compiler — a real on-device render is the only proof. Magenta + square particles
  pass the build.

## Red flags — stop
- "Renders fine" no screenshot you opened → drive it, read it.
- Ship a native shader/scene change straight to TestFlight, no sim render → magenta risk.
- Treat a `Requiring unknown module` redbox as a code bug → stale Metro desync; reload + dismiss.
- `bash -lc` for a pod / EAS build → visionos error coming; use a non-login shell.
- Maestro flow via stdin, or Cyrillic `inputText` → write a flow file, use ASCII.
- Hold a foreground shell through a 20-min build → background + poll the log.
- Maestro `element not found` on a screen you know renders → a system dialog (iCloud
  re-auth / permission) on top; dismiss the SYSTEM button, not the app's.
- `bash -c` non-login build but no `LANG` export → visionos gone but
  `Encoding::CompatibilityError` coming; export `LANG=en_US.UTF-8` too.
