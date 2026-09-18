---
name: mobile
description: "Use when verifying a mobile (Expo / React Native) change on a real device: \"test on the emulator\", \"run it on the simulator\", \"screenshot the app\", \"drive it with Maestro\", \"does this screen render\", \"tap through the app\", or when a local iOS/Android build plus TestFlight/Play ship needs driving. NOT for writing app code or authoring corgi-compose."
---

# Verify mobile change on device

## Overview
A change is not done until you drove it on a device and read the screenshot. A green
build can still render magenta. Pick surface → navigate → drive+assert (argent MCP
when present, else Maestro) → screenshot → look. Evidence before "works".

## Pick surface first
- **JS / TS / Skia / RN styles / shaders** → hot-reload over Metro. Use the **Android
  emulator** — fastest, no rebuild. Edit, save, live.
- **Native (Swift / Kotlin / SceneKit / new native dep / config plugin)** → no hot-reload.
  Rebuild (`expo run:ios` / `expo run:android`) to see it. A JS reload won't.
- One Metro serves both Android + iOS; `expo run:*` reuses a running one.
- **Fast ≠ representative — a JS/style edit still renders differently on iOS and Android.**
  Absolute positioning, `overflow`/clipping, font metrics, shadows and safe-area diverge
  per platform, which is how an Android-only run ships an iOS-only clip / mis-centre /
  cut-off. The Android emulator is the fast inner loop; for any shape- or layout-sensitive
  change, spot-check the same screen on the iOS sim before you trust it or ship.

## Drive loop
1. **Navigate** — deep link beats menu-tapping:
   - Android `adb shell am start -a android.intent.action.VIEW -d "<scheme>://<route>" <pkg>`
   - iOS `xcrun simctl openurl booted "<scheme>://<route>"`
   - or Maestro `scrollUntilVisible` + `tapOn`.
2. **Drive+assert** — **argent MCP when its tools are present** (`mcp__argent__*`):
   `list-devices`, `launch-app`, `describe` for the element tree, `gesture-tap` /
   `gesture-swipe`, `screenshot`; every action returns the screen after it, so no flow
   file and no guessed coordinates. Else **Maestro** — **flow must be a file** (no stdin
   `-`). Two devices attached (emulator + sim) → pass `--device <udid | emulator-5554>`.
   Tools: `tapOn:` text or `point: "50%,40%"`, `scrollUntilVisible`,
   `waitForAnimationToEnd`, `takeScreenshot`. Keep Maestro for a flow worth committing
   to `e2e/`.
3. **Screenshot** — argent `screenshot` (`scale: 1` writes a full-size PNG worth
   attaching), else `adb exec-out screencap -p > f.png` / `xcrun simctl io booted
   screenshot f.png`. Zoom detail: `sips -c <H> <W> --cropOffset <top> <left> f.png --out
   crop.png`.
4. **Read it.** Never assert "renders fine" on a frame you didn't open. Design
   reference at hand (Figma export, ticket mockup / bug screenshot) → Read it next
   to the capture and compare spacing / colour / type / icons — "renders" ≠
   "matches design". A change that has a design behind it wants the full
   pull-design → capture-same-states → labelled side-by-side → deviation-table pass:
   **[`design-parity`](../design-parity/SKILL.md)**.
5. **Geometry bug? Measure, don't eyeball.** A wrong shape (circle gone square,
   clipped / oval disc, mis-aligned pill, off-centre number) is invisible at
   full-frame scale — confirm it by the node's real box, not by squinting:
   - Android: `adb shell uiautomator dump /sdcard/u.xml && adb pull /sdcard/u.xml .`,
     then grep `content-desc="…" …bounds="[x1,y1][x2,y2]"` — `x2-x1` / `y2-y1` is
     the true px size (a square where you want a circle, or a cell far larger than
     its disc, is the tell).
   - Crop + upscale that box: `sips -c <h> <w> --cropOffset <top> <left> f.png --out
     c.png && sips -z <H> <W> c.png` (then open `c.png`).
   - Re-toggle the state and re-measure: a bug that only shows after a state change
     (a freshly-toggled day) won't appear on first paint.
