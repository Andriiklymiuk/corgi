---
description: Upgrade dependencies across a repo or the workspace - read the changelog, one upgrade per commit, tests and a healthy stack behind each.
---

Run the corgi **deps** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a library and version, a repo, "security only", or "everything". Empty → security fixes, then patches and minors; majors are listed, not started.

Follow the `deps` skill (load it with the Skill tool: `corgi:deps`): take the inventory and the
baseline, order the work, read the release notes from the source for every major, upgrade one
thing with the package manager's own command, then tests and `corgi status --until-healthy`.

Report what moved, what was held back and why, and the audit numbers before and after.
