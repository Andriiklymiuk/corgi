---
description: Measure and reduce the cyclomatic complexity of changed code. Pass a scope in plain words — a PR/MR link, a branch, files, or a service — plus an optional mode (`report` = numbers only, `gate` = pass/fail on the diff, `refactor` = fix the worst first, the default). No args = refactor mode on the current branch's diff against its base.
---

Run the **complexity** skill for `$ARGUMENTS`.

- `$ARGUMENTS` = scope (a PR/MR link or number, a branch, one or more files, a
  `corgi-compose.yml` service name) and/or a mode: `report`, `gate`, `refactor`.
  Empty → `refactor` on the current branch's diff against its base.
- Run **inside the repo or the stack folder**; a compose service resolves through
  `corgi exec <svc> -- <tool>` so the right toolchain measures it.

Per `plugins/corgi/skills/complexity/SKILL.md`:

1. **Find the bar** — the repo's own threshold from its linter config; else 10.
2. **Measure the diff** — touched functions, before (base version) and after, with the
   language's tool through corgi; manual count when no tool, and say so.
3. **Rank and report** the hotspots with numbers. `report` and `gate` stop here (`gate`
   prints `complexity gate: pass|fail`).
4. **Refactor the worst first**, one function at a time: guard clauses → extract with a
   what-not-how name → lookup table → named predicates → strategy only for a repeated
   switch-on-type → flatten loops.
5. **Prove behaviour held** — tests green before and after through `corgi test`; a
   characterization test first when nothing covers the function.
6. **End with the complexity report table** (function, CC before/after, cognitive
   before → after), what was extracted, and how behaviour was verified.

Honor every guardrail: never game the number (a one-liner hiding six branches, a
`part1/part2` split, a vague predicate, a suppression without a reason), never touch a
`manualRun` service, never widen past the touched functions unless asked, no AI
attribution.
