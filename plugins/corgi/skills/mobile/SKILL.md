---
name: mobile
description: Use when verifying a mobile (Expo / React Native) change on a real device — "test on the emulator", "run it on the simulator", "screenshot the app", "drive it with Maestro", "does this screen render", "check it on Android/iOS", "tap through the app", "is the animation right" — or when a local iOS/Android build + TestFlight/Play ship needs driving. Covers the device-driving loop (deep links, Maestro flows, screenshots, sips crops) AND the gotchas that actually bite — Maestro ASCII-only input, non-login-shell pod builds, Metro `--clear` redbox desync, native-needs-rebuild, SceneKit magenta-at-runtime, square particles, apple-targets widget/App-Clip extension creds + App Group capability, stale Android autolinking package, low-disk local-build ENOSPC, native dep/ABI skew → dyld symbol-missing launch crash, Maestro `launchApp` resuming the last screen. NOT for writing the app code (normal edits) or authoring corgi-compose (corgi skill).
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
- **Local iOS archive needs GBs of free disk — `df -h` FIRST.** A local prod build writes
  DerivedData + an archive + the IPA (10 GB+). Run low mid-build and `pod install` / the
  archive dies with an ENOSPC or a generic install failure — NOT a clear "disk full."
  Reclaim before launching: `~/Library/Developer/Xcode/DerivedData`, old
  `~/Library/Developer/Xcode/Archives`, stale `build/*.ipa`.
- **Maestro `launchApp` RESUMES the last screen, not home.** A flow assuming home (tap
  "Settings"…) fails `element not found` when the app resumes mid-app from a prior run
  (e.g. a game arena left open). Start from a known state — deep-link to the target route,
  or `launchApp: { clearState: true }` — don't assume the home screen.
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
- **Native dep version/ABI skew → `DYLD Symbol missing` CRASH AT LAUNCH.** A native module
  built against a different core ABI than the one linked — usually one dep drifted off the
  SDK's pinned version, a single patch is enough → `Termination Reason: DYLD … Symbol not
  found … (terminated at launch; ignore backtrace)`. Build + store upload pass CLEAN, no JS
  runs, the build just won't open. PRE-SHIP gate: run the SDK's version-alignment check (Expo:
  `npx expo install --check`) and pin the offender EXACT — a `~` range re-resolves it right
  back up — then reinstall + clean rebuild.
- **System dialog over the app blocks Maestro** — iCloud "verify password" re-auth, a push
  / ATT / location permission — reads as `element not found` (UI occluded, not gone).
  Dismiss step taps the SYSTEM button ("Not Now" / "Allow"), not the app's "Cancel" /
  "Skip". A change newly hitting a platform service (a sync that now actually queries)
  surfaces a prompt older runs never saw — whole suite suddenly fails on the home screen →
  screenshot before assuming a regression.
- **Stale incremental Android autolinking → bogus slug-derived package.** `expo
  run:android` prebuilds INCREMENTALLY; a leftover `android/**/autolinking.json` keyed on
  `com.<slug>` (slug `my-app` → `com.myapp`, not the real `com.org.app`) makes the
  generated `ReactNativeApplicationEntryPoint` reference `com.<slug>.BuildConfig` →
  `compileDebugJavaWithJavac` "package com.<slug> does not exist." `rm -rf android` for a
  clean prebuild. (Expo uses `expo-modules-autolinking`, NOT RN CLI —
  `react-native.config.js` `project.android.packageName` is ineffective.)
- **`make … | tee log` reports tee's exit (0), not the build's** — and a Makefile `eas
  build … || (test -f ipa)` fallback masks failure too. `exit ${PIPESTATUS[0]}` after the
  pipe; trust GROUND TRUTH (new IPA timestamp + "Submitted your app to App Store Connect"),
  never the exit code.
- **Maestro can't flip a SwiftUI / `@expo/ui` Toggle by tapping its label** — label Text +
  switch are separate elements. Tap the switch control (`point` on the row's right edge);
  gate it with the `checked` selector (`when: notVisible: { id, checked: true }`) so it
  flips only when off.

## Native extension targets (apple-targets widgets / App Clips)
A widget / App Clip / share extension via `@bacons/apple-targets` is a SECOND signed
target — own bundle id, profile, capabilities. Each bites once:
- **First build needs a ONE-TIME INTERACTIVE `eas` credential sync.** The non-login
  `--non-interactive` prod build can't create a new target's id + profile → "Credentials
  are not set up. Run this command again in interactive mode." Run `eas build --platform
  ios --profile production --local` (interactive, Apple login + 2FA) ONCE; after that the
  non-interactive build works.
- **Target `name` must be space-free + match the EAS-registered target.** `name: "My
  Widget"` makes the Xcode target "My Widget" but EAS keys creds on the sanitized
  productName ("MyWidget") → the build's `findNativeTargetByName` throws "Could not find
  target … in project.pbxproj." Space-free `name` + a `displayName` for the label; pin
  `bundleIdentifier`.
