---
description: Drive + verify a mobile (Expo / React Native) change on a device — Android emulator for JS/Skia (hot-reload), iOS simulator for native (rebuild). Navigate via deep links + Maestro, screenshot, and actually read it. Handles the gotchas — Maestro ASCII-only input, non-login-shell pod builds, Metro `--clear` redbox, SceneKit magenta-at-runtime + square particles — and the local build → TestFlight ship.
---

Run the **mobile** verify flow for `$ARGUMENTS`.

- `$ARGUMENTS` = what to verify (a screen / route / change) and/or `ship` for a local
  build + store submit. Nothing = verify the change just made, on the running device.

Per `plugins/corgi/skills/mobile/SKILL.md`:

1. **Pick surface.** JS / Skia / RN change → **Android emulator** (Metro hot-reload, no
   rebuild). Native (Swift / Kotlin / SceneKit / new dep / config plugin) → **rebuild**
   `expo run:ios` / `expo run:android`; won't hot-reload.
2. **Navigate** — deep link (`adb shell am start … -d "<scheme>://<route>"` /
   `xcrun simctl openurl booted "<scheme>://<route>"`) or Maestro `scrollUntilVisible` +
   `tapOn`.
3. **Drive + assert** with a Maestro flow **FILE** (`--device <udid>` when two devices
   attached); `screenshot` (`adb … screencap` / `simctl io … screenshot`); crop a detail
   with `sips`; **READ** the frame.
4. **Honor the gotchas** — Maestro `inputText` ASCII-only; local iOS builds run in a
   **non-login shell** with `LANG=en_US.UTF-8` (else the `visionos` pod error, then
   `Encoding::CompatibilityError`); background long builds + poll the log; a Metro
   `--clear` redbox is usually a STALE desync (cold reload + `tapOn: Dismiss`); a system
   dialog over the app reads as `element not found` (dismiss the SYSTEM button); SceneKit
   shader failures + a missing `particleImage` only show at RUNTIME (magenta / square
   particles) → verify on a sim BEFORE any TestFlight submit.
5. **Verify before done** — screenshot evidence; native visuals need a real on-device
   render, not just a green build.
