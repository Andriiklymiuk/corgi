---
name: idea
description: "Use when the user wants a page written and kept for something to build or decide - \"write a spec for X\", \"draft an HLD for the payments split\", \"RFC: move auth to the gateway\", \"I wanna make a scheduling app for a coffee shop\", \"turn this doc into tickets\" - or when a session is asked to read, answer or act on an existing idea page (`corgi idea …`, a /idea/<slug> link, \"the comments on the spec\"). The page is markdown kept by corgi, commented by people in the browser, VS Code and the phone, answered by the agent, split into tickets, built from or sent to Jira/Linear/Confluence. NOT for implementing the tickets (stories) or for tracker status (tracker)."
---

# Corgi idea: one page per thing to build, kept by the agent

**Done when:** the page exists (or is answered), its split into tickets reads right, and the person has the link.

Read `../_shared/conventions.md` first.

A page is `corgi idea`'s unit: markdown on disk (`corgi idea path <slug>`), comments
anchored to quoted words beside it, a split into tickets in its metadata, and a
link anyone can read and comment on. Types: **idea** (something from nothing),
**spec** (a feature in an existing product), **hld** (a design), **rfc** (a decision).
Everything below works through the CLI (`--json` everywhere) or the MCP tools
`corgi_idea_list`, `corgi_idea_read`, `corgi_idea_new`, `corgi_idea_comment`,
`corgi_idea_edit`, `corgi_idea_resolve`, `corgi_idea_build`, `corgi_idea_send`.

## Job 1 - Write a page

- Pick the type from the words: build / app / product → `idea`; feature / change in
  an existing repo → `spec`; architecture / design / system → `hld`; decide / propose /
  should we → `rfc`. Say which you picked in one clause; do not ask.
- `corgi idea new "<the sentence>" --type <t> [--workspace <id>] --json` from inside
  the workspace when there is one (the agent reads the repository to fit it). A page
  the user already wrote: `--from <file.md>` keeps it verbatim and only names, tags
  and splits it.
- Report: the name, the link (`link` in the JSON; `corgi agent up` when it is empty),
  the first lines of the split, and how many proposals wait on them. One screen.
- Never paste the whole page into the chat; it is on disk and in the browser.

## Job 2 - Answer the comments (when you are the agent on the page)

`corgi idea show <slug> --json` gives `comments`; those with `status: open` whose
last word is a person's wait on you. For each: read its `anchor.quote` in the page,
then either

- change the page (`corgi_idea_edit` with the whole markdown, or edit the file at
  `corgi idea path <slug>`) and reply `--reply-to <id>` saying what changed and in
  which section - the status becomes **fixed**; or
- answer in the reply without touching the page - **answered**.

Keep the `## Tickets`-level split in step: when scope moved, pass the new split to
`corgi_idea_edit` (`tickets`). A proposal of yours that a person accepted (replied
to) is work to do the same way. Never resolve a person's comment yourself; they do.
`corgi idea answer <slug>` does one unattended pass of all of this; the dashboard
does it on its own when it runs.

## Job 3 - Split into tickets, build, send

- `corgi idea tickets <slug>` shows the split with where each stands. The person
  edits it by commenting; you keep it true.
- `corgi idea build <slug> --workspace <id>` puts the split on the board as tasks
  under a plan and starts the daemon on the first (worktrees; `--slots N`). Needs the
  workspace watched with `--isolate`; the JSON's `why` says when it is not.
- `corgi idea send <slug> --to linear|jira [--project KEY]` makes an epic (a parent
  issue) with one ticket under it per line; `--to confluence --space KEY` publishes the
  page. Credentials are the watch's. Behind the usual confirm gate: say what will be
  created, once, then do it.

## Guardrails

- Comments are the person's; the page is yours to keep but never to rewrite wholesale
  without a comment asking for it.
- A comment you cannot act on gets a reply saying why, never silence.
- Link, not paste: the page's link, the epic's link.
