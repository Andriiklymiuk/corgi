---
description: Make a slow page, route, screen, build or test suite faster - measure first, fix the one biggest cost, measure again.
---

Run the corgi **perf** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = the slow thing and, when known, the target ("orders list under 1 s"). Empty → ask which thing is slow, once.

Follow the `perf` skill (load it with the Skill tool: `corgi:perf`): fix the conditions, take
five runs, name the biggest cost in numbers, change one thing, and take the same five runs again.
A change that gains under the noise is reverted.

Report before and after in the same units and conditions, the change, and what was not measured.
