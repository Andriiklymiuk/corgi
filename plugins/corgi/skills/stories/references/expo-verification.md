# Expo / React Native service — verify on a simulator, not just jest

A service is an **Expo app** when its `package.json` depends on `expo` (or
`react-native` with `ios/`/`android/` dirs). For these, a green jest run is NOT
proof the story works: Metro module interop, native modules, permissions,
entitlements, navigation, and visual layout only fail **on a device**. Verify on
a simulator and attach evidence, the same way a web story gets a Playwright run.

If the **expo plugin** is installed, lean on its skills for depth — they track
the current SDK: `expo:building-native-ui` (UI/navigation), `expo:expo-deployment`.
**Know the split, though:** `expo:expo-dev-client` is **EAS Build / TestFlight /
physical-device distribution** (`eas build [--local]`) — a *different* job from
verifying a story's change. For that, use the **fast local-simulator loop below**
(`prebuild → pods → xcodebuild → simctl`), not a slow cloud `eas build`. Reach for
the dev-client skill only when the story is actually about distribution / a physical
device. Not installed → this reference alone is enough. Degrade gracefully, never
block.

## 1. Decide rebuild scope — JS-only vs native

| Change | Examples | What's needed |
| --- | --- | --- |
| **JS-only** | components, screens, stores, i18n, styles | existing dev build/Expo Go + Metro reload — no rebuild |
| **Native** | new native dep (`expo install` of a lib with pods), `app.json` plugins / permissions / entitlements / Info.plist, prebuild config | **full rebuild**: prebuild → pods → xcodebuild → reinstall |

When unsure: did `package.json` native deps or `app.json` change? → native.

## 2. Build + run (iOS simulator, macOS host)

```bash
# Metro — exactly ONE instance per app; a stale Metro for the same app can hang
# builds and serve old bundles. Check first, reuse or kill.
lsof -i :8081 -sTCP:LISTEN || (bunx expo start > /tmp/metro-<svc>.log 2>&1 &)

# Native rebuild path:
npx expo prebuild -p ios
LANG=en_US.UTF-8 pod install --project-directory=ios
#   ^ REQUIRED: without a UTF-8 locale CocoaPods dies with
#     "Unicode Normalization not appropriate for ASCII-8BIT"
xcodebuild -workspace ios/<Name>.xcworkspace -scheme <Name> \
  -configuration Debug -sdk iphonesimulator -derivedDataPath ios/build
#   (same derived-data path `expo run:ios` uses — caches shared; `npx expo
#    run:ios` is the one-shot alternative when you don't need the artifact path)

# Install + launch (UDID from `xcrun simctl list devices available`):
xcrun simctl boot <udid>; open -a Simulator
xcrun simctl install <udid> ios/build/Build/Products/Debug-iphonesimulator/<Name>.app
xcrun simctl launch <udid> <bundle-id>
```

Long compiles: run xcodebuild in the background and keep working; the `.app` is
complete only when `Info.plist` exists inside it — `simctl install` before that
fails with "Missing bundle ID".

## 3. Drive the UI — Maestro

`maestro` (install: `brew install mobile-dev-inc/tap/maestro` — curl|bash
installers are often denied). Keep flows in the repo's `e2e/` so they ship with
the PR; they are the mobile equivalent of the visual/e2e harness for
FAILS-on-base regression checks when the bug is reproducible by flow.

Quirks that WILL bite (each cost a failed run once):

- **Always `launchApp: {stopApp: true}`** — apps that persist/resume their last
  route do not start on the home screen; tap sequences assuming the menu fail.
- **Prefer tap navigation over `openLink`** — deep links pop iOS's "Open in
  <App>?" dialog which races the next tap; if you must deep-link, follow with a
  conditional dismiss (`runFlow when visible: "Open"` / localized equivalent).
- **Guard variable screens with `runFlow when visible`** — first-launch
  onboarding, permission prompts, leftover dialogs.
- **Flows are locale-sensitive.** Simulators inherit the host region — text
  selectors must match the sim's language (or add `testID`s and select by id).
  Screenshot first when a selector unexpectedly misses.
- **Dynamic text** (room codes, generated ids): `maestro hierarchy` dumps the
  accessibility tree as JSON — parse the value out instead of guessing.
- **Verify visually**: `xcrun simctl io <udid> screenshot /tmp/x.png`, then Read
  the image. Assertions prove presence; the screenshot proves it looks right —
  attach the before/after to the PR for visual stories.
- **Watch the Metro log** (`/tmp/metro-<svc>.log`) after each flow — runtime
  `WARN`/`ERROR` (e.g. a swallowed exception behind an error state in the UI)
  shows up there, not in Maestro output.

## 4. Multi-device features (P2P / LAN / lobby)

Simulators share the Mac's network stack and Bonjour daemon — host/join over
mDNS/TCP between sims exercises the real radio path. Clone extras:

```bash
xcrun simctl create "iPhone X B" <device-type-id> <runtime-id>
```

Install the same `.app` on each; target Maestro per device with
`maestro --udid <udid> test …`. Mind product minimums (e.g. a 3-player gate
needs 3 sims). Delete cloned sims after (`simctl delete`).

## 5. Evidence into the story

- Spec's manual-verification section: which flows ran, on what sims, result.
- PR body: screenshots (before/after for visual), flow names in `e2e/`.
- A device-only bug found this way (interop, entitlement, dialog) → fix it in
  this story if in scope, else report `needs attention` — never ship on jest
  green alone when the change is native-scoped.
