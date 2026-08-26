---
name: design-parity
description: Use when a change has a design behind it and someone must prove the build matches — "compare it with the design", "does this match Figma", "check it against the mockup", "download the designs", "side-by-side with the design", "design review of this screen", "did we build what was designed", "pixel check", "the designer says it's off". NOT the device-driving loop itself (the `mobile` skill) or store marketing screenshots (`mobile-screenshots`).
---

# Prove the build matches the design

## Overview
"Implemented from the design" is a claim; the evidence is **design and app, same screen,
side by side, read by you**. Every deviation found this way was invisible in the code
review that preceded it. Loop: **pull the design → make the feature reachable → capture the
same states → compose + READ → deviation table → fix or declare deliberate**.

Applies to any surface with a design behind it — mobile, web, desktop. The capture command
changes; nothing else does.

## 1 · Pull the design to disk, before writing code
- **Extract file + node from the URL** the ticket carries (`…/design/<fileKey>/…?node-id=1-2`
  → node `1:2`). No node id → ask; a whole-file dump is unusable.
- **Metadata first, frames second.** A page's metadata is hundreds of KB of nested nodes —
  parse it for **top-level sections + frame names/ids only** (script it, don't read it
  whole), then export just the frames you need. Reading the raw dump burns the context you
  need for the actual work.
- **Names carry scope.** Sections/frames are usually annotated by the designer — `New`,
  `To change`, `Done`, `UC1: …`, `Old card component`. That annotation tells you which
  screens are in scope and which are the *current* build; read it before assuming.
- **Save into the repo**, not `/tmp`: `docs/design/<ticket>/NN-<screen>.png`. They outlive
  the session, reviewers open them from the branch, and the MR can link them.
- **Shoot the section overview too** (all frames of a flow in one image) — it shows the flow
  order and the states the designer expects.
- **Crop + upscale the small parts** (a badge, a chip, an icon row) before you build them.
  A 12 px badge decides whether it overlays an icon or takes its own slot — invisible at
  full-frame scale, and it *will* be the first thing the designer flags.

## 2 · Make the feature reachable — or you cannot verify it
A feature behind a flag, or needing data nobody seeded yet, cannot be walked in the app.
In order of preference:
1. **Seed real data** through the admin/API/back office. Best: exercises the real payload.
2. **A temporary local switch** that forces the feature on and fakes the missing values
   (dates, counters, "delivered" states). Fastest way to see real layout with real fonts.
3. **A throwaway preview screen** that mounts the components with fixtures. Last resort —
   proves styling only, never the flow or the gating.

Rules for the switch (2): one obvious constant, a comment saying it must never ship, flip
it to reach the *other* states (empty / done / overdue), then **revert and grep for it
before committing**. A preview constant left `true` ships the unreleased feature to
everyone.

Check what the environment actually holds before planning the run — if the staging/test
data has none of the feature's records, you already know you're on path 2.

## 3 · Capture the same states the design shows
- **One device + one locale** for the whole comparison run; mixing them makes the diff
  meaningless.
- **Full resolution, straight to a file** — never into the transcript. Crop afterwards.
- **Every state in the design, not just the happy path.** A flow typically ships 3–5:
  before / in progress / done / late / locked / empty. The design sheet shows them; a single
  screenshot hides the ones that are broken.
- Save next to the design: `docs/design/<ticket>/app/NN-<screen>.png`, and re-shoot into
  `fix-NN-*.png` after fixes so before/after stays readable.
- Plan the route: a flow can be blocked by an unrelated required step (a variant picker, an
  address, a pickup point). Either walk it once and note the taps, or pick the record that
  skips it.

## 4 · Compose the side-by-side, then actually read it
```
magick design.png -resize x1000 -bordercolor "#ddd" -border 2 /tmp/l.png
magick app.png    -resize x1000 -bordercolor "#ddd" -border 2 /tmp/r.png
magick /tmp/l.png -splice 0x40 -gravity north -annotate +0+8 "DESIGN" /tmp/l2.png
magick /tmp/r.png -splice 0x40 -gravity north -annotate +0+8 "APP"    /tmp/r2.png
magick /tmp/l2.png /tmp/r2.png +append compare/cmp-N-<screen>.png
```
- **Normalize by height**, not width — phone captures and design frames differ in DPI.
- **Read every composite.** A generated file is not a comparison; the finding only exists
  once you looked.
- **Zoom 2–3× for small components.** Badges, chips, counters and icon rows are where the
  differences live.
- Keep the composites in the repo (`docs/design/<ticket>/compare/`) and link the folder from
  the MR — the reviewer reruns your judgement in seconds.

## 5 · Report the deviations as a table
One row per finding: **what the design shows · what the app shows · fix or deliberate**.
- **Fix** anything that is an accident: truncation, a missing date, a clipped chip, a wrong
  icon, wrong copy.
