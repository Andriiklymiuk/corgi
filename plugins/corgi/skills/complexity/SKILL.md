---
name: complexity
description: Use when changed code must get simpler, not only work — "reduce complexity", "this function is a jungle", "simplify this", "refactor for readability", "god function", "deeply nested", "check the complexity of this PR", "is this AI slop", "does this branch too much" — or as the gate the stories and review skills run on a diff. Measures the cyclomatic (and cognitive) complexity of every touched function against the repo's own threshold, refactors the worst first, proves behaviour unchanged, and reports before/after. NOT for performance work (debug skill) or style-only lint fixes.
---

# Reduce complexity

## Overview
Generated code branches like a jungle: it works, then every later edit walks the whole
jungle again. This skill keeps changed code readable by measuring per-function
complexity against the repo's own bar, refactoring the worst function first, and proving
behaviour did not move. Three modes:

- **report** — measure and rank, edit nothing. What `review` runs on a PR.
- **gate** — pass/fail on a diff. What `stories` runs before its per-service gate.
- **refactor** — measure, fix the worst first, re-measure, report. The default for
  `/corgi-complexity`.

Scope is **the diff**, not the repo: touched functions versus `<base>`. A whole-repo
scan ("where are the hotspots") is a separate ask and stays read-only unless the user
picks a target.

## What is measured
- **Cyclomatic complexity (CC)** = decision points + 1. Decision points: `if`,
  `else if`, `case`, every loop, `catch`, the ternary, and each `&&` / `||` inside a
  condition. Per function, never per file.
- **Cognitive complexity** is the tiebreaker when CC alone misleads: nesting weighs
  more than sequence, so a flat `switch` scores lower than a nested `if` ladder with the
  same CC. A refactor that lowers CC and raises cognitive complexity made the code
  harder to read — revert it.
- Nesting depth over 3 is a hotspot on its own, whatever the numbers say.

## The bar — the repo's config wins
Read the repo's own threshold before applying a default, and use it verbatim:

| Where | Key |
|-------|-----|
| `.golangci.yml` | `gocyclo.min-complexity`, `gocognit.min-complexity` |
| eslint config | `complexity: [.., N]`, `sonarjs/cognitive-complexity` |
| `setup.cfg` / `pyproject.toml` / `radon.cfg` | `radon` / `xenon` `max-absolute`, `max-average` |
| `.rubocop.yml` | `Metrics/CyclomaticComplexity: Max`, `Metrics/PerceivedComplexity` |
| `sonar-project.properties` + the Sonar quality gate | `sonar.*.complexity` conditions |

No config → **10** is the threshold, with these bands: 1–5 fine, 6–10 refactor only if
you are in the function anyway, 11–15 refactor now, over 15 split. When a stack picks a
default and `.corgi/memory/` exists, record it as a `decision` fact (confirm first — the
`memory` skill) so the next run does not re-derive it.

## Measure — through corgi, on the diff
1. Touched files: `git -C <dir> diff <base>...HEAD --name-only`. The service's runtime
   is the one that owns the tool, so run it through corgi and the right toolchain
   resolves: `corgi exec <svc> -- <tool> <files>` (`--service-dir` when the code lives
   in a worktree — `stories` Phase 3). Outside a compose service, run the tool directly.
2. **Before** numbers come from the base version of the same file, not from memory:
   `git -C <dir> show <base>:<path> > /tmp/cc-base-<name>` and measure that too.
3. Tools, by language — pick the first that is installed; install only with the user's OK:

   | Language | Measure | Install |
   |----------|---------|---------|
   | Go | `gocyclo -over 0 <files>` · `gocognit -over 0 <files>` | `go install github.com/fzipp/gocyclo/cmd/gocyclo@latest` · `.../uudashr/gocognit/cmd/gocognit@latest` |
   | JS / TS | `npx eslint --no-eslintrc --parser-options=ecmaVersion:latest --rule 'complexity: [error, 0]' <files>` (each report line carries the count) | already in most repos |
   | Python | `radon cc -s -a <files>` · `xenon --max-absolute B <files>` | `pipx install radon xenon` |
   | Ruby | `rubocop --only Metrics/CyclomaticComplexity,Metrics/PerceivedComplexity <files>` | in the repo's bundle |
   | Anything else (Swift, Kotlin, Rust, PHP, C) | `lizard -C 0 <files>` (`-w` warns over the threshold) | `pipx install lizard` |

   No tool and no install → count by hand, per function, and say the count is manual.
4. Rank touched functions by CC descending. Report the ranking with numbers before
   touching anything — the user may stop you at "just these two".

## The gate (what stories and review enforce)
On a diff, for every touched function:

