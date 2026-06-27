---
description: Ship an Expo / React Native app to the stores via the repo's LOCAL prod build targets — clean prebuild → IPA/AAB → eas submit to TestFlight + Google Play. Handles the non-login-shell + LANG locale rule (incl. EAS's nested pod install), the no-double-bump-after-failure rule, background+poll (the build emits no completion event), ground-truth verification (new IPA/AAB + "Submitted…" line, .submit-skipped check — not the exit code), DRAFT-on-Play, and a stopShip abort. Gates on an on-device render first.
---

Run the **ship** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = optional scope — `ios` / `android` (one platform), `stop` (abort an
  in-progress build), or nothing (both platforms). The version + binary changes are real
  and outward-facing — confirm intent before submitting.

Per `plugins/corgi/skills/ship/SKILL.md`:

1. **Gate first.** Verify the change on a device + READ a screenshot (the `mobile` skill) —
   native-invisible changes ship magenta/blank. Tests + lint green. `df -h` for disk.
2. **Shell + locale.** Build in a NON-LOGIN shell that EXPORTS the locale:
   `nohup bash -c 'export LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8; make <ship-target>' > build/ship.log 2>&1 &`.
   The locale must be exported in the PARENT (EAS's nested pod install inherits it), not
   only inline in the Makefile.
3. **Bump ONCE.** `make ship` bumps the version first; if it dies after the bump, re-run the
   build+submit targets DIRECTLY (they don't bump) — never `make ship` again.
4. **Background + poll** the log for `BUILD SUCCEEDED|Submitting|Submitted your app|error:` —
   there is no completion event for the detached build. `stop` → kill
   `xcodebuild|gradle|eas build|eas submit|fastlane`.
5. **Verify by ground truth.** New `*.ipa`/`*.aab` timestamp + `Submitted your app…`;
   `build/.submit-skipped` empty; Play lands as a DRAFT (promote in console); install + open
   once to catch a launch-time ABI-skew crash.

Report: version shipped, per-store submit status (TestFlight processing / Play DRAFT), and
anything skipped — never just the exit code.
