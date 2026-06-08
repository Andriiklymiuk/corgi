# corgi run

## corgi run

Run all databases and services

### Synopsis

This command helps to run all services and their dependent services.

```
corgi run [flags]
```

### Options

```
      --ci                             CI mode: suppress spinners, banners, and color output.
                                       Plain log lines only. Implies --silent. Auto-enabled when CI=true env is set.
                                       Pair with --once for CI pipeline use: corgi run --once --ci
      --dbServices strings             Slice of db_services to choose from.
                                       
                                       If you provide at least 1 db_service here, than corgi will choose only this db_service, while ignoring all others.
                                       none - will ignore all db_services run.
                                       (--dbServices db,db1,db2)
                                       
                                       By default all db_services are included and run.
                                       		
  -d, --detach                         Start every service as a detached process group that survives corgi
                                       exiting, persist run-state to corgi_services/.state.json, print a JSON
                                       startup summary, and return immediately (no streaming, no watch).
      --dry-run                        Compute and print the start plan without any side effects: no make up,
                                       no git clone, no process spawn, no .env writes. Runs validation first, then
                                       reports the resolved start order and each service's port, dependencies,
                                       generated env keys, and whether it would be cloned. Pair with --json for a
                                       machine-readable plan. Exit 0 if valid, 1 if validation finds errors.
      --force                          With --detach: ignore an existing run-state and start anyway,
                                       removing the stale state file first.
      --gate-deps                      Gate service startup on dependency readiness for every depends_on edge,
                                       even ones without an explicit condition:. By default only edges that set
                                       condition: ready|started are gated; without this flag (and without
                                       condition:) services start in parallel as before.
  -h, --help                           help for run
      --host string                    IP to use instead of "localhost" in service URL env vars (so a phone
                                       on the LAN can hit your dev API). Pass an explicit IP or "auto"/"ip"
                                       to detect the first non-loopback IPv4. db_services stay on localhost.
                                       		
      --kill-port                      Reclaim service ports already in use (kill the holder) instead of aborting
      --logs                           Persist stdout/stderr of every service and db_service to
                                       corgi_services/.logs/<name>/<timestamp>.log.
                                       Keeps the last 10 runs per service; older logs are pruned automatically.
                                       Read them afterwards with: corgi logs
      --no-cache                       Ignore beforeStart cacheKey fingerprints; run every beforeStart step
      --no-watch                       Dusable watch for changes in corgi-compose file
      --notify                         Send a desktop notification when a service crashes unexpectedly.
                                       Requires notifications to be enabled (answer yes in: corgi doctor).
                                       Pass --notify=false to disable for a single run. (default true)
      --omit strings                   Slice of parts of service to omit.
                                       
                                       beforeStart - beforeStart in services is omitted.
                                       afterStart - afterStart in services is omitted.
                                       
                                       By default nothing is omitted
                                       		
      --open                           Open each service's URL in the browser when it passes its healthCheck (services with openOnReady set)
      --profile profiles:              Run only the named profile(s): services/db_services whose profiles:
                                       list contains a requested value, plus the transitive depends_on closure (so a
                                       profile still brings up the databases its services need, even if those
                                       databases have no profiles tag). Accepts a comma-separated list for the union,
                                       e.g. --profile backend,worker. Items with no profiles run only when no
                                       --profile is passed (docker-compose behavior). An unknown profile starts
                                       nothing. Composes with --services/--omit/--dbServices as an intersection
                                       (profile narrows first). By default (no --profile) everything runs.
      --pull                           Pull services repo changes
      --ready-timeout duration         Max time to wait for a database or dependency service to become ready
                                       before proceeding anyway (non-fatal). Applies to readiness gating and the
                                       database readiness probe. (default 15s)
  -s, --seed                           Seed all db_services that have seedSource or have dump.sql / dump.bak or other dump file in their folder
      --service-branch stringArray     Run a service on a git branch via a reused worktree under
                                       corgi_services/.worktrees: --service-branch name=branch (repeatable).
                                       Non-destructive — the main checkout is untouched. Clean up with: corgi worktree prune.
      --service-checkout stringArray   Run a service on a git branch by checking it out in place:
                                       --service-checkout name=branch (repeatable). Refuses on a dirty tree; leaves the
                                       repo on that branch afterwards.
      --service-dir stringArray        Override a service's working dir: --service-dir name=/path (repeatable),
                                       e.g. a git worktree. The dir must exist.
      --services strings               Slice of services to choose from.
                                       
                                       If you provide at least 1 services here, than corgi will choose only this service, while ignoring all others.
                                       none - will ignore all services run.
                                       (--services app,server)
                                       
                                       By default all services are included and run.
                                       		
      --tier string                    Env tier from the compose envTiers block (e.g. staging, prod). Selects each
                                       service's env dir and the tier's default dbServices. Empty = default.
      --tunnel                         Open public HTTPS tunnels alongside the stack for every service that
                                       declares a tunnel: block in corgi-compose.yml. Services whose tunnel
                                       hostname env vars (e.g. ${API_TUNNEL_HOST}) are unset are skipped with
                                       a warning — corgi run keeps going. Equivalent to running corgi tunnel
                                       in a second terminal, but bundled into one process. Auth still
                                       required per provider (e.g. ngrok config add-authtoken).
      --with-deps                      With --services: also start each service's depends_on closure (services + dbs)
      --yes                            Skip confirmation prompts (e.g. for a tier marked confirm)
```

### Options inherited from parent commands

```
      --describe                  Describe contents of corgi-compose file
      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")
  -l, --exampleList               List examples to choose from. Click on any example to download it
  -f, --filename string           Custom filepath for for corgi-compose
      --fromScratch               Clean corgi_services folder before running
  -t, --fromTemplate string       Create corgi service from template url
      --fromTemplateName string   Create corgi service from template name and url
  -g, --global                    Use global path to one of the services
      --interactive               Force interactive prompts even when no TTY/agent detected
      --json                      Emit machine-readable JSON output
      --privateToken string       Private token for private repositories to download files
  -o, --runOnce                   Run corgi once and exit
      --silent                    Hide all welcome messages
```

### SEE ALSO

* [corgi](corgi)	 - Corgi cli magic friend

###### Auto generated by spf13/cobra on 8-Jun-2026