- **An App Group (or any capability) on the extension is a credential black hole.** `eas`
  does NOT sync capabilities to the EXTENSION App ID ("Synced capabilities: No updates"
  yet signing fails "profile doesn't support the … App Group"). Reuse can't add a
  capability; even a freshly-regenerated profile lacks it until you enable it on the
  **identifier** by hand (portal → Identifiers → the extension id → App Groups ✓ → assign
  the group), then delete + recreate the PROFILE. Delete the PROFILE, **not** the
  identifier — deleting the App ID re-registers it WITHOUT the capability, strictly worse.
  A deep-link-only widget needs NO entitlements — declare none, skip the saga; add the App
  Group only when the widget must read app data (shared-UserDefaults "last result").
- **`cleanPrebuild` (rm ios/android) does NOT touch `targets/`.** A stale
  `generated.entitlements` re-links on the next prebuild after you drop it from
  `expo-target.config.js` — delete it by hand. Gitignore the generated artifacts.

## Ship (local build → store)
- iOS: repo's prod build-and-submit target (`make`/script step) = clean prebuild → IPA →
  `eas submit` to TestFlight, in the non-login shell. Bump the marketing version first if
  the last already shipped — but a build that FAILED after a bump leaves that version
  UNSHIPPED, so reuse it, don't bump again. Apple processes ~5–10 min after the upload.
- **Verify-before-ship:** native visual changes (shaders, particles, a 3D scene) invisible
  to the compiler — a real on-device render is the only proof. Magenta + square particles
  pass the build.
- **A build that compiles + uploads can still DIE AT LAUNCH** — a native version/ABI-skew
  (dyld symbol-missing) crash shows in NEITHER the build NOR the store upload, only when the
  build OPENS on a device. Install + open it ONCE before trusting the ship; if it bounces,
  read the device crashlog (Console.app / Devices & Simulators → View Device Logs).
  `DYLD … Symbol not found` = native version skew (see Gotchas), not app code.

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
- New apple-targets extension + straight `--non-interactive` build → "Credentials not set
  up"; do the one-time interactive `eas build --local` first.
- eas says a capability "synced" on an extension but signing fails "doesn't support App
  Group" → it's not on the identifier; enable on the portal identifier + recreate the profile.
- Android `package com.<slug> does not exist` at compileJava → stale incremental
  autolinking; `rm -rf android`, prebuild fresh.
- Trust a "build completed" exit code → tee / Makefile `||` masks it; confirm a new IPA +
  "Submitted".
- `tapOn` a SwiftUI/@expo/ui switch's label does nothing → tap the switch control by point.
- Launch a local iOS build without `df -h` → low disk kills pod install / archive mid-run
  with a misleading error; free GBs first.
- Maestro flow tapping from "home" after `launchApp` → it resumes the last screen;
  deep-link or clearState to a known start.
- Native dep off the SDK's pinned versions (a `~` resolved it up) → `DYLD Symbol missing`
  launch crash that builds + ships clean. Run the SDK version-alignment check; pin exact.
- "Uploaded = done" with nobody opening the build → a launch-time version/ABI-skew crash
  passes build + upload; install, open once, read the crashlog.

## See also
- **`expo:*` plugin skills** (separate plugin, when installed) — SDK-specific depth:
  `expo:expo-dev-client` (dev client + TestFlight), `expo:building-native-ui`,
  `expo:expo-module` (native modules), `expo:upgrading-expo`, … This skill is the
  device-driving + gotchas layer; lean on `expo:*` for API/SDK specifics.
- **`stories` skill → `references/expo-verification.md`** — the build-time "is an Expo
  change verified?" checklist (detect Expo, rebuild scope, Maestro/screenshot proof). It
  defers HERE for the actual drive loop + the gotchas above.
- Test on the **latest iOS** (the current simulator runtime), not a pinned version — a
  feature floor (e.g. accessory / Control widgets) is a deployment-target detail, not the
  test target.
