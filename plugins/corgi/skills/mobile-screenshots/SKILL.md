---
name: mobile-screenshots
description: Use when generating store screenshots for an Expo / React Native app — "make App Store screenshots", "Play Market screenshots", "store screenshots for all languages", "framed marketing screenshots", "screenshots in the right store sizes", "feature graphic", "upload screenshots to the stores", "send screenshots to App Store / Play automatically", "device-frame the screenshots with headlines". Covers the capture matrix (iPhone / iPad / Android phone+tablet × locales), framing via the external app-store-screenshots editor skill, exporting at exact store sizes, and uploading with fastlane deliver/supply. NOT a single-screen verify (that's the `mobile` skill) or authoring corgi-compose (the `corgi` skill).
---

# Store screenshots, end to end

## Overview
Goal isn't "a screenshot" — it's **N screens × every device class × every locale**,
**framed** (device mockup + headline + bg), at **exact store sizes**, optionally
**uploaded**. Four stages: **capture → frame → export → upload**. Driving one device is
the [`mobile`](../mobile/SKILL.md) skill; this is the matrix + the store pipeline on top.
Every quirk below cost a real session — honor them or re-shoot.

Pipeline at a glance:
1. **Capture** raw app screens — per screen, per locale, per device — into `/tmp`, then copy into the repo.
2. **Frame** them with the **app-store-screenshots** editor skill (device frame + localized headline + background).
3. **Export** framed PNGs at every required store size (`bun export`, headless).
4. **Stage + upload** screenshots + localized listings via `fastlane deliver` (App Store) / `supply` (Play).

## 1 · Capture — the matrix
Drive via deep links + Maestro (see the `mobile` skill). The store twist is reliability
across a long run of many screens × locales × devices.

**The capture recipe that actually holds (learned the hard way):**
- **One Maestro flow PER SCREEN.** Multiple screens in one flow lag/scramble — screen N's
  shot lands on screen N-1, or two adjacent screens swap. One screen, one `maestro test`.
- **`takeScreenshot` INSIDE the flow**, not a shell screenshot after. Maestro relaunches
  the app to home on test exit → a `simctl io … screenshot` / `adb … screencap` taken
  after the flow captures **home**. Keep the shot in the flow.
- **Gate home before navigating** with `extendedWaitUntil` on a locale-neutral home anchor
  (the app name shows on home in every language). `assertVisible` does NOT take a `timeout`
  in current Maestro ("Unknown Property: timeout") — use `extendedWaitUntil { visible, timeout }`.
- **Beat the push-slide with a DOUBLE `waitForAnimationToEnd`.** The first returns before
  the slide even starts (catches a mid-transition frame, prev screen peeking at the edge);
  the second waits the slide out.
- **Write shots to `/tmp`, copy into the repo only after.** Writing PNGs inside the Expo
  project trips **Metro's file-watcher → a "Refreshing…" hot reload mid-capture** that
  corrupts the next screens. Or add the screenshots dir to `metro.config` `watchFolders`/blocklist.
- **Stabilize PER LOCALE** (terminate + relaunch the app, wait ~12–18s). Sims/emulators
  **degrade over a long run** — snaps start failing (empty/MISSING), corrupt PNGs, or
  leaking home/settings. A fresh app per locale resets it. If even one locale still rots,
  hard-reboot the device (`simctl shutdown`+`boot`).
- **GPU-heavy screens corrupt intermittently.** 3D dice mid-roll, live game frames → a bad
  PNG ("improper image header") or MISSING. **Validate each PNG** (`magick identify`) and
  **retry**; capture during the still moment (countdown, settled dice), not peak motion.
- **Locale switch is in-app**, not external — the locale store is MMKV (not writable from
  the host). Open Settings, tap the language. **Language autonyms are stable across
  locales** (e.g. "Français" reads the same in every UI language) → tap by autonym.
- **Persisted game state needs recreating per locale.** A life counter / saved match keeps
  the player names from when it was first created → shows the wrong language. Recreate it
  in-flow (open the mode → start a fresh game) so names localize; dismiss any first-run
  icon guide ("Got it" / localized).
- **First-run blocks.** Fresh installs show onboarding + per-feature guides → skip them
  ("Skip" / localized) once. Watch out: onboarding often contains the **app name**, so a
  home-anchor `extendedWaitUntil` *false-passes* on it — skip onboarding before relying on
  the anchor.

**Per-device gotchas:**
- **iPad SpringBoard "Open in app?" confirm fires on every `simctl openurl`** (not on
  iPhone). Use Maestro **`openLink`** instead — no prompt, works on Android too.
- **Android nav bar shifts bottom CTAs up** — a "Start"/"Roll" point-tap that hits on
  iPhone (~93%) misses on Android (~94%, the gesture pill steals the bottom). Measure per
  device (`sips`/`magick -crop` the bottom band). And **decimals break Maestro `swipe`/point
  taps** ("Parsing Failed") — integer percents only.
- **Horizontal pickers (dice die-row) scroll only in their own zone** — swipe across the
  pills' x-range, not over the stepper, or it's a no-op and a clipped option's tap lands on
  the wrong control. On wide screens (iPad/tablet) all options show → no swipe needed.
- **Android tablet emulators boot LANDSCAPE** + show a persistent **launcher taskbar**.
  `adb -s <dev> emu rotate` → portrait (modern AVDs resist; set `hw.initialOrientation=portrait`
  in the AVD `config.ini`). The taskbar can't be hidden via `policy_control`; app content
  lays out **above** that inset, so **chop a fixed bottom band (~130px) in post** — removes
  the dock without losing UI. Gboard may crash-loop on the tablet AVD ("Gboard keeps
  stopping") → dismiss + `pm disable-user com.google.android.inputmethod.latin`.
- **Clean status bar:** iOS `xcrun simctl status_bar <dev> override --time 9:41 --batteryState charged --batteryLevel 100 …`; Android SystemUI demo mode (`am broadcast -a com.android.systemui.demo …`). Force dark to match (`simctl ui <dev> appearance dark` / `cmd uimode night yes`).

**Temporary app edits for clean DEV captures (revert after, flag to the user):**
- Silence the dev LogBox toast: `LogBox.ignoreAllLogs(true)` at the entry.
- If a screen lazy-`import()`s a native module whose async chunk doesn't resolve in dev,
  it redboxes — guard/skip that import for captures.
- **Disable any "resume to last route" on launch.** Each per-flow relaunch re-fires it and
  it **hijacks the deep-link nav** (you land on the resumed screen, not the target).
  These are capture-only — revert before shipping the app and tell the user.

## 2 · Frame — the app-store-screenshots editor skill
Don't hand-roll device frames + headline layout. Use the **app-store-screenshots** skill
(`ParthJadhav/app-store-screenshots` on GitHub — the one we used) — it scaffolds a Next.js
+ ShadCN editor that holds a phone/tablet mockup, localized headlines, theme/background,
and a one-click bundle export at every store size.
Drop the raw captures into its `public/screenshots/<platform>/<device>/{locale}/NN.png`,
seed `app-store-screenshots.json` (app name, theme, per-slide localized `label`+`headline`,
`locales`), `bun install && bun dev`. Headlines: one idea per slide, 3–5 words, one
emphasis word, headline in the top ~30–40%. (Installing a third-party skill executes its
code — confirm the source with the user first.)

## 3 · Export — exact store sizes
The editor's **Export bundle** renders every size × locale (`html-to-image` → zip). Automate
it headless so it's a command, not 16 clicks: puppeteer-core + the **system** Chrome (no
Chromium download), set device by editing the project json + reload, click "Export bundle",
capture the zip via CDP `Browser.setDownloadBehavior`. Wire it as `bun export`.

**Sizing quirks (why raw captures aren't enough):**
- **Modern iPhone native ≠ a valid App Store slot.** A 6.3" capture (1206×2622) isn't an
  upload size — scale to **6.9" 1320×2868** (identical aspect → no visible distortion).
- **Android phone is too tall for Play.** 1080×2400 (9:20) exceeds Play's "longest side ≤
  2× shortest". Don't pad (seam on full-bleed game screens) — **frame to 1080×1920** (the
  editor does this). iPad 13" (2064×2752) + Android tablet (after the taskbar chop) are
  valid as-is.
- Feature Graphic (1024×500) is a separate Play asset — the editor has it.

## 4 · Upload — fastlane (no binary)
`eas submit` ships the **binary**; screenshots + listing text go up with **fastlane
deliver** (App Store) / **supply** (Play). Stage the framed shots + parsed listing copy
into fastlane's layout, then run the lane. Same idea as a `make submit`, for media+text.

**Author the listings as one markdown file per locale** (`store-listings/{en,ru,uk,fr}.md`),
NOT straight into fastlane's `metadata/<locale>/*.txt` tree. The MD is the human-editable
source of truth — App Store name / subtitle / keywords / promo / description + Play title /
short / full, all in one reviewable file per language. A small staging script
(`stage-store-assets.mjs`) parses each MD **positionally** (fixed heading order) and writes
fastlane's many tiny per-field `.txt` files + copies the framed shots into
`screenshots/<locale>/` and `metadata/android/<locale>/images/`. One file to translate and
diff per locale beats hand-editing a dozen `.txt` files; keep keys at parity across locales
(same idea as the app's i18n `en/ru/uk/fr` parity).

- **App Store** — `deliver(skip_binary_upload: true, submit_for_review: false, overwrite_screenshots: true, sync_screenshots: true, force: true)` updates the **currently editable / in-flight version** (App Store now needs only iPhone 6.9" + iPad 13" sets). It does NOT submit — the user presses Submit.
- **Play** — `supply(skip_upload_apk: true, skip_upload_aab: true, track: "production")` updates the **main store listing** (title/short/full + phone & tablet shots). `validate_only: true` for a dry run first. Screenshots go in `metadata/android/<locale>/images/{phoneScreenshots,tenInchScreenshots}/`.
- **Auth, no env (preferred):** App Store Connect **API key json file** at a fixed gitignored path → fastlane reads it, **no 2FA**. Fallbacks: `ASC_*` env (CI), then Apple-ID via Keychain (`fastlane fastlane-credentials add` — the **real** password + a cached 2FA session; this is NOT the app-specific password `altool` keeps in Keychain, `deliver`/spaceship can't use that). Play: a service-account json (file or env).
- **Locale codes differ per store** — map your app locales to ASC vs Play codes (e.g. `ru` → App Store `ru`, Play `ru-RU`; `en` → `en-US`/`en-US`). Get it wrong and the upload no-ops or mis-files.
- **`deliver` REPLACES all screenshots for the sizes you send** (`overwrite`/`sync`) — send the full set per display family, not a partial.
- **Submit-for-review is opt-in, default OFF.** Upload-only lands the changes in the editable App Store version / staged Play listing; you press Submit by hand. Gate the actual review submit behind a flag (e.g. `SUBMIT_FOR_REVIEW=1` → a separate `*Submit*` target), so iterating on the listing never trips the review clock. When you DO submit: App Store `deliver(submit_for_review: true)` needs the review questions answered up front (`submission_information` `export_compliance_uses_encryption: false`, `add_id_info_uses_idfa: false`) or it stalls, and `automatic_release` decides auto-vs-manual release on approval; Play's analog is `supply(changes_not_sent_for_review: false)` to send the staged listing changes for review.

## Guardrails
- Capture, frame, export, upload are **separate** — verify each before the next. A scrambled
  capture frames a scrambled slide; a wrong-size export rejects at upload.
- **Validate every PNG** (`magick identify`) — "ok" must mean a fresh, valid frame, not a
  stale/half-written file that happens to be valid.
- **READ the montages** (`magick montage`, downscale tall shots with `sips -Z`). Eyeball
  every screen × locale before declaring done — wrong-screen, wrong-language, and
  mid-transition shots all pass a file check.
- **Revert the capture-only app edits** (LogBox / resume / import skips) and shut the
  sims/emulators down when finished. Tell the user which app-code edits to revert.
- **Never commit store secrets** — API key + service-account json are gitignored.

## Red flags — stop
- Many screens in one Maestro flow → they scramble; one flow per screen.
- Shell screenshot after the Maestro flow → captures home (relaunch); shoot in-flow.
- A "Refreshing…" bar in a shot → Metro reloaded because you wrote into the project; stage to `/tmp`.
- Snaps drifting to home/settings deep into a run → device degraded; stabilize per locale / hard-reboot.
- Uploading raw 1080×2400 phone shots to Play → over 2:1, rejected; frame to 1080×1920.
- Uploading a 6.3" iPhone capture to App Store → not a valid slot; scale to 6.9".
- Reusing vela's Keychain item for `deliver` → that's an `altool` app-specific password; deliver needs an API key or the real Apple-ID password.

## See also
- **[`mobile`](../mobile/SKILL.md)** — the single-device drive loop (deep links, Maestro
  flow files, `--device <udid>`, `sips` crops) + the build/ship gotchas (non-login-shell
  pod builds, Metro `--clear` redbox, magenta SceneKit, stale Android autolinking). This
  skill is the matrix + store layer on top of it.
- **app-store-screenshots** skill (external — `ParthJadhav/app-store-screenshots`, the one
  we used) — the Next.js framing editor + Export-bundle that does stages 2–3 (device frame,
  localized headline, background, and rendering every store size). Drive its export headless
  for stage 3's `bun export`.
