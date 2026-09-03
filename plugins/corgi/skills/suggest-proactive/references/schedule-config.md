# Arming the proactive-suggest schedule

`suggest-proactive` does **not** schedule itself. Two host mechanisms can fire it, and
they are not the same thing:

- **`/schedule`** creates a cloud routine on a cron. It outlives the terminal session.
  Use it for the unattended weekly run.
- **`CronCreate`** is session-only: the job lives in memory, fires while the REPL is
  idle, dies when the session ends, and a recurring job auto-expires after 7 days.
  Use it for a one-off or a same-day trial.

Either way the prompt names the absolute workspace path — the job fires with no implied
cwd, and the skill refuses to run without a `corgi-compose.yml`.

## Arm (recurring, weekly) — `/schedule`

Weekly matches the rate limit (default 1 ticket/week; hard ceiling 3 — a tighter
cadence does not file more). Pick an off-:00 minute.

```
cron:    "23 9 * * 1"          # Monday ~09:23 local
prompt:  "Run /corgi-suggest-proactive in workspace /abs/path/to/workspace"
```

## Trial run — `CronCreate`, one shot

For a first run, a single next-Monday shot proves the flow without committing to a
cadence:

```
cron:      "23 9 * * 1"
recurring: false
prompt:    "Run /corgi-suggest-proactive in workspace /abs/path/to/workspace"
```

A recurring `CronCreate` job also works for a week-long trial; tell the user it
expires after 7 days and offer to move it to `/schedule` when it lapses.

## Disarm

```
CronList                          # find the job id
CronDelete <id>                   # remove it
```

`CronList` / `CronDelete` only see `CronCreate` jobs; a `/schedule` routine is managed
through `/schedule` itself.

Cancelling the job is safe at any time — the `corgi_services/suggest-history.json` state stays
consistent (it's only appended to by the run itself).
