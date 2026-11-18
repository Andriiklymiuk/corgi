---
description: A security pass on a change, a repo or the workspace - map the ways in, prove each finding on the local stack, fix worst first.
---

Run the corgi **security** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a diff, a service, a folder or a question ("check auth on the order routes"). Empty → the current branch's diff against its base.

Follow the `security` skill (load it with the Skill tool: `corgi:security`): list every way in,
hunt by kind, prove each finding with a request on the local stack, fix the class and not the
line, and put a leaked secret's rotation under `## You do`. Probes run only against the
user's own stack.

Report each finding with its severity word, its proof and its state, and what was not checked.
