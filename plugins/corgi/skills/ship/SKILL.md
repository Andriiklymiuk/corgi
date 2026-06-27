---
name: ship
description: Use when shipping an Expo / React Native app to the stores via the repo's LOCAL build targets — "ship it", "make ship", "build and submit to TestFlight / Play", "push a release to the App Store", "cut a build", "release the app", "upload the binary". Drives a local prod build → IPA/AAB → eas submit to TestFlight + Google Play, with the shell/locale rules, the no-double-bump rule, background+poll (no completion event), ground-truth verification (not the exit code), DRAFT-on-Play behavior, and a stopShip escape hatch. Gates on the `mobile` skill's on-device render before submitting. NOT for verifying a change on a device (mobile skill), IAP / store metadata (purchases skill), or remote EAS cloud builds.
---

# Ship a local build to the stores

## Overview
Ship = repo's prod LOCAL build+submit target (`make ship`, or `make localIosProd` +
`localAndroidProd`) → clean prebuild → IPA/AAB → `eas submit` to TestFlight + Play. Build
COMPILING ≠ shipped. **Ground truth = a new IPA/AAB on disk + a "Submitted…" line**, never
the exit code. Long + unattended → background + poll.

## Before you ship
- **Render gate (`mobile` skill).** Native-invisible change (shader, particles, 3D scene,
  Skia) compiles + ships MAGENTA / square / blank. Drive on device + READ the screenshot
  FIRST. Green build ≠ verified build.
- Tests + lint/types green. A ship is no place to find a broken bundle.
- `df -h` first — a local iOS archive writes DerivedData + archive + IPA (10 GB+); low disk
  kills `pod install` / the archive mid-run with a misleading error.

## The shell + locale rule (this is the #1 ship failure)
Run the build in a **NON-LOGIN** shell that **also EXPORTS the locale**:
```
nohup bash -c 'export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8; make <ship-target>' > build/ship.log 2>&1 &
```
- `bash -lc` (login) puts a broken rbenv Ruby on PATH → `pod install` dies with a
  misleading CocoaPods `visionos` error at prebuild. So: non-login.
- BUT a non-login shell drops the profile's locale → empty `LANG`/`LC_ALL` →
  `Encoding::CompatibilityError` ("CocoaPods requires UTF-8"). So: export it.
- **And it bites one level down:** `eas build --local` runs its OWN nested prebuild +
  `pod install` inheriting the PARENT env. A Makefile that sets `LANG` *inline* for its
  own prebuild step is necessary but NOT sufficient — if the ambient shell's `LANG` is
  empty, EAS's internal pod install dies the same way while the Makefile's succeeded.
  Always export in the parent. (Check first: `echo "LANG=$LANG"` — empty is the tell.)

## Version bump — ONCE
`make ship` bumps the marketing version (and EAS bumps iOS buildNumber / Android
versionCode per attempt) FIRST, then builds. **If the build dies after the bump, the
version is bumped but UNSHIPPED — do NOT run `make ship` again** (it double-bumps). Re-run
the build+submit targets DIRECTLY (`make localIosProd && make localAndroidProd`) — they
don't bump — reusing the already-bumped version.

## Drive it
1. **Background + poll** — there's no foreground tool for 20–40 min, and the `nohup … &`
   wrapper "completes" the instant it backgrounds (any task-runner marks THAT done) — so
   there is **no completion event for the real build**. Poll the log:
   ```
   until grep -qiE "BUILD SUCCEEDED|BUILD FAILED|Submitting|successfully uploaded|Submitted your app|error:" build/ship.log; do sleep 25; done
   ```
   Watch the phases: pods → archive/Gradle → IPA/AAB exported → `eas submit` → Apple/Google
   ingest ("Waiting for submission to complete" can sit minutes — normal).
2. **stopShip escape hatch** — to abort mid-build, kill the build processes (a `stopShip`
   make target, or `pkill -f "xcodebuild|gradle|eas build|eas submit|fastlane"`). The
   detached `nohup` build is not a tracked task — `pkill` it, don't wait.

## Verify by ground truth (not the exit code)
- **iOS:** a new `*.ipa` (fresh timestamp) + `✔ Submitted your app to Apple App Store
  Connect!` / `successfully uploaded to App Store Connect`. Apple processes ~5–10 min after.
- **Android:** a new `*.aab` + `✔ Submitted your app to Google Play Store!`. Play submits
  land as a **DRAFT** release by default (`changes_not_sent_for_review` / draft status) —
  you promote it in Play Console; "submitted" ≠ "released".
- **`build/.submit-skipped`** (or the WARNING line) — a Makefile `eas build … || (test -f
  ipa)` fallback + `tee`'d logs report exit 0 while a submit was SKIPPED. If that file is
  non-empty, the upload did NOT happen — upload by hand (Transporter / `make submitIos`).
- **A build that compiles + uploads can still DIE AT LAUNCH** — native version/ABI skew
  (`DYLD Symbol not found`) shows in NEITHER build NOR upload. Install + open the binary
  ONCE; if it bounces, it's dep-version skew (pin exact, see `mobile`), not app code.

## Red flags — stop
- `bash -lc` for the build → `visionos` CocoaPods error incoming; non-login shell.
- Non-login but no `LANG` export → `Encoding::CompatibilityError`; export `LANG`/`LC_ALL`.
- Pods "worked" but the build later failed on pods → it's EAS's NESTED pod install with an
  empty ambient `LANG`; export it in the parent shell, not just inline in the Makefile.
- Re-running `make ship` after a post-bump failure → double version bump; re-run the
  build+submit targets directly instead.
- Trusting a "build completed" exit code / a `tee`'d "✓" → `tee` reports tee's exit and a
  Makefile `||` masks failure; trust the new IPA/AAB timestamp + the "Submitted…" line.
- "Uploaded = released" on Play → it's a DRAFT; promote it in Play Console.
- "Uploaded = done" with nobody opening the build → a launch-time ABI-skew crash passes
  build + upload; install + open once.
- Shipping a native shader/scene/Skia change straight to a store with no on-device render →
  magenta risk; run the `mobile` render gate first.

## See also
- **`mobile` skill** — the device-driving loop + the full gotcha list (the render gate this
  skill leans on, plus the dyld-skew / disk / autolinking traps a ship hits).
- **`purchases` skill** — store IAP + metadata (separate from the binary submit).
- `expo:*` plugin skills (when installed) — EAS / SDK specifics.
