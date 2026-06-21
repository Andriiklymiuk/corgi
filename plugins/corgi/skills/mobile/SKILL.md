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
- **Fast ≠ representative — a JS/style edit still renders DIFFERENTLY iOS↔Android.** Absolute
  positioning, `overflow`/clipping, font metrics, shadows and safe-area diverge per platform
  (this is exactly how an Android-only run ships an iOS-only clip / mis-centre / cut-off).
  The Android emulator is the fast inner loop; for any shape- or layout-sensitive change,
  spot-check the SAME screen on the iOS sim before you trust it or ship — don't conclude
  from one platform.

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
5. **Geometry bug? MEASURE, don't eyeball.** A wrong shape (circle gone square,
   clipped / oval disc, mis-aligned pill, off-centre number) is INVISIBLE at
   full-frame scale — confirm it by the node's real box, not by squinting:
   - Android: `adb shell uiautomator dump /sdcard/u.xml && adb pull /sdcard/u.xml .`,
     then grep `content-desc="…" …bounds="[x1,y1][x2,y2]"` — `x2-x1` / `y2-y1` is
     the true px size (a square where you want a circle, or a cell far larger than
     its disc, is the tell).
   - Crop + UPSCALE that box: `sips -c <h> <w> --cropOffset <top> <left> f.png --out
     c.png && sips -z <H> <W> c.png` (then open `c.png`).
   - Re-toggle the state and re-measure: a bug that only shows AFTER a state change
     (a freshly-toggled day) won't appear on first paint.
6. **Mutating action? Confirm it PERSISTED.** After a tap that writes state (toggle a
   day, save a value), re-open the screen — or a DIFFERENT view of the same data — and
   check the change is still there. The optimistic first frame can lie; the round-trip
   through the store is the proof it actually wrote.
7. **Setting that drives output? CHANGE it and watch the value RECOMPUTE.** Don't just
   confirm a setting saved — change it and verify every dependent screen moves (the
   countdown, the prediction, the badge, the chart). If the setting "saves" but the output
   doesn't budge, a DERIVED value is overriding it — a history/auto average, a cached
   default, something computed from the data instead of from the setting. That silent
   override (the setting only *looks* applied) is a common, screenshot-invisible bug, and
   the on-device before/after is the only proof. The fix is usually to make the screen read
   the setting directly and surface the computed value as a *suggestion*, not an override.

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
- **A gesture-handler `Pressable` as the SIZED flex cell stretches its child — circle →
  square.** RNGH `Pressable` doesn't hold a fixed pixel width the way a plain `View` does,
  and an INLINE / dynamic width style (worse with React Compiler on) lets the child disc
  grow to fill the cell → a "circle" renders as a rounded square — and often only AFTER a
  re-render (a freshly-toggled day) while the first-paint ones still look right. Fix: size
  the cell with a plain `View` / STATIC `StyleSheet` entry, keep the shape a FIXED, centred
  child, and mirror the screen's already-working sibling cell (e.g. the month-view DayCell)
  instead of re-deriving sizes inline. `onLayout` on an RNGH Pressable is flaky too — put
  it on a plain wrapper.
- **Absolute-fill background behind a separately-centred label clips / offsets on iOS.** A
  disc drawn as a `position:absolute` layer BEHIND a sibling number can sit off-centre or
  get clipped at the top on iOS (fine on Android). Fix: make it ONE in-flow element — a
  fixed circle with the label INSIDE it — so the cell centres the whole unit. (Keep an
  absolute layer only for a shape that must bleed past the cell, like a joined period
  pill.)
- **Attach a dev client to Metro + recover a blank screen (Android).** Boot the emulator
  detached, `expo start --dev-client`, `adb reverse tcp:8081 tcp:8081`, then deep-link
  `<scheme>://expo-development-client/?url=http%3A%2F%2Flocalhost%3A8081` to attach and pull
  the bundle. The dev-launcher menu re-appears after a `force-stop` (Continue, or
  `keyevent 4`, to dismiss). But `adb shell input keyevent 4` (back) on a top-level route
  drops the app to a BLANK screen — you backed OUT of the route, it didn't crash — recover
  by re-launching the dev-client URL (or `<scheme>://<route>`), not by waiting.