6. **Mutating action? Confirm it persisted.** After a tap that writes state (toggle a
   day, save a value), re-open the screen — or a different view of the same data — and
   check the change is still there. The optimistic first frame can lie; the round-trip
   through the store is the proof it actually wrote.
7. **Setting that drives output? Change it and watch the value recompute.** Don't just
   confirm a setting saved — change it and verify every dependent screen moves (the
   countdown, the prediction, the badge, the chart). If the setting "saves" but the output
   doesn't budge, a derived value is overriding it — a history/auto average, a cached
   default, something computed from the data instead of from the setting. That silent
   override (the setting only *looks* applied) is a common, screenshot-invisible bug, and
   the on-device before/after is the only proof. The fix is usually to make the screen read
   the setting directly and surface the computed value as a *suggestion*, not an override.

## Gotchas
- **A feature behind a flag, or with no seeded data, can't be driven — force it locally,
  then revert.** Waiting for the flag/data means the UI ships unverified. Add one obvious
  constant that turns the feature on and fakes the missing values (dates, counters, a
  "delivered" state), shoot every state by flipping it, then **revert and grep for it before
  committing** — a preview constant left on ships the unreleased feature to everyone. Check
  what the test environment actually holds first; often it has none of the feature's records
  and you already know you're on this path. Seeding real data through the admin/API beats it
  whenever that's available.
- **Manual-layout text clips in two classic ways.** A fit-to-content sizing call collapses a
  multi-line label to one line (the tail truncates) — measure with a **width-constrained**
  fit and set the height from that. And a padded/inset label often does not count its insets
  in the measured size, so a chip cuts its own text ("5 da…") — add the padding by hand.
  Both look fine in code review and only show in a screenshot you read.
- **A nil value formatted into a sentence prints the gap** — "From the ", "publish X from
  to get…", a double space. Hide the whole line/clause when the value is missing (a separate
  string variant for it), instead of interpolating an empty placeholder. Real data usually
  has the value, so this only ever surfaces on the edge case your users hit.
- **Maestro web mode must run `--headless` locally.** Headed launches the *system* Chrome,
  so if the developer already has Chrome open, Chrome's single-instance handoff leaves the
  driver attached to a blank `data:,` tab it cannot measure — the run dies before it ever
  navigates, with `NullPointerException: null cannot be cast to non-null type kotlin.Int at
  maestro.drivers.CdpWebDriver.deviceInfo`. The blank tab looks like a hung app or a stale
  driver; it is neither, and killing Maestro / hunting stray processes finds nothing. CI is
  immune because it already passes `--headless`, which starts its own browser instance —
  which is also why this only ever reproduces on a laptop. Run
  `maestro test --headless --screen-size <W>x<H> flows/<flow>.yaml`; never close the
  developer's browser to work around it.
- **Maestro `inputText` ASCII-only** — no Cyrillic / non-Latin. Use ASCII query, or text
  via `adb`. Prove cross-locale: type a Latin word matching only via another locale's
  string.
- **A fixed-coordinate tap on a CTA misses after the screen reflows.** Tapping a remembered
  `(x,y)` for "Start" / "Submit" / "Continue" lands on the wrong control once selecting an
  option removed a line above it (a "need ≥2 players" warning clears, a validation row
  disappears) — the button shifted up, your tap hits whatever is now there (often a
  destructive "Leave" / "Cancel" below it), and the flow silently resets. Re-read the
  node's real bounds after the state change (`uiautomator dump` + grep the `text=` bounds,
  or Maestro `tapOn:` by text), never reuse pre-change coordinates across a mutating tap.
- **Local iOS build must be a non-login shell — which then must re-export `LANG`.**
  `nohup bash -c 'export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8; …; make <prod-target>'` — not
  `bash -lc`. Login profile puts a broken Ruby on PATH → `pod install` dies with a
  misleading `visionos` CocoaPods error at prebuild. But non-login shell drops the
  profile's locale → without the explicit `LANG`, `pod install` dies with
  `Encoding::CompatibilityError` ("CocoaPods requires UTF-8"). Need both. **It bites
  again one level down:** `eas build --local` runs its own nested prebuild + `pod
  install` that inherits the parent shell's env — so a Makefile that sets `LANG`
  *inline* on its prebuild step is necessary but not sufficient; the ambient shell must
  `export LANG`/`LC_ALL` too, or EAS's internal pod install dies the same way while the
  Makefile's own one succeeded (a baffling "pods worked, then the build failed on pods").
