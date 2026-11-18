# Proof for a screen, a mobile app or a webhook

Part of Phase 3 of `stories`: read before the per-service gate when the story changes something a person sees, runs on a phone, or is called by an outside provider. Each item is one of the gate's rules.

- **Screenshots - tool you reach for, not box to tick.** Decide once:
  1. **User asked** ("with screenshots", "attach screens", "show me how it looks")
     → capture, attach to **ticket** (Linear/Jira, through tracker's own
     upload - §6a); where asked-for screenshot goes when no place named.
     Put in **PR body when user said PR** ("screenshots in the PR",
     "include them in the PR"), both when asked for both.
  2. **Nobody asked, but look helps you test** (layout fix jsdom can't see,
     native control, design reference to compare, Expo flow driven on
     sim) → take it, **read it**, verify - what drivers below are for.
     Keep as working file by default, say "verified on <device/browser>" in
     report; shot genuinely helps reviewer (shows one thing diff can't) → attaching fine - judgement call, not requirement.
  3. **Neither** → nothing to capture, no apology. Api-only, config, CI,
     test-only, most logic changes have nothing to show; "a screen changed" not by itself reason to shoot every state.
  Don't shoot terminal, don't stack screens to look thorough. When capturing,
  pick driver in order, first present wins:
  1. **argent MCP** (`mcp__argent__*` tools in tool list; `argent --version` on
     PATH) - iOS simulator, Android emulator, or Chromium page over CDP (web
     service in browser started with `--remote-debugging-port`): `list-devices`,
     `launch-app` / `open-url`, `describe` for tap coordinates, `gesture-tap`,
     `screenshot`. Every action returns screen after it, so no flow file, no
     guessed coordinates. Read argent rule/skill it ships before first call.
  2. **Maestro** (`mobile` skill; its MCP when connected) - flow file with
     `takeScreenshot` inside flow, `--device <udid>`.
  3. **Repo's web harness** (Playwright / Cypress screenshot) for browser
     surface with no argent; else headless browser (`npx playwright screenshot`,
     `chrome --headless --screenshot`).
  4. **Bare capture** last - `xcrun simctl io <udid> screenshot`, `adb exec-out
     screencap -p`.
  Whatever captured: log in as account class story is about, seed
  state ticket describes, **read each image** before attaching or concluding.
  **Asked-for** set (trigger 1) complete: every state spec names (empty /
  in progress / done / error) and every rendered side of multi-service story - web or admin console showing same record gets shot too. **Verification** shot (trigger 2) covers one spot change touches, nothing
  more. Name each file `<n>-<what>.png`. Attaching: ticket through tracker's own
  upload, PR per Phase 5 (§6a link form). No driver, no device (host with no
  simulator or browser) → repro steps in spec and PR instead; say so in
  report when user asked for shots.
- **Ticket links design (Figma / mockup) → story done at READ side-by-side**,
  not green tests. Pull frames into `docs/design/<ticket>/`, capture same states
  from app, compose labelled design-vs-app images, put deviation table (fixed vs
  deliberate divergence) in PR/MR - table = record; composed images
  go with it per screenshot bullet above (ticket when user asked for
  screenshots, PR when they said PR, else your call). Feature still behind flag or without seeded data
  forced on with temporary, clearly-marked local switch - reverted before commit. Full
  loop: `design-parity` skill.
- **Expo / React Native service → verify on simulator, not just jest**
  (`references/expo-verification.md`). Detect: `package.json` depends on `expo`
  (or `react-native` + `ios/`/`android/`). Jest-green not done: Metro
  interop, native modules, permissions/entitlements, visual layout only
  fail on device. Native-scoped change (new native dep, `app.json`
  plugins/permissions) → rebuild dev client (prebuild → `LANG=en_US.UTF-8 pod
  install` → xcodebuild), re-install; JS-only → existing build + Metro
  reload. Drive changed flow with driver screenshot bullet picked -
  argent MCP when present, else **Maestro** (flows in `e2e/`, committed with
  PR) - confirm by reading screen driver returns (screenshot bullet's
  trigger 2: verification, not attachment), watch Metro log for runtime
  errors. Multi-device features (P2P/LAN): clone simulators - share
  host's network/Bonjour, so real radio path testable. Use
  `expo:*` plugin skills for SDK-specific guidance when installed;
  same-plugin **`mobile`** skill = canonical device-driving loop + build
  gotchas (Maestro flow-as-file + `--device`, non-login `LANG` pod builds,
  `--clear` redbox, apple-targets extension creds + App Group, stale autolinking,
  tee/exit-code masking) - invoke for VERIFY. **Never run its
  local-build → TestFlight/Play *ship* from story** - shipping separate,
  owner-approved step, only when ticket/spec explicitly calls for release.
  Non-macOS host (no simulator) → fall back to visual-bug manual-only
  path: spec + PR carry repro steps; say so in report.
- **Frontend change with design reference → screenshot + compare, not just
  green tests.** Spec recorded reference (Phase 1: screenshot, mockup, design
  export, prototype-app capture)? Done = rendered UI **matches it**. Capture
  implemented screen (driver order in screenshot bullet above - argent
  MCP, Maestro, repo's web harness, bare capture), **Read reference + capture
  side by side**, compare layout, spacing, colour, typography, icons, copy.
  Bug screenshot = "before" - capture must show fixed at that spot.
  Mismatch → fix, or list as intentional deviation in PR body. Compare
  for you; images attach only per screenshot bullet (user asked → ticket, PR
  when they said PR). Functionally perfect + visually off = not done.
- **Webhook / callback feature** (new inbound endpoint external provider calls -
  Stripe, GitHub, Twilio, e-sign…) → **test with simulated signed payload, not
  live call:** assert signature check + handler behaviour against sample event
  (repeatable, CI-safe). **Don't gate on live delivery** - needs provider config
  draft PR can't assume. Put **live check** in spec's manual-verification +
  PR body: `corgi tunnel <svc>` for public URL, point provider (or CLI,
  e.g. `stripe listen --forward-to <url>`) at it.
