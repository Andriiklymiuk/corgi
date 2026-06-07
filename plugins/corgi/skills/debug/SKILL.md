---
name: debug
description: Use to diagnose a corgi stack OR to gather runtime data while investigating a bug. Two entry modes — (1) a stack misbehaves: a service won't start, crashed, is stuck/unhealthy, hangs on boot ("debug the stack", "why is X not starting", "the api is down"); (2) you're working a bug ticket (Jira/Linear) and hit "need more data" — pull logs, request traces, analytics, or backend perf data, local or deployed ("pull the logs for this request", "what do the staging logs say", "check the 500s", "why is this endpoint/query slow"); client-side/UI perf (render jank, slow animation, bundle) is the app framework's profiling job, not this. Local-first (corgi ps/status/doctor/logs + targeted retries); escalates to whatever logs/analytics provider the repo uses (CloudWatch/ECS, Coralogix, Datadog, Sentry, Grafana/Loki, New Relic, … — auto-detected from the repo's README) on demand and after asking. Callable from the stories skill mid-investigation. NOT for authoring corgi-compose.yml (corgi skill), starting a stack (run skill), or reviewing PRs (review skill).
---

# Corgi debug

Three entry modes:

- **Broken stack** — a service won't start, crashed, is unhealthy, or hangs on
  boot. Steps 0–3 (local); Step 4 only if a deployed env is implicated.
- **Bug investigation** — on a ticket, hit *"need more data"*: runtime logs, a
  request trace, analytics, or **backend perf** ("why is order fetching slow", a slow
  endpoint/query) — **local or deployed**. Stack may be fine; often jump straight to
  **Step 4** (provider data / APM trace), Steps 0–2 only if it reproduces locally.
  **Client-side / UI perf** (render jank, slow animation, first-load, bundle size) is
  app-profiling — **not** corgi's stack data → the app framework's perf tooling, not here.
- **CI red on a PR** — a pushed branch's checks failed ("why is CI red", "the build
  broke on my PR"). corgi has **no CI command** — the failing job log comes from the
  **forge CLI**, then reproduce locally → **Step 5**.

**Local-first.** Exhaust the fast local probes before anything external. The
provider is read from the repo (`README`/`CLAUDE.md`), never hard-coded, queried on
demand. Work in order; **stop the moment the cause is explained** — don't run the
whole ladder for a one-liner.

## Guardrails (non-negotiable)

- **Never foreground a `corgi run` from a Bash call** — `beforeStart` runs inline
  *before* run-state is written and `corgi run` streams forever, so a sync call hangs
  (10-min timeout) and never reaches the next step. Always `--detach` (or
  `run_in_background: true`). See `../corgi/references/long-running.md`.
- **Bound every read.** Logs via idle-limited `corgi logs` (`--idle <Ns>`); raw files
  via `Read(limit: ~100)`. Never an unbounded read of a `.log`/`.state.json` (logs
  cap at 50MB).
- **Local before external (broken-stack mode).** Stack won't start → exhaust `ps` →
  `status` → `doctor` → `logs` before any analytics MCP (most "won't start" bugs are
  ports/docker/env, found locally). **Bug-investigation mode**, symptom is
  deployed/remote (a staging/prod 500 that may not reproduce locally) → go straight
  to Step 4; local probes only if it reproduces.
- **Analytics only when the investigation needs it AND the user OKs it.** Detect the
  provider, ask first, read-only, scope to service+env+window, **never echo secrets**.
- **Run safe fixes; ask before destructive/remote** (`--force`, dropping a DB,
  killing a port the user may want, anything hitting a deployed env).
- **~2 honest tries per service**, then report `needs attention` + what you saw.

## Step 0 — Snapshot (broken-stack mode)

```
corgi ps --json        # name/kind/port/status/url (+ startedAt); reconciles corgi_services/.state.json
corgi status --json    # live TCP/HTTP probe — the ONLY liveness truth
# uptime = startedAt from `corgi ps --json`; an older corgi omits it → cat corgi_services/.state.json
```

