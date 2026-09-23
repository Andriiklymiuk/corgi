---
description: Capture the same screen on the base branch and on yours and attach both to the PR/MR - "screenshot comparison", "before and after", "prove the UI changed", "show me the visual diff". Pass a PR/MR link or number, or nothing to use the current branch.
---

Run the corgi **before-after** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a PR/MR link or number to attach the result to, plus optionally the
  screen to capture ("the White Label campaign brief", a deep link, a route). Empty →
  the current branch's PR/MR, and ask which screen if the diff touches more than one.
- The base to compare against is the PR/MR's target branch, or the service's base
  branch when run on a bare branch.

Follow the `before-after` skill (`plugins/corgi/skills/before-after/SKILL.md`): record
the route to the screen once, build the target the app actually ships (scheme and
configuration are different axes - confirm with `-showBuildSettings` before installing),
capture the branch, then check out the base, rebuild, replay the **same** route, and
capture again. Return to the starting branch, including on failure.

Read both images before uploading - for the change you expect, for anything else that
moved, and for tokens or real user data that must not be published. Upload with
`glab api … --form file=@…` (`--form`, not `--field`) and paste the returned markdown
into a two-column table naming the device and environment.

Never build a "before" from the diff, a mockup, or memory - the diff not showing the old
state is the entire reason this exists. If the base cannot be built, attach the after
alone and say so.
