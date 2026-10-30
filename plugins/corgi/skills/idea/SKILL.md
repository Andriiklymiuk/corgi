---
name: idea
description: "Use when the user wants a link to a doc, spec, HLD, RFC or plan from this conversation so other people can read and comment on it - \"ok, give me link to doc we created\", \"a link so my friends can comment on the hld\", \"share this hld with my team for review\", \"дай ссылку на документ, который мы сделали\" - corgi turns the markdown file into a page with a link anyone can comment on (no account), the agent answers. Also when they want a page written and kept for something to build or decide (\"write a spec for X\", \"draft an HLD\", \"RFC: move auth to the gateway\", \"turn this doc into tickets\"), or when a session is asked to read, answer or act on an idea page (`corgi idea …`, a /idea/<slug> link someone sent, \"the comments on the spec\"). NOT for implementing the tickets (stories) or for tracker status (tracker)."
---

# Corgi idea: one page per thing to build, kept by the agent

**Done when:** the page exists (or is answered), its split into tickets reads right, and the person has the link.

Read `../_shared/conventions.md` first.

A page is `corgi idea`'s unit: markdown on disk (`corgi idea path <slug>`), comments
anchored to quoted words beside it, a split into tickets in its metadata, and a
link anyone can read and comment on. Types: **idea** (something from nothing),
**spec** (a feature in an existing product), **hld** (a design), **rfc** (a decision).
Everything below works through the CLI (`--json` everywhere) or the MCP tools
`corgi_idea_list`, `corgi_idea_read`, `corgi_idea_new`, `corgi_idea_share`, `corgi_idea_comment`,
`corgi_idea_edit`, `corgi_idea_resolve`, `corgi_idea_build`, `corgi_idea_send`.

## Job 0 - A link for the team

"Give me a link for the team", "share the spec": the person wants a URL to paste in a
chat, now. No questions, no rewrite.

1. Find the page. A spec written in this session (`docs/superpowers/specs/*.md`, a
   plan, the file you just wrote) → its path. A page already here → its slug
   (`corgi idea list --json`). Nothing on disk yet → write the agreed text to a file
   first.
2. `corgi idea share <file-or-slug> --json`. A file becomes a page word for word
   (type spec; `--type` for others); sharing the same file again after an edit updates
   that page, and the link stays. No agent run, no tasks on the board.
3. Reply with the link alone on its own line, then one line: people open it, highlight
   and comment; the agent answers each comment and amends the page; the comments ring
   on the desktop and the phone.
4. For people outside the team, offer `--for 7d` (the link expires) or `--read-only`
   (no comments) in one line; never by default.
5. `public: false` → say the link works on this Mac only, and give `hint` (`corgi agent
   up` gives it a public address). Never hand a `127.0.0.1` link to a team as if it
   works.

While you brainstorm, `corgi idea open <file.md>` puts the file in the browser as a page
with comments, so the person reads and marks it up instead of scrolling the chat; the
VS Code has the same (right-click the file: Open in Corgi Preview). Comments people leave
reach you; your changes to the page are written back to the file, unless the person
edited the file since - then their edit wins and the next share brings it in.

Pictures and videos: link them from the spec with a path next to it -
`![checkout flow](media/checkout.mp4)` - and they travel with the link (files inside the
repository only, up to 40 MB each); a YouTube, Loom or Vimeo link plays where it is.
A video made with the showcase skill belongs here: one line per clip, under the
heading it explains.

Closing: `corgi idea share <slug> --off`; a link that leaked: `--rotate` (a new link,
the old one stops working). Anyone with the link reads and comments, nothing more: no
build, no send, no share, no file paths.

## Job 0b - A page someone sent you, or following one

A `/idea/<slug>?k=…` link from someone else is a page on their laptop. You take part
as a guest, under your own name (never as "agent" - that is the page's own agent):

- Read: `corgi idea show "<link>"` (text, with every thread and its id), `--json` for
  the structure; MCP `corgi_idea_read` with the link as `slug`. Without corgi,
  `GET <link path>/agent?k=…` is the same text and says how to post.
- Comment on exact words: `corgi idea comment "<link>" "…" --quote "<words>" --by
  <name>`, `--reply-to <id>` in a thread; MCP `corgi_idea_comment` with the link as
  `slug`. The page's agent answers there; you do not edit their page.
- Follow it: `corgi idea follow "<link>" --json` prints one JSON line per new version,
  comment, reply, status, sign-off or decision, until stopped - run it in the background
  (a monitor) and act on the lines. The same works for a page here: `corgi idea follow
  <slug>`.

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
  `corgi idea path <slug>`) and reply as the agent:
  `corgi idea comment <slug> "<what changed, in which section>" --reply-to <id> --as-agent --changed`
  (or `corgi_idea_comment` with `by: "agent"`, `replyTo`, `changed: true`) - the
  status becomes **fixed**; or
- answer in the reply without touching the page: the same without `--changed` -
  **answered**.

A reply without `--as-agent` is a person's: it keeps the comment waiting on the
agent, and the dashboard will answer it again.

Keep the `## Tickets`-level split in step: when scope moved, pass the new split to
`corgi_idea_edit` (`tickets`). A proposal of yours that a person accepted (replied
to) is work to do the same way. Never resolve a person's comment yourself; they do.
`corgi idea answer <slug>` does one unattended pass of all of this; the dashboard
does it on its own when it runs.

When a thread is settled and its decision line (written on resolve) is rough, reword
it with `corgi_idea_decide` (`thread`, `text`): one plain sentence, what and why. Keep
the page true to the decisions; a comment that reopens one is the only way to undo it.
A comment under the heading "Mockup" is about the drawn mockup, not the text: answer
it, and the mockup is drawn again with it.

## Job 3 - Split into tickets, build, send

- Sign-off: `corgi idea approve <slug>` (people do this, not you); `corgi idea set
  <slug> --approvals 2 --build-when-approved on` makes the last sign-off start the build.
- `corgi idea mock <slug>` draws the page's screens as a clickable HTML mockup under the
  page (a minute or two). Offer it for an idea or spec with screens.
- A team's own page type: `.corgi/idea-templates/<type>.md` in the repo, then
  `--type <type>`; `corgi idea templates` lists them.

- `corgi idea tickets <slug>` shows the split with where each stands. The person
  edits it by commenting; you keep it true.
- `corgi idea build <slug> --workspace <id>` puts the split on the board as tasks
  under a plan and starts the daemon on the first (worktrees; `--slots N`). Needs the
  workspace watched with `--isolate`; the JSON's `why` says when it is not.
- `corgi idea send <slug> --to linear|jira [--project KEY]` makes an epic (a parent
  issue) with one ticket under it per line; `--to confluence --space KEY` publishes the
  page. Credentials are the watch's. Behind the usual confirm gate: say what will be
  created, once, then do it.

## Voice

Pages and tickets read like a careful colleague wrote them, not a template: short
sentences, plain words, the real screen or table or tool by name, the reason beside
every choice, guesses admitted where they sit. A ticket's title is what a teammate
would say picking it up ("Add the week grid the owner drags shifts onto"); its body
is prose - what and why, how you know it is done, what to know first. When you reply
on a page or rewrite a ticket by hand, write the same way; `corgi idea new` already
asks the agent for it.

## Guardrails

- Comments are the person's; the page is yours to keep but never to rewrite wholesale
  without a comment asking for it.
- A comment you cannot act on gets a reply saying why, never silence.
- Link, not paste: the page's link, the epic's link.