1. One that started at or under the threshold must end at or under it.
2. One that started over it must not rise; when the change lands inside its body, it
   should drop.
3. A new function starts at or under the threshold.

Net: the highest CC among touched functions after the change is not higher than before.
`gate` prints one line — `complexity gate: pass` or
`complexity gate: fail — <n> function(s) over <t>: <name> <before>→<after>, …` — then
the report table. `review` turns a fail into a finding: `nit` by default, `blocking`
over 15 or on a hot path (its cost rule), naming the number and the tactic that fixes it.

## Refactor tactics, in order
1. **Guard clauses.** Invert the condition, return early, delete a nesting level. The
   cheapest CC you will ever remove.
2. **Extract a function whose name says what, not how.** `resolveDiscount(order)`
   carries meaning; `handlePart2` moves the branches and loses it.
3. **Lookup table** for an `if / else if` or `switch` chain that maps a value to a
   value. Pure data only — a table whose entries run side effects hides control flow.
4. **Named predicates.** `if isEligibleForRefund(order)` replaces a four-clause boolean
   and the name is the documentation.
5. **Strategy / polymorphism** for a switch on type, only when the same switch appears
   in two or more places. One switch is cheaper than a class hierarchy.
6. **Flatten loops.** Extract the body, use `continue` instead of a nested `if`.

Small functions with clear names beat one function with comments explaining sections.
A name that needs "and" is two functions.

## Don't game the metric
The number is a proxy for a reader's effort; anything that lowers the number and
raises the effort is a regression, whatever the table says:

- A dense one-liner that hides six branches behind a ternary chain or a chained `?.`.
- `doThingPart1` / `doThingPart2` — complexity moved, meaning lost.
- A predicate named `check1` or `isValid` with no noun.
- A lookup table whose values are closures with side effects.
- A suppression (`//nolint:gocyclo`, `eslint-disable complexity`, `# noqa`) without a
  reason a reviewer would accept on the same line.

The cognitive-complexity check (above) catches most of these mechanically; a name that
got vaguer catches the rest.

## Preserve behaviour
- Tests before and after, through corgi so the env resolves: `corgi test --service <svc>`
  (or the discovered runner). Green both times, same count.
- **No test covers the function → write a characterization test first**: pin the
  current output for a handful of inputs, including the edge that the deepest branch
  handles. Then refactor. A refactor with no test is a rewrite.
- Public and exported signatures stay unless the user asked to change them.
- Minimum diff outside the target function. No code comments — the repo rule applies
  (a comment explaining a section is the signal to extract the section).
- Stop rule: a function that will not come under the bar after ~2 honest attempts is
  reported with its number and a suggested split, not forced.

## Workflow
1. Measure the diff (or the named files), before and after versions.
2. Rank, report hotspots with numbers. Stop here in `report` mode; in `gate` mode print
   the gate line and stop.
3. Refactor the worst function first, one at a time, tactics in order.
4. Re-measure both metrics.
5. Verify: tests green, behaviour unchanged, diff reviewable.
6. Report.

## Output
End every run with:

```
## Complexity report
threshold: 10 (.golangci.yml gocyclo.min-complexity)

| Function        | CC before | CC after | cognitive before → after |
|-----------------|-----------|----------|--------------------------|
| parseOrder      | 14        | 4        | 21 → 5                   |
| applyDiscounts  | 9         | 9        | 11 → 11 (untouched body) |

extracted: validateHeader, resolveDiscount
behaviour verified: corgi test --service api — 212 passed before and after
```

Numbers and the diff do the talking; keep prose to what the table cannot say.

## Guardrails
- `report` and `gate` never edit.
- Never touch a `manualRun` service; never widen past the touched functions unless the
  user named a wider scope.
- No suppression annotations without a same-line reason.
- No AI attribution in commits, PR bodies, or comments.
- Preview a refactor's diff before committing when the user did not pre-authorise it.

## Red flags — stop
- Refactoring before a number exists → measure first, show the ranking.
- CC fell, cognitive rose or a name got vaguer → revert that step.
- A function split into numbered parts → name what each part decides, or don't split.
- Tests skipped because "it's a pure refactor" → the characterization test is the refactor's proof.
- A `nolint` / `eslint-disable` added to pass the gate → remove it, refactor or report.
- Whole-repo rewrite offered for a one-function ask → scope is the diff.

## See also
- **`review`** — runs this in `report` mode on every PR diff (Phase 3 hunt list).
- **`stories`** — runs this in `gate` mode before the per-service gate (Phase 3).
- **`memory`** — where a stack's chosen threshold is recorded as a `decision`.