- **Long builds → background + poll log.** `nohup … > build/log 2>&1 &`, then poll:
  `until grep -qiE "BUILD SUCCEEDED|BUILD FAILED|Submitting|successfully uploaded|error:" build/log; do sleep 20; done`.
  The `nohup … &` wrapper returns instantly — a task-runner marks that "complete" the
  moment it backgrounds, so there is **no completion event for the real build**; never
  wait for a done signal, poll the log for milestones. No foreground tool for 20 min.
- **Local iOS archive needs GBs of free disk — `df -h` first.** A local prod build writes
  DerivedData + an archive + the IPA (10 GB+). Run low mid-build and `pod install` / the
  archive dies with an ENOSPC or a generic install failure — not a clear "disk full."
  Reclaim before launching: `~/Library/Developer/Xcode/DerivedData`, old
  `~/Library/Developer/Xcode/Archives`, stale `build/*.ipa`.
- **Maestro `launchApp` resumes the last screen, not home.** A flow assuming home (tap
  "Settings"…) fails `element not found` when the app resumes mid-app from a prior run
  (e.g. a game arena left open). Start from a known state — deep-link to the target route,
  or `launchApp: { clearState: true }` — don't assume the home screen.
- **Metro `--clear` while a dev client is connected** → `Requiring unknown module N` redbox
  on a lazy `import()` (async-chunk id desync). Usually stale — cold relaunch + one-two
  Maestro `tapOn: "Dismiss"` clears it to a healthy screen. Not a code bug.
- **`expo-doctor` non-zero during a build usually benign** (peer-dep + RN-directory-metadata
  warnings) — doesn't fail the build or the submit.
- **Native dep version/ABI skew → `DYLD Symbol missing` crash at launch.** A native module
  built against a different core ABI than the one linked — usually one dep drifted off the
  SDK's pinned version, a single patch is enough → `Termination Reason: DYLD … Symbol not
  found … (terminated at launch; ignore backtrace)`. Build + store upload pass clean, no JS
  runs, the build just won't open. Pre-ship gate: run the SDK's version-alignment check (Expo:
  `npx expo install --check`) and pin the offender exact — a `~` range re-resolves it right
  back up — then reinstall + clean rebuild.
- **System dialog over the app blocks Maestro** — iCloud "verify password" re-auth, a push
  / ATT / location permission — reads as `element not found` (UI occluded, not gone).
  Dismiss step taps the system button ("Not Now" / "Allow"), not the app's "Cancel" /
  "Skip". A change newly hitting a platform service (a sync that now actually queries)
  surfaces a prompt older runs never saw — whole suite suddenly fails on the home screen →
  screenshot before assuming a regression.
- **Stale incremental Android autolinking → bogus slug-derived package.** `expo
  run:android` prebuilds incrementally; a leftover `android/**/autolinking.json` keyed on
  `com.<slug>` (slug `my-app` → `com.myapp`, not the real `com.org.app`) makes the
  generated `ReactNativeApplicationEntryPoint` reference `com.<slug>.BuildConfig` →
  `compileDebugJavaWithJavac` "package com.<slug> does not exist." `rm -rf android` for a
  clean prebuild. (Expo uses `expo-modules-autolinking`, not RN CLI —
  `react-native.config.js` `project.android.packageName` is ineffective.)
- **`make … | tee log` reports tee's exit (0), not the build's** — and a Makefile `eas
  build … || (test -f ipa)` fallback masks failure too. `exit ${PIPESTATUS[0]}` after the
  pipe; trust ground truth (new IPA timestamp + "Submitted your app to App Store Connect"),
  never the exit code.
- **Attach a dev client to Metro + recover a blank screen (Android).** Boot the emulator
  detached, `expo start --dev-client`, `adb reverse tcp:8081 tcp:8081`, then deep-link
  `<scheme>://expo-development-client/?url=http%3A%2F%2Flocalhost%3A8081` to attach and pull
  the bundle. The dev-launcher menu re-appears after a `force-stop` (Continue, or
  `keyevent 4`, to dismiss). But `adb shell input keyevent 4` (back) on a top-level route
  drops the app to a blank screen — you backed out of the route, it didn't crash — recover
  by re-launching the dev-client URL (or `<scheme>://<route>`), not by waiting.
