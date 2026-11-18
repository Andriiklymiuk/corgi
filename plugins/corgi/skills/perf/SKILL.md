---
name: perf
description: "Use when something is slow and must get faster: \"this page is slow\", \"the API takes 3 seconds\", \"the app janks on scroll\", \"startup is slow\", \"why is the build slow\", \"profile this\". Measures first, finds the one biggest cost, fixes it, measures again. NOT tangled code that runs fine (complexity) or a service that is down (debug)."
---

# Make it faster, with numbers

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** the same measurement, taken before and after on the same machine and data,
shows the gain, and nothing else got slower or broke.

A fix without a number before it is a guess. Most guesses about speed are wrong: the slow
part is rarely where the code looks slow.

```
name the slow thing ──▶ measure ──▶ find the biggest cost ──▶ fix one ──▶ measure again
                           ▲                                                  │
                           └──────── still too slow: next biggest ◀───────────┘
```

## 1. Name it and set the target

- **One sentence:** who waits, for what, how long now, how long is fine. "Orders list:
  2.4 s to first row on a mid phone, target under 1 s."
- No target from the person → take the product's own bar (another screen that feels
  fine), or stop at the point where the next fix costs more than it gives.
- **Fix the conditions** before the first number: the same data size as production
  (seed it; 20 rows hide every problem), a release or production build, a warm run and
  a cold run stated apart.

## 2. Measure

Run each measurement at least five times; report the median and the worst. One run is
noise.

| The slow thing | Measure with |
|---|---|
| An API route | `curl -sS -o /dev/null -w '%{time_total}\n' <url>` in a loop; the service's own timing log through `corgi logs --service <svc> --grep '<route>'` |
| A database query | `EXPLAIN (ANALYZE, BUFFERS)` through `corgi exec <service> -- <the repo's db shell>`; the query log for count per request |
| A web page | The browser's performance trace and network panel (the `web` skill's driver); Lighthouse in the repo's CI when it has it |
| A React Native screen | argent `react-profiler-*` for renders and commits, `native-profiler-*` for the native side, on a release-like build |
| A Go, Node or Python process | `go test -bench . -benchmem`, `pprof`; `node --cpu-prof`; `py-spy` |
| Startup, a build, a test suite | `time <cmd>`; the tool's own timing flag (`--profile`, `-json`, `--durations`) |

`corgi exec <service> -- <cmd>` runs a tool inside the service's resolved environment, so
the profile sees the same database and config as the running stack.

## 3. Find the one biggest cost

Read the profile top down. The cause is almost always one of these:

1. **Work done many times.** A query per row (N+1), a request per item, a render per
   keystroke, the same file read in a loop. Count the calls: 200 queries for one page is
   the finding.
2. **Work done on too much.** A list with no page, a query with no index or no `LIMIT`,
   a payload ten times what the screen shows, an image larger than its box.
3. **Work done in a row that could overlap.** Three independent calls awaited one after
   another.
4. **Work done again.** No cache on something that rarely changes; or a cache that is
   never hit because its key always differs.
5. **Work done too early.** Code, data or a module loaded at start that only one screen
   needs.
6. **Waiting, not working.** A lock, a pool with one connection, a timeout that is hit
   every time, DNS, a cold start.

Name the cost in numbers: "1.9 s of 2.4 s is 212 queries for line items". If no single
cost is over about a fifth of the total, say so: the thing is slow all over, and that is
a design question for the person, not a patch.

## 4. Fix one, measure again

- **One change, then the same measurement.** Two changes at once and you do not know
  which one worked.
- **Keep what the numbers keep.** A change that gains under the noise is reverted: it is
  cost with no return.
- **A faster wrong answer is a bug.** Tests stay green. A cache gets its invalidation in
  the same change, and a test that reads stale data after a write.
- **Smallest change first:** an index, a `LIMIT`, a join instead of a loop, a batch, a
  memo on the hot component. A rewrite, a new cache service or a queue is listed with
  its expected gain and cost, not started.
- **An index or a migration on a large table** is the person's call: write the statement
  under `## You do` with when to run it.
- **Guard the gain** where the repo can: a benchmark, a query-count assertion in a test,
  a size budget in CI. Without one the slowness comes back unnoticed.

## 5. Report

```
orders list, 5 runs, 4000 seeded orders, release build, Pixel 7 emulator
before   median 2.4 s, worst 3.1 s    212 queries
after    median 0.6 s, worst 0.8 s    3 queries
change   one join for line items; page of 50; index on orders(user_id, created_at)
left     images are 4x their box: about 0.2 s more, needs the CDN config
not measured: a real phone on a slow network
```

Before and after in the same units, same conditions. Name what was not measured.

## Red flags

- A fix before a measurement.
- A number from one run, or from a debug build.
- "Should be faster."
- A cache added with no invalidation.
- Twenty test rows.

## See also

- **[`quality`](../quality/SKILL.md)** for a whole-repo pass, **[`complexity`](../complexity/SKILL.md)** for code that is hard to read, not slow.
- **[`web`](../web/SKILL.md)** and **[`mobile`](../mobile/SKILL.md)** drive the screen you measure.
- **[`architecture`](../architecture/SKILL.md)** when it is slow because of where the boundaries are.
