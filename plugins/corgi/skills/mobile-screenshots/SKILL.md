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
- **If the locale store IS host-writable (AsyncStorage/zustand-persist), bake the locale
  into the relaunch instead.** A FRESH mount renders **native tab-bar labels** (`NativeTabs`)
  in the target language; an in-app switch updates the JS content but leaves NativeTabs'
  **cached native labels stale** (mixed-language tab bar in the shot). Set the persisted
  locale key in the same manifest patch as the seed (below), then relaunch.
- **Persisted game state needs recreating per locale.** A life counter / saved match keeps
  the player names from when it was first created → shows the wrong language. Recreate it
  in-flow (open the mode → start a fresh game) so names localize; dismiss any first-run
  icon guide ("Got it" / localized).
- **First-run blocks.** Fresh installs show onboarding + per-feature guides → skip them
  ("Skip" / localized) once. Watch out: onboarding often contains the **app name**, so a
  home-anchor `extendedWaitUntil` *false-passes* on it — skip onboarding before relying on
  the anchor.

**Seed realistic content host-side, not via an in-app route.** Empty apps shoot empty (a
tracker's blank calendar, a zero-state dashboard). The tempting fix — a throwaway
`<scheme>://seed` route that fills the store — fails on real apps: a root `NativeTabs` (or
fixed `Stack`) has **no container to render an ad-hoc route**, expo-router **excludes
`_`-prefixed files** from routing, and a route added mid-session only registers after a
Metro **cold restart**. Patch the persisted store from the host instead, then relaunch:
- zustand-persist / AsyncStorage apps store values **inline as JSON strings** in
  `…/Library/Application Support/<bundle>/RCTAsyncLocalStorage_V1/manifest.json` (each key
  + a `<key>__mtime`). Read it, overwrite the data / theme / locale blobs, write back, then
  `simctl terminate` + `launch` so the store rehydrates.
- **Seed time-relative data into the visible window** — an entry on the **1st of the current
  month** lands on-screen on the calendar, not off in history. Bake the **brand appearance**
  (light/dark + accent) into the theme blob so the deck matches the palette regardless of
  the device setting.
- MMKV-backed stores aren't host-writable → fall back to driving the UI with Maestro to
  create the data.

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

**Expo dev-client (dev build) gotchas:**
- **Hide the floating "Tools" FAB or it lands in every shot.** Add
  `["expo-dev-client", { showFloatingButton: false }]` to `app.config` plugins, and/or
  toggle **"Tools button"** off in the dev menu (persists per install — do it once before
  the run; survives relaunch).
- **Load the build from Metro deterministically:** `simctl openurl <udid>
  "<scheme>://expo-development-client/?url=http%3A%2F%2Flocalhost%3A8081"`, then tap the
  SpringBoard **"Open?"** confirm (Maestro `tapOn` the localized "Open"). Plain
  `simctl launch` **reuses the cached bundle** — it won't pick up a new route / fresh JS.

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
- **Keychain beats a plaintext key file — but store it base64.** Safer than a gitignored `.p8`/json on disk: keep the key in the macOS Keychain, inject per-run (`ASC_KEY_CONTENT` env + `is_key_content_base64: true`). **Store base64, NOT the raw PEM** — `security -w` returns a multi-line PEM as HEX, and `app_store_connect_api_key` then dies `invalid curve name (OpenSSL::PKey::ECError)`. `base64 <key.p8 | tr -d '\n'` stays single-line printable → round-trips clean. The key is **account/team-level** (one issuer per team) → store ONCE; one shared Keychain item serves every app in the team. `issuer_id` is the UUID at the top of ASC → Users and Access → Integrations → App Store Connect API, **not** the key's name.
- **Locale codes differ per store** — map your app locales to ASC vs Play codes (e.g. `ru` → App Store `ru`, Play `ru-RU`; `en` → `en-US`/`en-US`). Get it wrong and the upload no-ops or mis-files.
- **`deliver` REPLACES all screenshots for the sizes you send** (`overwrite`/`sync`) — send the full set per display family, not a partial.
- **Submit-for-review is opt-in, default OFF.** Upload-only lands the changes in the editable App Store version / staged Play listing; you press Submit by hand. Gate the actual review submit behind a flag (e.g. `SUBMIT_FOR_REVIEW=1` → a separate `*Submit*` target), so iterating on the listing never trips the review clock. When you DO submit: App Store `deliver(submit_for_review: true)` needs the review questions answered up front (`submission_information` `export_compliance_uses_encryption: false`, `add_id_info_uses_idfa: false`) or it stalls, and `automatic_release` decides auto-vs-manual release on approval; Play's analog is `supply(changes_not_sent_for_review: false)` to send the staged listing changes for review.
- **"Invalid screen size" on a size you KNOW is current = stale fastlane.** New device slots ship in `deliver` releases; an old fastlane rejects a valid current size (e.g. iPhone 6.9" 1320×2868) it doesn't recognize. Upgrade fastlane before doubting the shot.
- **Don't push the App Store NAME by default.** The title is globally unique + account-locked; `deliver` setting `name` fails `the app name ... already being used ... on a different account - /data/attributes/name` if anyone else owns it. Gate the `name.txt` write behind a flag (`ASC_SET_NAME=1`); set the title in App Store Connect directly. Subtitle/keywords/promo/description still upload.
- **A brand-new app's FIRST version "No data"-crashes the metadata pass.** `deliver` calls `fetch_app_store_review_detail`, which raises `No data (RuntimeError)` before screenshots upload until the review-detail object exists. Fill **App Review Information** in ASC once, OR push screenshots-only with `skip_metadata: true`.
- **`sync_screenshots` is beta-gated** — `deliver` refuses it unless `FASTLANE_ENABLE_BETA_DELIVER_SYNC_SCREENSHOTS` is set; export it in the lane.
- **Listing fields have hard length caps — validate before upload.** App Store: name/subtitle ≤30, keywords ≤100, promo ≤170 chars; `deliver` aborts `An attribute value is too long ... cannot be longer than 100 characters - /data/attributes/keywords`. Localized listings carry localized field *labels*, so measure **positionally** (the Nth App Store block), not by the English label — else non-`en` locales slip past unchecked.
- **A 500 loop on screenshot upload is ASC being flaky, not you.** `deliver` repeats `Waiting for screenshots to appear ... Server error got 500`; name + metadata already committed, so just re-run screenshots-only later (deliver itself notes only a 503 self-recovers).
- **Staging App Review Info via fastlane writes the phone (PII) into `metadata/review_information/` — keep that dir out of git** (pull the phone from the Keychain, not a committed file). Watch the trap: `.gitignore` has **no inline comments** — a `path/ # note` line silently fails to ignore, leaking the file; put the comment on its own line.

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
- Reusing an `altool`/Transporter app-specific password from Keychain for `deliver` → deliver/spaceship can't use that; it needs an API key or the real Apple-ID password.
- `invalid curve name` from `app_store_connect_api_key` → Keychain handed the PEM back as HEX; store the key **base64** + `is_key_content_base64: true`.
- `deliver` "Invalid screen size" on a current slot → stale fastlane; upgrade it.
- `deliver` "app name already used on a different account" → stop pushing `name`; set the title in ASC.
- `No data (RuntimeError)` on a first-ever version → fill App Review Information in ASC, or `skip_metadata` for screenshots-only.
- `deliver` "attribute value is too long / cannot be longer than N characters" → a listing field over cap; trim it and re-check EVERY locale positionally (localized labels hide the overflow).
- `deliver` looping `Server error got 500` on screenshots → transient ASC; re-run later, don't touch config.
- `.gitignore` line with an inline `# comment` → git ignores nothing on it; a staged secret/PII (App Review phone, keys) sneaks into the commit. Comment on its own line.

## See also
- **[`mobile`](../mobile/SKILL.md)** — the single-device drive loop (deep links, Maestro
  flow files, `--device <udid>`, `sips` crops) + the build/ship gotchas (non-login-shell
  pod builds, Metro `--clear` redbox, magenta SceneKit, stale Android autolinking). This
  skill is the matrix + store layer on top of it.
- **app-store-screenshots** skill (external — `ParthJadhav/app-store-screenshots`, the one
  we used) — the Next.js framing editor + Export-bundle that does stages 2–3 (device frame,
  localized headline, background, and rendering every store size). Drive its export headless
  for stage 3's `bun export`.
