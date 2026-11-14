---
name: quality
description: "Use to raise the code quality of a repo or area: \"improve quality\", \"harden this\", \"tech debt pass\", \"audit this repo\", \"what is wrong with this code\". Baseline, real defects worst first, proof. NOT a PR review (review), one tangled function (complexity), module or service boundaries (architecture) or how a screen looks (unslop)."
---

# Raise code quality

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the baseline was taken and run again with nothing worse, every fix has its proof, and
the report lists what was found, fixed and left, each with a reason.

## What this is for

Quality is not tidy code. It is how often the thing breaks, how fast a person notices, and how
safely the next change lands. A pass that renames things and leaves a silent failure made the
repo look better and left it no safer. Hunt what hurts people first; style comes last or never.

```
scope ──▶ baseline ──▶ hunt by kind ──▶ rank ──▶ fix one, prove it ──┐
                                                       ▲             │
                                                       └── next ◀────┤
                                   report ◀── baseline again ◀───────┘
```

## 1. Scope and baseline

- **Scope first.** Repo, folder or feature. Empty means the area changed most lately: `git log --since=3.months --name-only --format= | sort | uniq -c | sort -rn | head -20`. Churn plus bug-fix commits marks where defects live.
- **Read the repo's own bar:** its CLAUDE.md or AGENTS.md, CI config, lint config. Their rules beat this list.
- **Record the baseline before touching anything:** tests (count, failures, time), types, lint, build, coverage if the repo has it, dependency audit. A red baseline goes in the report: never call a failure yours or not yours from memory.

## 2. Hunt by kind, worst first

Read `../_shared/second-reviewer.md` for the failure kinds that live outside a diff. Write every
finding as a scenario: who does what, in which order, what breaks. "Possible race" is not a finding.

1. **Wrong answers.** Guessing from text or shape instead of a real signal; off-by-one and boundary values; timezones; floats for money; empty, huge and unicode input.
2. **Silent failures.** A caught error that does nothing, a fallback nobody chose, a default that hides a missing setting, a retry that repeats a write. A failure must reach a person or a log.
3. **Data and concurrency.** Check then write as two steps, two writes outside one transaction, an older response landing last, unsafe shared state, and no timeout on a call over the network.
4. **Security basics.** Secrets in code or logs, input that reaches a shell, query or path, missing authorization on a route, a dependency with a known hole.
5. **Tests that cannot fail.** Assertions on a mock, snapshots nobody reads, a test with no assertion. The real check: break the code on purpose and see the test go red, then undo it. A test that stays green is not a test.
6. **Flaky tests.** Fixed sleeps, order dependence, the real clock, the real network. Most are timing: wait on the condition. Never add a retry to hide one.
7. **Missing tests** on what can hurt: money, auth, data loss, migrations, and every bug already fixed once. Coverage is a map of what is untested, not a target.
8. **Speed, measured.** Queries in a loop, unbounded lists, work on every render, startup cost. Profile first and quote the numbers. No number, no finding.
9. **Dead weight.** Unused code and dependencies, a third copy of the same logic, config nobody reads. Delete rather than refactor where you can.
10. **Docs that lie.** A README command that no longer runs, a comment that contradicts the code. Run the commands.

Candidates, adapt to the stack. Read each hit: some are fine.

```bash
grep -rnE 'catch *(\([^)]*\))? *\{ *\}|except: *pass|_ = err\b|\.catch\(\(\) *=> *(undefined|null|\{\})\)' src
grep -rnE 'TODO|FIXME|HACK|\.skip\(|\bxit\(|t\.Skip\(' src
grep -rnE 'sleep\(|setTimeout\(.*[0-9]{3,}|time\.Sleep' src --include='*test*'
grep -rnE '(password|secret|token|api_?key) *[:=] *["'"'"'][^"'"'"']{8,}' src
```

Then the stack's own tools, when the repo has them: `go vet`, `staticcheck`, `npm audit`, `bun outdated`, `knip`, `ruff`, `swiftlint`.

## 3. Rank, then fix one at a time

Rank by harm times how likely, divided by cost to fix. Fix from the top.

- **Behaviour bug:** a test that fails first, then the fix, then the test passes. Run the whole suite.
- **Refactor:** tests stay green with no test edited. A test that had to change means behaviour moved.
- **Same defect, every copy.** After a fix, search for the pattern and fix the siblings in the same
  pass. A bug fixed in one of three files is still a bug.
- **One fix, one commit.** Each can be reverted alone, and the message says what broke.
- **Smallest change that does it.** No rewrites, no new abstraction for one caller, no reformatting a file you did not otherwise change, no version jumps slipped into a fix.
- **Anything over a day's work, a public interface or a migration:** do not start it. List it with the scenario and a cost.
- **Stop when fixes turn cosmetic.** Name the bar you stopped at. A pass with no end is churn.

## 4. Prove and report

Run the baseline again. Nothing may be worse: same tests plus new ones, no new lint, build green.
Then one short table and the numbers:

```
fixed  API retried a charge after a timeout: a second click billed twice      test + idempotency key
left   orders list loads 4000 rows on open: 1.8 s on a phone                  a day's work, needs paging
none   tests: 412 → 431, all pass; lint 3 → 0; audit 2 high → 0
```

Say plainly what you did not check: real devices, production data, load.

## How the pass itself goes wrong

Read from 60 real sessions. Where a person had to step in, it was rarely the code. It was these:

- Fixes left as "deferred" when they were in reach, or claimed before they were run.
- A status answered from memory: "pushed?", "fixed?". Look, then answer.
- "Not supported" or "not used" said without reading the source.
- A PR aimed at the wrong branch, or tied to work it has nothing to do with.
- A step handed to a person that the run could have done, or written so vaguely they had to ask twice.
- Parallel agents nobody asked for.
- The same failing call sent again instead of a changed one.

`../_shared/conventions.md` has the rule for each.

## Sources

Google, [what to look for in a code review](https://google.github.io/eng-practices/review/reviewer/looking-for.html):
tests that fail when the code breaks, no over-engineering, match the surrounding code.
Google Testing Blog, [where flaky tests come from](https://testing.googleblog.com/2017/04/where-do-our-flaky-tests-come-from.html),
and Luo et al., [an empirical analysis of flaky tests](https://huang.isis.vanderbilt.edu/cs8395/paper/flakytest.pdf): most fixes are async waits.

## See also

- **[`review`](../review/SKILL.md)** for a PR or MR, **[`complexity`](../complexity/SKILL.md)** for the function that is a jungle.
- **[`architecture`](../architecture/SKILL.md)** when the defects come from where the boundaries are.
- **[`risk`](../risk/SKILL.md)** to score a change, **[`unslop`](../unslop/SKILL.md)** for how the product looks and feels.
