---
name: web
description: "Use to verify a web change in a real browser: \"check it in the browser\", \"does the page render\", \"click through the flow\", \"test the form\", \"is the console clean\". Opens the running stack, drives the page, reads the screenshot, the console and the network. NOT a phone or simulator (mobile), a design match (design-parity) or store shots."
---

# Verify a web change in a browser

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the changed page was driven in a browser on the running stack, you read its
screenshot, and the console and the failed requests are reported, at a phone width too
when the layout changed.

A green build and a passing unit test do not show that a page renders. The page does.

```
stack up ──▶ open the route ──▶ drive the flow ──▶ screenshot ──▶ read it
                                      │
                                      └──▶ console + network ──▶ report
```

## 1. Get the page up

- `corgi status --json`: the web service and what it calls are healthy. Not up → the
  `run` skill, then come back. `corgi open <service> --json` prints the URL.
- Go straight to the route that changed. A page behind a login uses the repo's seeded
  test user (README, seed script, `.env.example`). No seeded user → ask once for a test
  account. Never type a real person's password, and never put one in the chat.
- A flag that hides the feature is turned on locally for the run and turned off before
  the commit. Search for it before you commit.

## 2. Pick the driver

First match wins. `../_shared/session-tools.md` has the whole table.

1. **Chrome tools** (`mcp__claude-in-chrome__*`): the person's own Chrome, a new tab.
   Best when the page needs their login or an extension.
2. **argent on a Chromium target**: `describe` gives the element tree with frames, so a
   click never comes from guessed pixels.
3. **Playwright**: `npx playwright screenshot --viewport-size=390,844 <url> <file>` for
   one capture; a short script for a flow. The repo's own e2e suite beats a new script:
   `corgi test --e2e` when the compose file has an `e2e:` block.
4. **None of them**: `curl -sS -o /dev/null -w '%{http_code} %{time_total}s\n' <url>` and
   the HTML. Say in the report that no browser ran.

Do not trigger a JavaScript `alert`, `confirm` or `prompt`: a browser dialog blocks the
driver. A page that opens one is tested around it, and the report says so.

## 3. Drive it like a person

- **The path the ticket names, start to end**, then one wrong input: empty, too long, a
  double click on the submit.
- **Wait on a condition**, never on time: an element that appears, a request that ends.
- **A write is checked twice.** After a save, reload the page. The value that survives
  the reload is the proof; the first frame can be optimistic.
- **Every state the change can show**: loading, empty, error, full. An error state is
  forced with a stopped service (`corgi stop --service <svc>`), then started again.
- **Two widths when the layout changed**: about 390 and 1280. Dark mode when the app has
  one. A second browser only when the change uses a new CSS or JS feature.

## 4. Read what the browser says

- **The screenshot.** Open it. Never write "renders fine" about a picture you did not read.
- **The console.** An error or a new warning that your change causes is a finding. One
  that was there on the base branch is named as old, not fixed in silence.
- **The network.** A 4xx or 5xx, a request sent twice, a request that never ends. Match
  it in `corgi logs --service <api> --grep '<path>'`.
- **Keyboard only, once**: Tab reaches the new control, Enter or Space works it, focus
  is visible. A control with no name for a screen reader is a finding.

## 5. Report

```
page      /settings/billing on web (localhost:3000), 390 and 1280 wide
flow      change plan → confirm → reload: plan kept
console   0 errors; 1 old warning (React key, on base too)
network   PATCH /api/plan 200 in 180 ms; no failed request
not run   Safari, a real phone
```

Attach the screenshot where the reviewer reads it (`before-after` for a visual change on
a PR). A regression worth keeping becomes a test in the repo's e2e suite, in the same PR.

## Red flags

- "Should render" with no browser run.
- A screenshot taken and not opened.
- A fixed `sleep` before a capture.
- A login typed with a real person's password.
- The same failing click sent again: read the element tree, then change the call.

## See also

- **[`mobile`](../mobile/SKILL.md)** for a phone or a simulator, **[`before-after`](../before-after/SKILL.md)** for proof on the PR.
- **[`design-parity`](../design-parity/SKILL.md)** to match a design, **[`perf`](../perf/SKILL.md)** when the page is slow.
- **[`debug`](../debug/SKILL.md)** when the request fails on the server.
