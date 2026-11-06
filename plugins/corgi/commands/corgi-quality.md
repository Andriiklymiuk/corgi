---
description: Raise the code quality of a repo or an area - take a baseline, hunt real defects by kind, fix them worst first with proof, report what is left. Pass a folder or a feature, or nothing for the area changed most lately.
---

Run the corgi **quality** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a repo, a folder or a feature. Empty → the area with the most churn lately.

Follow the `quality` skill (load it with the Skill tool: `corgi:quality`): record the baseline first,
hunt by kind, rank by harm, fix one at a time with a failing test or green tests as proof, then run
the baseline again. Anything over a day's work is listed, not started.

Report what was found, fixed and left, with the numbers before and after.
