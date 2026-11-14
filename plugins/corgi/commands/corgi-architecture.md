---
description: Improve how an app or workspace is put together - map it from the code, measure cycles, hubs and change coupling, then move the seam that hurts most in small green steps.
---

Run the corgi **architecture** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a repo, a folder, a workspace, or the change that hurts ("adding a payment method"). Empty → the change history picks it.

Follow the `architecture` skill (load it with the Skill tool: `corgi:architecture`): draw the map as
it is from the imports and the compose file, measure before judging, write each problem as a
scenario with its numbers, then move one seam at a time with a fence rule and the tests unchanged.
Anything over a day's work, a public interface or a data migration is written down as a decision,
not started.

Report the map, the problems, what moved and what was left, with the numbers before and after.