- **Deliberate** is allowed — a design drawn for one platform often can't be transplanted
  (a rich card list where the platform uses compact rows, a web modal where the platform
  pushes a screen). Keep the *information* parity, change the container, and **name the
  divergence in the MR**. A silent divergence reads as a bug to the designer and to QA.
- Re-shoot after the fixes and post the composites; "fixed" without a new capture is the
  same unverified claim you started with.

## What the comparison actually catches
Every one of these passed code review and a green build:
- **Text truncated to one line** — a label sized with a fit-to-content call collapses
  multiline text. Measure with a **width-constrained** fit and set the height from it.
- **A padded label clipped** ("5 da…") — padding/inset implementations often aren't included
  in the intrinsic size; add the inset by hand or the chip cuts its own text.
- **An empty interpolation** — "From the " / "publish X from  to get…" when the date or
  value is nil. Hide the **whole line/clause** when the value is missing, and keep a
  variant string for it; never format an empty placeholder into a sentence.
- **An icon invisible on a coloured surface** — same-colour asset on a same-colour header.
  Template-tint it, or ship the inverse asset.
- **A decorative marker given its own slot** instead of overlaying the element it qualifies
  (design usually anchors it to the icon's corner).
- **A primary action in the wrong place** — pinned bottom bar in the design, inline under
  the content in the build. Placement is part of the design, not a detail.
- **A drop zone / empty state drawn solid** where the design is dashed, or with a plain
  glyph where the design has a filled button.

Generic rules that fall out of the list: measure text with the real width; drop empty
clauses instead of formatting blanks; tint icons for their surface; anchor markers to what
they qualify; treat CTA placement and empty-state styling as design, not decoration.

## Copy is part of the design
The strings in the mockup are the spec — verify them the same way, and ship them properly:
- **Add keys in the source language first**, then translate **before** pushing to the
  translation platform. Most push commands are configured to overwrite the platform's
  translations with the file's content, so pushing an untranslated file wipes good
  translations with source-language text.
- **Push only the new keys** — build a partial file of just those keys, with its own
  config, instead of shipping the whole catalogue up. Smaller blast radius, reviewable.
- **Read the CLI's flags before running.** Translation CLIs carry destructive options —
  a "cleanup" that deletes every key not in the upload is common, and short flags collide
  (`-c` may be `--cleanup`, not `--config`). Spell out the long flag.
- **Verify from the platform's API afterwards** — key exists, translation per locale, tag
  applied. "Upload succeeded" is not "the key is there in six languages".
- **Match the product's register** — formal vs informal per locale — by reading how the
  neighbouring keys are already translated, not by choosing per string.
- Keep placeholders identical across locales (`%@` / `%d` / `{name}`); a dropped one
  crashes or prints raw at runtime, in the one language nobody tests.

## Guardrails
- **The design is the source of truth for what is on screen** — behaviour comes from the
  ticket, layout/copy comes from the design. Disagreement between them is a question for
  the author, not a decision to make silently.
- **No claim without a composite you read.**
- **The verification switch never lands in the branch** — revert + grep before commit.
- **Never post a partial pass as "matches design"** — say which screens/states you compared
  and which you could not reach, and why.
- **No AI attribution** anywhere the work surfaces — MR body, comments, commit messages.

## Red flags — stop
- "Implemented from the design" and no design file on disk → pull the frames first.
- Reading a whole design-file metadata dump into context → parse for frame names/ids only.
- Comparing from memory of the mockup → open both images side by side.
- One happy-path screenshot for a flow the design draws in five states → shoot the states.
- A composite generated but never opened → it proved nothing.
- Verifying a flagged feature "later, once the flag exists" → temporary switch now, real
  data when it lands; otherwise the design gap ships.
- A preview/mock constant still in the diff at commit time → strip it, grep to be sure.
- A platform-native divergence left unexplained in the MR → name it, or it reads as a bug.
- Chip/label text ending in "…" or a sentence with a double space → sizing and empty-value
  bugs, not fonts.
- Pushing an untranslated catalogue to the translation platform → it overwrites live
  translations; translate first, push only the new keys.
- Trusting an upload's success line for copy → query the platform API for the keys.

## See also
- **[`mobile`](../mobile/SKILL.md)** — the device drive loop (deep links, flows, capture)
  and the platform gotchas. This skill is the design-comparison layer on top of it.
- **[`mobile-screenshots`](../mobile-screenshots/SKILL.md)** — the store-screenshot matrix
  (framing, sizes, upload). Same capture discipline, different output.
- **[`review`](../review/SKILL.md)** — when reviewing someone else's PR/MR that carries a
  design link, the composites are the evidence to ask for.
- **[`stories`](../stories/SKILL.md)** — a story whose ticket links a design is not "done"
  at green tests; it is done at a read composite.
