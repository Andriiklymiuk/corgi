---
name: before-after
description: "Use when a visual change should be proven against the state it replaced: \"screenshot comparison\", \"before and after\", \"show me the before/after\", \"prove the UI changed\", \"attach screenshots to the PR\", \"do a visual diff of my change\". Captures the same screen on the base branch and on yours and puts both in the PR/MR. NOT a design comparison (design-parity), NOT store marketing shots (mobile-screenshots), NOT a pixel-diff assertion in CI (the repo's own visual harness)."
---

# Before and after, from the branch that changed it

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** before.png and after.png of the same screen are captured, read by you, and in the PR/MR body.

## Overview

A UI diff says what the code does; it never says what the screen looked like before. The
reviewer either trusts the description or builds the base branch themselves — and nobody
builds the base branch. So the change ships on a claim.

This skill produces the evidence: **the same screen, same device, same data, captured
twice — once on the base branch, once on yours — attached to the PR/MR.**

```
base branch ──▶ build ──▶ launch ──▶ navigate ──▶ capture  before.png
                                                    │
your branch ──▶ build ──▶ launch ──▶ navigate ──▶ capture  after.png
                                                    │
                                            upload ──▶ PR/MR body
```

Neighbours, so you pick the right one:

| Skill | Compares your build against |
| --- | --- |
| **this one** | **the state it replaced (the base branch)** |
| `design-parity` | a design — Figma, a mockup, a prototype |
| `mobile-screenshots` | nothing; it produces store marketing assets |
| the repo's visual harness | a committed baseline, as a CI assertion |

## Guardrails

- **Same everything except the branch.** Same device, same account, same locale, same
  theme, same data row, same scroll position. One variable changes: the code. A "before"
  taken on a different record or a different simulator proves nothing and will be
  believed anyway.
- **Capture the base branch FIRST or LAST, never from memory.** If you already have the
  after, build the base and take the before — do not describe the old state from the
  diff. The whole point is that the diff does not show it.
- **Never invent a before.** No mockup, no hand-drawn approximation, no screenshot of a
  different app version pulled from a ticket. If the base cannot be built or reached,
  say so in the PR and attach the after alone.
- **Restore the branch.** Always return the working tree to the branch you started on,
  including on failure. A base-branch checkout left behind is how the next command
  builds the wrong thing.
- **Scratch files go to the session scratchpad**, not `/tmp` — the host names it in the
  environment. Shared `/tmp` leaks between sessions and users.
- **No secrets in an image.** Read every shot before uploading: tokens in a debug
  banner, a real user's name and address, an internal URL. An upload is public to
  everyone who can read the PR, and deleting the PR comment does not unpublish it.

## Phase 0 — Decide it is worth it

Run this when the change is **visible and the reviewer cannot see it from the diff**:
layout, spacing, colour, a component swapped, copy in situ, an empty state, a new
control. Skip it for logic, API shape, tests, config, or a copy change whose string is
right there in the diff.

Cost is two builds. On a native app that is real time — say so and get a yes before
starting, unless the user already asked for the comparison.

## Phase 1 — Find the screen, on your branch

Get to the screen **once**, on the branch you are on, and write down the exact route:
launch → the taps → the scroll. That route is replayed verbatim on the base build, so
record it as steps, not as a memory.

Reaching it:
- **Deep link** where the app has one — fastest and the most reproducible.
- **Driver** (`mobile` skill: Maestro, argent, Playwright) for the tap sequence. Prefer a
  flow file over ad-hoc taps: the flow is what makes the second capture identical.
- **Seed the data first** if the screen needs a particular record, and note which one.
  "an order with a discount" is not reproducible; `Test order #1 (discount)` is.

A screen behind a feature flag needs the flag **on in both builds**. Check it rather
than assume — a flag that defaults off gives you two identical "before" shots and a very
confusing hour.

## Phase 2 — Build the right thing

This is where the time goes, so get it right once.

- **Build the target the app actually ships**, not the first one that compiles. On iOS a
  scheme and a configuration are different axes: `-scheme App -configuration Staging` can
  produce a binary with the production bundle id and none of the staging resources, which
  then crashes on launch with a nil unwrap in `didFinishLaunchingWithOptions`. Use the
  dedicated scheme (`App Staging`) and confirm `PRODUCT_BUNDLE_IDENTIFIER` and
  `FULL_PRODUCT_NAME` with `-showBuildSettings` before installing.
