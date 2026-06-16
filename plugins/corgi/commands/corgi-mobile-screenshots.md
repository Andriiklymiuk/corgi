---
description: Generate store screenshots for an Expo / React Native app end to end — capture N screens × iPhone / iPad / Android-phone+tablet × every locale, frame them (device mockup + localized headline + background) with the external app-store-screenshots editor skill, export at exact App Store / Play sizes, and upload screenshots + localized listings via fastlane deliver / supply. Honors the hard-won capture quirks (one Maestro flow per screen, in-flow takeScreenshot, /tmp staging, per-locale stabilize) and the no-env iOS auth.
---

Run the **mobile-screenshots** store pipeline for `$ARGUMENTS`.

- `$ARGUMENTS` = which screens / locales / device classes to (re)shoot, and/or a stage
  (`capture` | `frame` | `export` | `upload`). Nothing = the full matrix, all four stages.

Per `plugins/corgi/skills/mobile-screenshots/SKILL.md` — four separate stages, verify each
before the next:

1. **Capture** the matrix — N screens × iPhone / iPad / Android-phone+tablet × every locale,
   into `/tmp`, then copy into the repo. Drive via deep links + Maestro (the `mobile` skill).
   Non-negotiables: **one Maestro flow PER SCREEN**; **`takeScreenshot` INSIDE the flow**
   (a shell shot after captures home — Maestro relaunches); gate home with
   `extendedWaitUntil` (NOT `assertVisible … timeout`); **double `waitForAnimationToEnd`**
   to beat the push-slide; **stage to `/tmp`** so Metro's watcher doesn't hot-reload
   mid-shoot; **stabilize per locale** (terminate+relaunch, hard-reboot if it rots);
   switch locale **in-app** (tap by language autonym); recreate persisted game state per
   locale so names localize; integer percents only.
2. **Frame** with the external **app-store-screenshots** skill
   (`ParthJadhav/app-store-screenshots`) — drop raw captures into its
   `public/screenshots/...`, seed `app-store-screenshots.json` (per-slide localized
   headline + `locales`), one idea per slide, 3–5 words. (Installing a third-party skill
   runs its code — confirm the source first.)
3. **Export** at exact store sizes — the editor's **Export bundle**, driven headless
   (puppeteer-core + system Chrome) as `bun export`. iPhone → 6.9″ 1320×2868; Play phone
   → frame to 1080×1920 (raw 1080×2400 is over Play's 2:1); iPad 13″ + Android tablet
   (after the ~130px taskbar chop) valid as-is.
4. **Upload** with **fastlane** (no binary). Author listings as **one markdown file per
   locale** (`store-listings/{en,ru,uk,fr}.md`) → a staging script parses them positionally
   into fastlane's per-field `.txt` tree + stages the framed shots. `deliver`
   (`skip_binary_upload`, `submit_for_review: false`) updates the in-flight App Store
   version; `supply` (`skip_upload_apk/aab`, `track: production`, `validate_only` to dry-run)
   updates the Play listing. **Submit-for-review is opt-in, default OFF** — keep upload and
   submit as separate targets gated by a flag (`SUBMIT_FOR_REVIEW=1`) so iterating never
   starts the review clock; when submitting, App Store needs `submission_information`
   (`export_compliance_uses_encryption: false`, `add_id_info_uses_idfa: false`) +
   `automatic_release`, Play needs `changes_not_sent_for_review: false`. **iOS auth, no env:**
   App Store Connect **API key json file** at a gitignored path (no 2FA) — NOT the `altool`
   app-specific password from Keychain, which `deliver` can't use. Map locales per store
   (`ru` → ASC `ru` / Play `ru-RU`).

**Verify before done** — `magick identify` every PNG and **READ a montage** of every
screen × locale (wrong-screen / wrong-language / mid-transition shots all pass a file
check); **revert the capture-only app edits** (LogBox / resume-to-route / import skips) and
shut the sims/emulators down; **never commit** the API-key / service-account json.