- **An agent / tool file-write may not trip Metro fast-refresh — you read the old bundle.** A
  save that doesn't come from the editor's own save sometimes never reaches Metro's watcher,
  so the device still runs the previous code and your "fix" looks unchanged (or falsely
  passes). Before trusting any after-edit screenshot, confirm a fresh `Android Bundled … (N
  modules)` / `iOS Bundled …` line appeared in the Metro log since your edit; if not, force a
  reload (re-launch the dev-client URL, or dev-menu → Reload) and re-shoot. A delta bundle
  (`… (1 module)`) is the proof it picked up the change. **Worse after a prod build's
  `cleanPrebuild` (`rm ios/android`):** the still-running Metro from `expo run:ios`
  detaches — a tool-write, a `touch`, and a cold app relaunch all fail to re-bundle (you
  keep running the old code; a stray `(1 module)` delta for an unrelated file fools you into
  thinking it reloaded). Only kill Metro (`lsof -ti :8081 | xargs kill`) + restart
  `expo start --dev-client --clear` — a full `(N modules)` re-bundle — loads the new code.
- **A cold relaunch (force-stop → launcher / deep-link) re-bundles over Metro — a black /
  blank frame with no mounted UI for tens of seconds is normal, not a crash.** The first
  probe after a kill catches the bundle still loading: empty view tree (Android `uiautomator
  dump` returns nothing / your target node missing), a blank iOS screenshot, Maestro
  `element not found`. Don't read it as a failure and burn retries — poll: re-dump / re-shoot
  in a loop until a known node appears, or watch the Metro log for the `Bundled` line, then
  screenshot / navigate / assert. Hits both platforms (the JS bundle loads on cold start
  either way); a dev/Metro build pays it on every relaunch, a release build doesn't.
