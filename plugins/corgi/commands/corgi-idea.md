---
description: Write and keep a page for something to build or decide - an idea, a spec, an HLD, an RFC - commented by people in the browser, VS Code and the phone, answered by the agent, split into tickets, built from or sent to Jira/Linear/Confluence.
---

Run the corgi **idea** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = what to write about (a sentence), a page's slug or `/idea/<slug>` link
  (then: answer its comments), or a file path with `--from` (keep it, name and split it).
  `--type spec|hld|rfc` picks the shape; omitted, pick it from the words and say so.
- A new page: `corgi idea new "…" --type <t> --json` from inside the workspace; report
  the name, the link and the split in one screen.
- An existing page: `corgi idea show <slug> --json`, answer every open comment by a
  person (change the page and reply, or just reply), keep the split in step.
- `build` or `send to <jira|linear|confluence>` in the arguments → `corgi idea build` /
  `corgi idea send`, behind one confirm.

Follow the `idea` skill (load it with the Skill tool: `corgi:idea`).