- **Confirm which environment the build points at** before logging in. A build aimed at
  production means a real account and real data in the screenshot.
- **Check what the launch actually did.** A process id from `simctl launch` is not a
  running app. Screenshot it; if you land on the home screen, read the crash report
  (`~/Library/Logs/DiagnosticReports/`) rather than relaunching and hoping.
- **Long builds go in the background** so the session keeps working, and the result is
  read from the task output rather than guessed.

## Phase 3 — Capture both states

1. Capture the current branch → `after.png`.
2. Stash or commit, check out the base (`git checkout <base>`), rebuild, reinstall.
3. Replay the **same** route from Phase 1 → `before.png`.
4. Return to your branch. Verify with `git branch --show-current`.

Frame both shots the same way: the panel that changed should occupy the same part of the
screen in each. A before framed on a different scroll offset reads as a bigger change
than it is, which is its own kind of lying.

Name them `before.png` / `after.png`. If the change spans screens, suffix them —
`before-orders.png` — and keep the pairs adjacent in the table.

## Phase 4 — Read them yourself

Open both. Confirm the thing you changed is the thing that differs, and that nothing
else moved. Two failures this catches:

- **The change is not visible** — a flag is off, the build is stale, or you captured the
  same branch twice. Two identical images attached to a PR claiming a redesign is worse
  than no images.
- **Something else moved** — a version banner, a date, an unrelated regression you just
  introduced. The second one is a finding, not a framing problem.

## Phase 5 — Attach them

Upload to the forge, then reference them in the PR/MR body. Never link a local path and
never paste a `file://` URL.

**GitLab** — `glab api` uploads with `--form`, not `--field`:

```bash
glab api --method POST "projects/<url-encoded-path>/uploads" --form "file=@<path>/before.png"
```

`-F` is `--field` and sends form *fields*; it returns `400 Bad Request` on a file and
looks like a permissions problem, which sends you hunting for a token you do not need —
the CLI's own auth is fine. The response carries `markdown` (`![before](/uploads/…)`);
use that string verbatim. Then `glab mr update <n> --description "$(cat body.md)"`.

**GitHub** — `gh` has no upload endpoint. `corgi assets push before.png after.png
--key <key> --dir <repo>` commits them to the repo's long-lived `pr-assets/<key>`
branch and prints the `https://github.com/<owner>/<repo>/blob/pr-assets/<key>/<path>?raw=true`
markdown to paste — details in `../_shared/forge-commands.md` §6a. That is the one link form a private
repo renders: `raw.githubusercontent.com` shows an empty box there, and an image
committed on the PR branch dies with the branch after the merge. Do not invent a host.

Put them in a table so they sit side by side, with one line saying what to look at:

```markdown
## Before / after

Captured on the iOS simulator, staging, same record and scroll position.

| Before (`master`) | After (this MR) |
| --- | --- |
| ![before](/uploads/…/before.png) | ![after](/uploads/…/after.png) |

Left: plain heading, outlined circles, one sentence per step. Right: bordered panel,
filled header bar, bold title over grey subtext.
```

Name the capture conditions — device, environment, account class, locale. A reviewer who
cannot tell where a shot came from cannot use it, and a shot from an unnamed environment
invites the "was that production?" question.

## Phase 6 — Report

State what was captured and on what, that both were read, and anything the comparison
surfaced beyond the intended change. If only one side exists, say which and why — an
after-only PR with an honest note beats a fabricated before.

## Red flags — stop

- The before came from the ticket, a mockup, or your description of the old code → that
  is not a before. Build the base.
- Two shots that look identical → you captured the same build twice, or the flag is off.
  Do not attach them.
- The before is on a different record, device, locale or scroll position → recapture.
- Uploading without reading the image first → check for tokens, real names, internal URLs.
- A `raw.githubusercontent.com` or local-path image link in the body → an empty box on a
  private repo; use the blob `?raw=true` form (§6a) and verify the file sits at that ref.
- Left on the base branch after capturing → return to your branch before anything else.

## See also

- `mobile` — driving the device: deep links, Maestro, simulator lifecycle.
- `design-parity` — when the target is a design rather than the previous build.
- `mobile-screenshots` — the store matrix, framed and localized.
- `stories` — calls this during verification when the change is visual.
- `review` — offers it when a UI diff arrives with no visual evidence.
