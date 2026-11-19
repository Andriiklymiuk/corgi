---
description: Drive + verify a mobile (Expo / React Native) change on a device - Android emulator for JS/Skia (hot-reload), iOS simulator for native (rebuild).
---

Run the **mobile** verify flow for `$ARGUMENTS`.

- `$ARGUMENTS` = what to verify (a screen / route / change). Nothing = verify the change
  just made, on the running device. A local build + store submit is `/corgi-ship`.

Per the `mobile` skill (load it with the Skill tool: `corgi:mobile`):

1. **Pick surface.** JS / Skia / RN change → **Android emulator** (Metro hot-reload, no
   rebuild). Native (Swift / Kotlin / SceneKit / new dep / config plugin) → **rebuild**
   `expo run:ios` / `expo run:android`; won't hot-reload.
2. **Navigate** - deep link (`adb shell am start … -d "<scheme>://<route>"` /
   `xcrun simctl openurl booted "<scheme>://<route>"`) or Maestro `scrollUntilVisible` +
   `tapOn`.
3. **Drive + assert** with a Maestro flow **file** (`--device <udid>` when two devices
   attached); `screenshot` (`adb … screencap` / `simctl io … screenshot`); crop a detail
   with `sips`; **read** the frame.
4. **Honor the gotchas** in the skill (Maestro input, the build shell and locale, long
   builds, system dialogs); framework- and app-specific traps are in its
   `references/gotchas.md`.
5. **Verify before done** - screenshot evidence; native visuals need a real on-device
   render, not just a green build.
