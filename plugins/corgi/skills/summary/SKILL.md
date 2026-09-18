---
name: summary
description: "Use when someone asks where a batch of work stands: \"give me a summary\", \"what's left\", \"status of all the MRs\", \"what did we ship today\", \"recap\", \"which PRs are still open\". Reads the live state of every PR/MR and ticket the session touched and reports it grouped by ticket, with clickable links. NOT a code review (review), NOT a standup post for someone else (standup)."
---

# Where the batch actually stands

## Overview

After a long session the state lives in three places that disagree: what the session
remembers doing, what the forge says now, and what the tracker says now. Auto-merge
fires, someone else merges a branch, a pipeline goes red an hour after it went green.
A summary written from memory is wrong by the time it is read.

So: **re-read the live state, then report it grouped the way the work was asked for** —
one block per ticket, not one flat list of links.

Neighbours:

| Skill | For |
| --- | --- |
| **this one** | the user asking where their own batch stands, now |
| `standup` | a post for the team channel, written for people who were not here |
| `review` | judging the code in a PR, not reporting its state |
| `tracker` | moving tickets, not describing them |

## Guardrails

- **Never report state from memory.** Every row is re-fetched at summary time. "Merged"
  because the session armed auto-merge two hours ago is a guess, and the one row that is
  wrong is the one that matters.
- **Links must be clickable.** Markdown links in a table. A fenced code block renders
  monospace and kills the links — if a user asks for aligned columns, the table is the
  answer that keeps both.
- **Say what is not done.** The open rows and the blocked rows are the reason anyone
  asked. Leading with the merged list buries them.
- **Do not claim a deploy you did not verify.** Merged is not deployed; a manual
  production job is not a deploy. Check, or say "merged, deploy is a manual job".
- **Attribute review findings honestly.** A defect a reviewer caught is theirs, not a
  thing that "was improved" in the passive voice.

## Phase 1 — Gather, live

Collect in one pass, in parallel where the tool allows:

| Source | What to read |
| --- | --- |
| forge | per PR/MR: `state`, head `pipeline.status`, `reviewers`, `merge_when_pipeline_succeeds`, unresolved discussion count |
| tracker | per ticket: current status, assignee |
| the session | which PR/MR belongs to which ticket, and what was deliberately left out |

Sources:
- **GitLab** — `glab mr view <n> -R <repo> -F json`; threads via `…/discussions`.
- **GitHub** — `gh pr view <n> --json state,statusCheckRollup,reviews,reviewRequests`.
- **Tracker** — the `tracker` skill's read path.

A pipeline reading `success` while a job inside it failed under `allow_failure` is
still worth a word; so is an MR whose auto-merge was dropped by a later push.

## Phase 2 — Group by ticket, not by repo

One block per ticket, headed with its key and a short plain title, then a table of the
repos that carry it. A ticket spanning four repos is one block with four rows — that is
the shape the work actually has, and a flat list hides which repo is holding it up.

```markdown
## [ABC-123] Short plain title — DONE ✓

| Repo | MR | Reviewer | State |
|---|---|---|---|
| api | [!42](https://…/merge_requests/42) | Sam | MERGED ✓ |
| web | [!37](https://…/merge_requests/37) | Alex | OPEN — auto-merge armed *(waiting on CI)* |
```

Rules for the table:
- **MR/PR is a link**, written `[!42](url)` / `[#42](url)`.
- **State carries the nuance** — `MERGED ✓`, `OPEN — auto-merge armed`, `OPEN — 2
  unresolved threads`, `OPEN — pipeline red`, `CLOSED *(superseded by !44)*`. The
  parenthetical goes in italics in the cell; never in a trailing note the eye skips.
- **Reviewer is who is actually on it**, or `—`. Not who you hoped would take it.
- Mark the ticket's own outcome in the header (`— DONE ✓`) only when the tracker says so.

Then a one-line tracker roll-up under the blocks:

```
Tickets: ABC-123 → Done · ABC-124 → In Review
```

## Phase 3 — Say what mattered

After the tables, at most a few short sections. Include a section only when it changes
what the reader does next:

- **What still needs them** — open decisions, missing credentials, a manual job someone
  has to play, a question waiting on a person. This goes near the top if anything is in it.
- **What a review caught** — defects found in the session's own work, in one line each.
  This is the highest-signal part of most summaries and the easiest to omit out of
  vanity; include it, credited to whoever found it.
- **Anything deliberately not done** — scope consciously left out, with the reason. A
  reader who does not see it assumes it was forgotten.

Skip praise, skip restating each diff, skip a "next steps" list that repeats the open
rows. If everything merged and nothing needs them, the summary is the tables and one line.

## Phase 4 — Check it before sending

- Every link opens the thing it names.
- Every state matches what was fetched this minute.
- The open and blocked rows are visible without scrolling past the merged ones.
- Nothing claims a deploy, a release or an approval that was not observed.

## Red flags — stop

- A row whose state came from what the session did, not from a fetch → re-read it.
- The whole summary in a fenced code block → links are dead; use a table.
- Merged rows first and the blocker at the bottom → reorder.
- "Improved", "cleaned up", "fixed some issues" → name the defect or drop the line.
- A ticket marked Done in the summary but not in the tracker → one of them is wrong;
  find out which before reporting either.

## See also

- `tracker` — reading and moving ticket state.
- `standup` — the same facts written for a team channel.
- `stories` — produces the batch this reports on; its Phase 6 report is this format.
- `ship` — release state, when the question is "is it out" rather than "is it merged".