**Trust `corgi status` (live probe) for liveness, not `corgi ps` status.** ps
`status` only means the PID/container *exists* — `db_services` only go
`running`/`stopped`, never `crashed`, and a container-backed service (docker runner,
pid 0) isn't refreshed by a liveness check, so a crash won't flip it to `crashed`.
Judge those by `corgi status` + `docker ps`.

Classify on **real signals** (JSON keys lowercase):

| State | Tell | Next |
|-------|------|------|
| **healthy** | `corgi status` `healthy:true` | not the problem |
| **crashed** | ps `status:"crashed"` OR a newest `*.crashed.log` | Step 2 |
| **up but probe red** | ps `status:"running"` BUT `corgi status` `healthy:false` | Step 2 + check the `healthCheck:` path |
| **never spawned / mid-beforeStart** | entry **absent** from `.state.json` | Step 1 |

Uptime = now − `startedAt`; call out "X up 4m, Y never came up". **No exit code in
`.state.json`** — read it from the log body (Step 2), not the file or the `.crashed`
suffix.

**"Stuck" splits** (corgi has no booting-vs-ready flag):
- **Hung in `beforeStart`** → `corgi run --detach` itself never returned; service
  **absent** from `.state.json`. Launched in a Bash call → still blocking; check that
  run's output / `--logs`; `--omit beforeStart` to isolate.
- **`start:` running, port never opens** → tail logs: advancing = slow boot, a stack
  trace = real failure.
- **Slow first boot vs hung** — a first run that clones repos / seeds a DB / pulls
  images legitimately takes minutes. Tell apart ONLY by whether the log keeps
  advancing: `corgi status --watch` (or a 2nd `corgi status --json` after ~30s,
  compare the healthy count). Cross-ref `../corgi/references/debugging.md`.

## Step 1 — `corgi doctor` when something won't start

```
corgi doctor --json    # tools installed, Docker up, ports free — and ONLY those
```

First thing on "doesn't work". Fixes (offer, then run the safe one): **port busy** →
`corgi run --services <x> --kill-port --detach` (ask if it's a port the user wants);
**Docker down** → start it, re-`doctor`; **missing tool** → install (`required:`
names it).

Doctor does NOT cover — route elsewhere: **clone not done** → service dir empty /
"failed to clone" in run output (`corgi init`, fix branch/URL/token); **seed/dump
failure** → only on `corgi run -s`; check `seedFrom*`; **env unset/wrong** →
`corgi env <x>`; **wrong `healthCheck:` path** → service binds the port but
`corgi status` gets 404/5xx; **bad compose** → `corgi validate`.

## Step 2 — Local logs

Needs a `--logs` run. Default = bounded, single service:

```
corgi logs --service <x> --json --idle 3s    # the stuck/crashed one, bounded
```

Cross-service correlation only (reads each newest file in full, no idle — pricier):
`corgi logs --all --json`. Find the first error / stacktrace / crash marker
(`*.crashed.log`) + the exit code from the log body. **No logs captured** → re-run
just that service with `--logs` (Step 3).

## Step 3 — Targeted fix / retry

Smallest change that could work, then re-gate. **Never foreground a `corgi run`.**

- **Service with dependencies** → `corgi run --services <x> --with-deps --detach --logs`.
  Without `--with-deps` an isolated retry against a down dependency just re-crashes.
- **A consumer can't reach a healthy producer** (404 / connection-refused to another
  service, not a crash) → the producer is fine; check the **consumer's resolved env**:
  `corgi env <consumer>` for a stale or wrong dependency URL — a prior `--host`/remote
  run rewrote it, the producer was excluded from the run set so no
  `depends_on_services` alias got generated, or the `.env` was hand-edited. Re-run
  without the override / with `--with-deps` so corgi regenerates the wiring.
- **Reproduce a suspect service on its branch / worktree** →
  `corgi run --services <x> --with-deps --detach --logs --service-branch <x>=<branch>`
  (corgi makes a non-destructive worktree off the pushed branch; main checkout
  untouched), or `--service-dir <x>=<worktree-dir>` for live/uncommitted code. Guard:
  `corgi run --help | grep service-branch` (absent → `git checkout <branch> &&
  corgi run --services <x>`).
- **DB is the problem** (most common) → `corgi run --dbServices <db> --services none
  --detach`, then `corgi status --json` / its logs.
