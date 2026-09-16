# Putting the Proactive bot on a clock

The default clock is the corgi daemon: a **routine** runs in the workspace with the
watch's caps, quiet hours and budget, filed under the bot, one inbox row per run.

```
corgi agent bot add proactive --template proactive --workspace <id>
corgi agent routine add suggest --bot proactive --workspace <id>      # weekly Mon 09:30
corgi agent routine add suggest --bot proactive --schedule "weekly fri 16:00"
corgi agent restart
corgi agent routine run suggest                                       # now, as a trial
corgi agent routine list                                              # when it last ran
corgi agent routine rm suggest                                        # off
```

A schedule is `daily HH:MM`, `every 6h` (at least 5m) or `weekly Mon HH:MM`.
Weekly matches the filing cap (default 1 per week, ceiling 3): a tighter clock
proposes nothing more. The daemon needs the workspace watched (`corgi agent watch
enable --workspace <id>`); without `--bot` the routine runs plain, same prompt, no soul.

## Without the daemon

- **`/schedule`** makes a cloud routine on a cron that outlives the terminal:
  `cron "23 9 * * 1"`, prompt `Run /corgi-suggest-proactive in workspace /abs/path`.
  The job fires with no implied cwd, so the prompt names the absolute path.
- **`CronCreate`** is session-only: fires while the REPL is idle, dies with the
  session, a recurring one expires after 7 days. Good for a one-off trial
  (`recurring: false`). `CronList` / `CronDelete <id>` manage it.

Stopping any clock is safe at any time: `.corgi/corgi_services/suggest-history.json`
is only appended to by the run itself.
