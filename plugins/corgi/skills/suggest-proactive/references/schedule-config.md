# Arming the proactive-suggest schedule (`/schedule` / CronCreate)

`suggest-proactive` does **not** schedule itself — the host harness `/schedule`
(CronCreate) owns the cadence and fires the job while the REPL is idle. This is the
exact config to arm, disarm, and re-arm it.

## Arm (recurring, weekly)

Create the job via CronCreate with a standard 5-field cron, an **off-:00 minute** (to
avoid fleet pile-up), `durable: true` (survives session restarts →
`.claude/scheduled_tasks.json`), and the **absolute workspace path** baked into the
prompt (the cron fires with no implied cwd):

```
cron:      "23 9 * * 1"          # ~weekly, Monday ~09:23 local (off-minute on purpose)
recurring: true
durable:   true
prompt:    "Run /corgi-suggest-proactive in workspace /abs/path/to/workspace"
```

- **Default cadence is weekly** — it matches the rate-limit (default 1 ticket/week).
  Hourly/daily is allowed, but the per-week cap (hard ceiling 3) still binds, so a
  tighter cadence does NOT file more.
- The prompt MUST name the absolute workspace path. The skill `cd`s / resolves there
  and refuses if no `corgi-compose.yml` is found.

## 7-day auto-expire — you MUST warn the user

CronCreate **recurring** jobs fire a final time and then **delete themselves after 7
days**. When you arm a recurring job, tell the user this and offer to **re-arm** it
when it lapses. Re-arming is just creating the job again with the same config.

## One-shot alternative (conservative default for first-timers)

For a first run, prefer a single next-Monday shot instead of a recurring job — it
proves the flow end-to-end without committing to a cadence:

```
cron:      "23 9 * * 1"          # next Monday ~09:23
recurring: false
durable:   true
prompt:    "Run /corgi-suggest-proactive in workspace /abs/path/to/workspace"
```

## Disarm

```
CronList                          # find the job id
CronDelete <id>                   # remove it
```

Cancelling the job is safe at any time — the `corgi_services/suggest-history.json` state stays
consistent (it's only appended to by the run itself).