- **Two apps on the machine → the dev client attaches to the wrong Metro.** When a second
  Expo project is already running `expo start`, it owns the default Metro port 8081, so a
  freshly-launched dev client auto-attaches there and serves the other project's bundle —
  a baffling redbox (e.g. a "missing native module" naming a module/file the current app
  doesn't even use, or just the wrong screen). `expo run:ios --port 8082` moves this
  project's Metro to a free port, but the launched binary still asks for 8081, and the
  `<scheme>://expo-development-client/?url=http://localhost:<port>` deep link often does
  not redirect it. Fix: set the binary's saved packager location, then relaunch — iOS
  `xcrun simctl spawn booted defaults write <bundleId> RCT_jsLocation "localhost:<port>"`;
  Android `adb reverse tcp:<port> tcp:<port>` then the dev-client `?url=` deep link.
  Symptom = wrong-app bundle, not a code bug.
- **Framework- and app-specific traps** (SceneKit/Metal shaders and particles, RNGH
  `Pressable` sizing, native views inside a `formSheet`, native header search on
  recent iOS, paywall capture, SwiftUI toggles): `references/gotchas.md` — read it
  when the screen uses one of those.

## Native extension targets (apple-targets widgets / App Clips)
A widget / App Clip / share extension via `@bacons/apple-targets` is a second signed
target — own bundle id, profile, capabilities. Each bites once:
- **First build needs a one-time interactive `eas` credential sync.** The non-login
  `--non-interactive` prod build can't create a new target's id + profile → "Credentials
  are not set up. Run this command again in interactive mode." Run `eas build --platform
  ios --profile production --local` (interactive, Apple login + 2FA) once; after that the
  non-interactive build works.
- **Target `name` must be space-free + match the EAS-registered target.** `name: "My
  Widget"` makes the Xcode target "My Widget" but EAS keys creds on the sanitized
  productName ("MyWidget") → the build's `findNativeTargetByName` throws "Could not find
  target … in project.pbxproj." Space-free `name` + a `displayName` for the label; pin
  `bundleIdentifier`.
- **An App Group (or any capability) on the extension is a credential black hole.** `eas`
  does not sync capabilities to the extension App ID ("Synced capabilities: No updates"
  yet signing fails "profile doesn't support the … App Group"). Reuse can't add a
  capability; even a freshly-regenerated profile lacks it until you enable it on the
  **identifier** by hand (portal → Identifiers → the extension id → App Groups ✓ → assign
  the group), then delete + recreate the profile. Delete the profile, **not** the
  identifier — deleting the App ID re-registers it without the capability, strictly worse.
  A deep-link-only widget needs no entitlements — declare none, skip the saga; add the App
  Group only when the widget must read app data (shared-UserDefaults "last result").
- **`cleanPrebuild` (rm ios/android) does not touch `targets/`.** A stale
  `generated.entitlements` re-links on the next prebuild after you drop it from
  `expo-target.config.js` — delete it by hand. Gitignore the generated artifacts.

## Ship (local build → store)
The full local-build → store-submit flow (LANG/shell rules, no-double-bump, background +
poll, ground-truth verify, `stopShip`) is the **`ship` skill** — this section is the
on-device render gate it leans on. In short:
- iOS: repo's prod build-and-submit target (`make`/script step) = clean prebuild → IPA →
  `eas submit` to TestFlight, in the non-login shell. Bump the marketing version first if
  the last already shipped — but a build that FAILED after a bump leaves that version
  unshipped, so reuse it, don't bump again. Apple processes ~5–10 min after the upload.
- **Verify-before-ship:** native visual changes (shaders, particles, a 3D scene) invisible
  to the compiler — a real on-device render is the only proof. Magenta + square particles
  pass the build.
- **A build that compiles + uploads can still die at launch** — a native version/ABI-skew
  (dyld symbol-missing) crash shows in neither the build nor the store upload, only when the
  build opens on a device. Install + open it once before trusting the ship; if it bounces,
  read the device crashlog (Console.app / Devices & Simulators → View Device Logs).
  `DYLD … Symbol not found` = native version skew (see Gotchas), not app code.

## See also
- **[`before-after`](../before-after/SKILL.md)** — when the change should be proven against
  the state it replaced: build the base branch too, capture the same screen twice, attach
  both to the PR. This skill is the capture half of it.
- **[`design-parity`](../design-parity/SKILL.md)** — when the change has a design behind it:
  pulling the frames to the repo, making a flagged/unseeded feature reachable, labelled
  side-by-sides, and the deviation table (fixed vs deliberate) the MR needs.
- **`ship` skill** — driving a local-build → App Store / Play submit (LANG/shell rules,
  no-double-bump, background+poll, ground-truth verify, `stopShip`). It gates on this
  skill's device render before submitting.
- **`purchases` skill** — IAP / store-metadata + RevenueCat: a version-controlled
  source-of-truth for product ids + App Review notes, and pushing IAP `reviewNote` via the
  App Store Connect API (fastlane `deliver` can't).
- **`expo:*` plugin skills** (separate plugin, when installed) — SDK-specific depth:
  `expo:expo-dev-client` (dev client + TestFlight), `expo:building-native-ui`,
  `expo:expo-module` (native modules), `expo:upgrading-expo`, … This skill is the
  device-driving + gotchas layer; lean on `expo:*` for API/SDK specifics.
- **`stories` skill → `references/expo-verification.md`** — the build-time "is an Expo
  change verified?" checklist (detect Expo, rebuild scope, Maestro/screenshot proof). It
  defers here for the actual drive loop + the gotchas above.
- Test on the **latest iOS** (the current simulator runtime), not a pinned version — a
  feature floor (e.g. accessory / Control widgets) is a deployment-target detail, not the
  test target.
