---
name: memory
description: The workspace memory convention for a corgi stack — a committed, shared `.corgi/memory/` store of decisions, incidents, domain facts, and recurring fixes that the suggest/debug/stories/review skills read before acting and append to after a notable outcome. Use when a skill needs to read prior workspace decisions/incidents, when recording why a choice was made or how an incident was fixed, or when a recurring fix should be PROPOSED as a learned skill/template. Read-before-act, confirm-before-write, never stores secrets, never auto-installs anything.
---

# Corgi workspace memory

A corgi workspace remembers things not in git or code: **why** a driver/port/template was chosen, how a past **incident** was fixed, **domain** facts, recurring **fixes**. Store is `.corgi/memory/` — committed, shared by the team via the repo, keyed to the `corgi-compose.yml` stack. This skill is the convention; `suggest`/`debug`/`stories`/`review` reference it.

Workspace-scoped analog of the host harness's per-project memory — that one is private/single-machine; this one is committed/shared/stack-scoped.

## Guardrails (non-negotiable)

- **Opt-in.** No `.corgi/memory/` → reads skip silently (no memory is not an error); writes **offer** to create the store, never force it.
- **No secrets, ever.** Store is committed. Never write a credential, token, API key, password, connection string with a password, private hostname, or PII. Record *that* a secret exists and where it's configured ("import needs `STRIPE_KEY` in the api env"), never the value. `corgi memory lint` fails the store on a key-shaped string.
- **Confirm before write.** Drafting a fact is fine; writing it needs the user's OK (same spirit as the spec sign-off gate). One fact per durable outcome — not chatter.
- **Never auto-install a learned skill or template.** A recurring pattern produces a *proposal* a human approves; this skill never writes an executable skill or edits a plugin/template.

## File format — one fact per file

`.corgi/memory/<type>/<name>.md`, where `<type>` ∈ `decisions | incidents | domain | fixes` (the folder) and `<name>` is unique kebab-case == the filename stem:

```markdown
---
name: postgres-over-mysql
description: Chose Postgres for the main DB over MySQL — JSONB + partial indexes.
type: decision        # decision | incident | domain | fix
service: api          # optional corgi-compose service, or "stack"
created: 2026-06-08
links: ["[[billing-cycle-rules]]"]
pattern:              # fix-type only: the stable key recurrence is counted on
---

Short body. Why, not what. Reference related facts with [[their-name]].
```

- `description` is the one line the index shows and a reader sees first — make it carry the fact.
- `[[name]]` links form the **memory graph**; a link to a missing fact is a lint warning.
- `type` must match the folder. `fix` facts add `pattern:` (the recurrence key).

## The index

`.corgi/memory/index.md` is **generated** (`corgi memory index`) — never hand-edit it. Lists every fact's name + description by type. **Read the index first** (cheap), then open only the 1–3 fact files whose description matches your task. Don't dump the whole store into context.

## Read-before-act (suggest / debug / stories)

If `.corgi/memory/` exists, before acting:
1. `corgi memory list --json` (or read `index.md`) — scan descriptions.
2. Open only the matching facts.
3. Use them: **suggest** — don't propose what a `decision` rejected; cite a `domain` fact / past `incident` as evidence. **debug** — check `incidents/` for "seen this before?" and reuse the recorded fix. **stories** — honor `decision` constraints, reuse an `incident` fix for a regression, ground a free-text feature in `domain`.

## Confirm-before-write (stories / review)

After a notable, durable outcome, draft a fact and **show it for confirmation**:
- **stories** — a non-obvious bug root cause that could recur → `incident` (+ a `fix` with a `pattern:` if it's a code-level pattern); a cross-service contract choice → `decision`.
- **review** — a reviewer thread that settles a durable convention (Mode B, after the thread resolves) → `decision`.

On OK: `corgi memory add --type <t> --name <slug> --desc "<one line>" [--service <s>] [--pattern <key>]` scaffolds the file, then `corgi memory index` refreshes the index, then run `corgi memory lint` (must pass — catches secrets / broken links). Commit the new fact with the related change. No `.corgi/memory/` yet → offer to create it (`corgi memory add` creates the dir on first use); declined → skip, don't block.

## Learned skills — detect, PROPOSE, never install

After writing a `fix` fact, check recurrence:
1. `corgi memory list --type fix --json` → count by `pattern`.
2. A `pattern` seen **≥ 3 times** (default threshold) is a learned-skill candidate.
3. Write a **proposal** to `.corgi/memory/proposals/<date>-<pattern>.md`:

```markdown
---
name: retry-on-429
kind: skill        # skill | template
count: 3
sources: ["[[retry-on-429-billing]]", "[[retry-on-429-search]]", "[[retry-on-429-webhook]]"]
created: 2026-06-08
---

## Recurring pattern
We've added the same exponential-backoff-on-429 wrapper in 3 services (links above).

## Proposed remedy
A project skill `retry-on-429` (or a corgi template) that documents/scaffolds the
shared backoff helper so the next service reuses it instead of re-deriving it.

## Approval (human ticks to accept — nothing auto-installs)
- [ ] This pattern is real and worth standardizing
- [ ] Author it as a: ( ) project skill  ( ) corgi template
- [ ] Owner: ____   Target repo/path: ____
```

4. **Stop there.** Print the proposal path. Do **not** create a skill file, edit the plugin, or add a template — a human approves and authors it as a separate explicit task. Proposals are committed so the team sees the candidate.
