# Skill evals

Two checks keep the plugin honest after every edit to a skill:

1. **Activation** — does the right skill fire on its own for a realistic
   prompt, and stay quiet for a near miss? `scripts/skill-activation.sh` runs
   each prompt through `claude -p --max-turns 1 --allowedTools Skill` and
   reads which skill was invoked. Works with any Claude Code; needs a login.
2. **Outcome** — `claude plugin eval plugins/corgi` runs the cases here
   (`<case>/prompt.md` + `graders/*.md`) with a no-plugin baseline arm and a
   score threshold. Early access at the time of writing; the cases follow the
   documented shape and the CI job tolerates the command being unavailable.

Add a case when a skill gains a rule someone once broke: the prompt that
broke it, and a grader that would have caught it.