- **Full-stack flake** → `corgi restart`.
- **A `beforeStart` step wrongly skipped** → `--no-cache` (re-runs *every*
  `beforeStart` step, ignoring `cacheKey` — **not** an image/deps rebuild). Truly
  stale generated artifacts → `corgi clean -i corgi_services` / `corgi run --fromScratch`.
- **Env wrong** → `corgi env <x>`. **Compose wrong** → `corgi validate`.

Re-gate: `corgi status --ready --service <x> --timeout 120s --json`. Green → report.
Red after ~2 tries → `needs attention` + the error, move on.

## Step 4 — Logs / analytics from the provider (on demand, ask first)

**Use when** the ticket needs runtime/deployed data (a staging/prod request, a 500
you can't reproduce locally, **or a slow backend path's trace/timing** — APM /
slow-query log) **or** local logs don't explain a local bug. For a bug investigation
this is **first-class, not a last resort**.

**Before the ask-gate:** (a) if the symptom carries a ticket key whose tracker MCP is
connected, read the ticket first (`mcp__linear-server__get_issue` /
`mcp__atlassian__getJiraIssue`) — it usually already holds the endpoint / request-id
/ timestamp / env to scope the query; (b) verify the provider's MCP is actually
connected — absent → jump to step 4 (connect-this-MCP message + docs pointer); don't
collect scoping you can't use.

1. **Detect the provider** — one bounded pass over the docs (corgi is
   provider-agnostic; extend the pattern to whatever the repo names):
   ```
   grep -liE 'coralogix|cloudwatch|ecs|datadog|sentry|new ?relic|grafana|loki|kibana|elastic|splunk|honeycomb' README.md CLAUDE.md Claude.md claude.md 2>/dev/null
   ```
   (filename casing varies — `Claude.md` vs `CLAUDE.md`.) Map the hit to its access
   path — a connected **MCP** for that provider if one exists (e.g. Coralogix →
   DataPrime; CloudWatch/ECS → the AWS MCP; Datadog/Sentry/Grafana → their MCP), else
   the provider's CLI/API. Multiple providers named (e.g. APM + logs) → pick the one
   the repo ties to **logs** for the affected service, not the APM/metrics one. Then
   read that provider's own section in the repo docs for the exact fields to scope the
   query (application/subsystem, environment, request id / endpoint / error class). None found → infer from the deploy target and say
   so: "no logs provider named in the docs — this stack deploys on `<infer>`; connect
   its MCP/CLI or paste logs," and stop.
2. **Ask before querying** — provider + which env + the time window + the
   service/request.
3. **Query read-only**, scoped to service + env + a tight window. Summarize the
   relevant lines; never dump raw payloads or secrets.
4. **MCP not connected** → tell the user exactly what to connect, and point at the
   repo's logs/observability README section.

## Step 5 — CI red on a PR (forge logs, then reproduce locally)

A pushed branch's checks failed. corgi has no CI command — pull the failing job's log
from the **forge CLI**, then reproduce with the service's own gate.

1. **Failing log** (read-only): GitHub — `gh pr checks <n>` (which failed) →
   `gh run view <run-id> --log-failed`. GitLab — `glab ci status -R <repo>` →
   `glab ci trace -R <repo>` (the failing job).
2. **Read the first real error** — stacktrace / failing test / lint / build; skip the
   green jobs.
3. **Reproduce locally with the gate**, don't guess: `corgi test --service <x>` (or
   `corgi exec <x> --ensure-deps -- <lint/build cmd>`). Green locally but red in CI →
   an env gap (a CI-only var, a tool/node version, stale cache) — name it.
4. **Fix → push**, re-check `gh pr checks` / `glab ci status`. ~2 tries, then report
   `needs attention` + the failing job.

## Report

What was wrong, what you ran, what fixed it (or what's still `needs attention` and
why). Stopped at the analytics ask-gate → name the provider and the query you'd run.

## Called from another skill

The `stories` skill (or any investigation) can invoke `debug` mid-flight when it
needs runtime data — jump to **Step 4** (provider data) or **Steps 0–2** (reproduce
locally). Return the findings; don't take over the parent flow.
