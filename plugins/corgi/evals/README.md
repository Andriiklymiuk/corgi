# Skill evals

Two checks keep the plugin honest after every edit to a skill:

1. **Activation** - does the right skill fire on its own for a realistic
   prompt, and stay quiet for a near miss? `scripts/skill-activation.sh` runs
   each prompt through `claude -p --max-turns 1 --allowedTools Skill` and
   reads which skill was invoked. Works with any Claude Code; needs a login.
2. **Outcome** - `claude plugin eval` runs the cases here twice, with the
   plugin and without, and scores what the answer contains. The number to
   watch is the difference between the two.

## Run them

```
claude plugin eval plugins/corgi --scaffold --tag read-only --judge-model sonnet
```

`--scaffold` is not optional: every case builds its workspace with its
`scaffold.sh` (one small Python api, a `corgi-compose.yml`, a local bare
remote - `_fixtures/common.sh`). Without it the case runs in an empty folder
and both arms score 0. Add `--runs 1 --no-publish` while editing a case and
`--case <name> --ablation none --keep-temp` to read one run's `trace.jsonl`.

| tag | cases | needs |
|---|---|---|
| `read-only` | handoff, the two reviews | nothing: read and search tools only |
| `shell` | proactive, push-your-own, stories | `--allow-tools Bash Edit Write`, and `corgi` on `PATH` |

The `shell` cases run commands, so the harness sandboxes the shell and refuses
to start on a machine it cannot fence in (a symlink inside `~/.docker` is the
usual reason; the message names it). They are written and their fixtures are
checked, but they have not been run end to end: pilot them with `--runs 1`
before trusting a score, and keep them out of the CI gate until then.

## What a case is

`<case>/case.yaml` (the prompt, the tools, the graders) and `<case>/scaffold.sh`.
No case reaches a forge or a tracker: a pull request is saved under `.pr/<n>/`
(`pr.json`, `diff.patch`, `head/`), merge request threads under `.mr/<n>/`, and
the prompt says so. Nothing can be posted from a run.

Grade what the answer contains, not which tools were called. The `tool_used`
grader on `Skill` is shown but not scored; its `input_match` is a regex over
the call's input, so quote the skill name (`'"(corgi:)?review"'`) or a stray
word in the arguments matches.

Add a case when a skill gains a rule someone once broke: the prompt that
broke it, a fixture where the rule changes the answer, and a grader that
would have caught it.