- **An agent / tool file-write may not trip Metro fast-refresh — you read the OLD bundle.** A
  save that doesn't come from the editor's own save sometimes never reaches Metro's watcher,
  so the device still runs the PREVIOUS code and your "fix" looks unchanged (or falsely
  passes). Before trusting any after-edit screenshot, confirm a fresh `Android Bundled … (N
  modules)` / `iOS Bundled …` line appeared in the Metro log SINCE your edit; if not, force a
  reload (re-launch the dev-client URL, or dev-menu → Reload) and re-shoot. A delta bundle
  (`… (1 module)`) is the proof it picked up the change.
- **Two apps on the machine → the dev client attaches to the WRONG Metro.** When a second
  Expo project is already running `expo start`, it owns the default Metro port 8081, so a
  freshly-launched dev client auto-attaches there and serves the OTHER project's bundle —
  a baffling redbox (e.g. a "missing native module" naming a module/file the current app
  doesn't even use, or just the wrong screen). `expo run:ios --port 8082` moves THIS
  project's Metro to a free port, but the launched binary still asks for 8081, and the
  `<scheme>://expo-development-client/?url=http://localhost:<port>` deep link often does
  NOT redirect it. Fix: set the binary's saved packager location, then relaunch — iOS
  `xcrun simctl spawn booted defaults write <bundleId> RCT_jsLocation "localhost:<port>"`;
  Android `adb reverse tcp:<port> tcp:<port>` then the dev-client `?url=` deep link.
  Symptom = wrong-app bundle, not a code bug.
- **Native `headerSearchBarOptions` (react-native-screens) on iOS 26 floats to the bottom by
  default.** The default `placement: "automatic"` drops the search field to the BOTTOM of the
  screen, overlapping content (a UIKit root-screen toolbar-integration bug) → set
  `placement: "stacked"` and it anchors below the title bar as expected (rn-screens forces
  `allowToolbarIntegration:false` for stacked, which dodges the bug). These header/search
  options are JS nav config → they HOT-RELOAD on an already-built dev client (no native
  rebuild), so iterate the layout live on the sim. (`headerLargeTitle` can also render blank
  in some expo-router setups — if it does on yours, draw the big title in-content instead of
  fighting it; verify per-app, don't assume.) A documented "native X can't anchor / doesn't
  work here" is often a STALE, fixable conclusion — re-test the native option on-device first.
- **"Search visible at rest AND tucking on scroll" (Telegram-style) wants a working large
  title; without one, drive it from JS.** `hideWhenScrolling: true` on its own leaves a
  stacked search HIDDEN at rest (pull-to-reveal). To show it at the top and hide it once the
  list scrolls, keep `headerSearchBarOptions` mounted and REMOVE it (set `undefined`) past a
  scroll threshold — with hysteresis whose gap clears the search bar's own height, or the
  layout shift from removing it bounces the offset back over the threshold (flicker loop).
  Gate to iOS — a Material toolbar search icon (Android) is compact and shouldn't hide on
  scroll.

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
- "Looks like a circle" from a full-frame screenshot → MEASURE the node bounds + zoom-crop;
  square discs, top-clipped shapes and off-centre numbers are invisible at scale.
- RNGH `Pressable` sized with an inline / dynamic width (React Compiler on) → child stretches
  (circle → square, often only after a re-render); size with a plain View + static StyleSheet,
  fixed centred child.
- `keyevent 4` left a blank screen → you backed out of the route, not a crash; re-launch the
  dev-client URL to recover.
- Native search floating at the bottom of the screen on iOS 26 → default
  `headerSearchBarOptions` placement; set `placement: "stacked"`. (Large title blank in your
  setup? draw it in-content.) Re-test any stale "native X doesn't work here" on-device —
  header/search options hot-reload, so it's cheap.
- Concluded a layout / shape change is fine from ONE platform → iOS and Android clip, centre
  and size differently; spot-check shape-sensitive UI on the iOS sim too before ship.
- Hand-building a custom cell / shape with inline, per-render sizes → mirror the screen's
  existing working component and lift sizes into a static StyleSheet; inline / dynamic styles
  are where shape bugs (and React Compiler surprises) hide.
- Your edit isn't showing on device → you may be reading the OLD bundle; confirm a new Metro
  `Bundled` line since the edit (force a reload if none) before concluding anything.
- Verified a mutating tap by the optimistic frame only → re-open the screen / another view and
  confirm the write PERSISTED through the store, not just the instant paint.
- Changed a setting and nothing downstream moved → a derived / auto value (an average, a
  cached default, a value computed from the data) is overriding it; the setting isn't the
  source of truth. True for ANY setting → output — units, theme, thresholds, sort order, a
  prediction — so change-it-and-watch is the universal test; the fix is to read the setting
  directly and demote the computed value to a suggestion.

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
