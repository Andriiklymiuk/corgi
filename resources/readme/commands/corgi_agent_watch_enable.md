# corgi agent watch enable

## corgi agent watch enable

Watch this workspace (run inside it)

```
corgi agent watch enable [flags]
```

### Options

```
      --action string                       notify (default) or fix — fix starts a headless claude with the matching skill, draft PRs only
      --approve                             An unattended review of a pull request I was asked to review may approve it when nothing blocks and the risk card allows
      --assignee string                     me (default) or any
      --auto                                Shorthand for --action fix --prs --comments (not --reviews: reviewing someone else's PR is a separate ask): work on what arrives without being asked, draft PRs only
      --auto-allow string                   Answer a permission prompt for a tool that only reads — Read, Grep, Glob, a web search — on the daemon's own: reads, or off (Bash always waits for a person; iTerm2 sessions only)
      --auto-carry                          Carry a session that hit its five-hour quota to another of the workspace's accounts with budget, once per limit (only profiles the accounts list names)
      --auto-for string                     With --action fix, what to work on unattended: tickets, comments, reviews (comma separated). Empty means everything
      --auto-merge                          Merge a pull request of mine the moment its checks pass and it is approved (read from the forge once a round)
      --bots                                Comments from bot accounts count too (a review bot whose findings are to be fixed); off, a bot is not a person waiting
      --ci                                  Also builds that went red on something of mine — the one kind that brings its own test for done
      --comments                            Also new comments on issues assigned to me
      --compact-at int                      Type /compact into a session past this much context the next time it stops — 85 is where the board goes red; 0 is off
      --days-off string                     Days the watch sleeps through — weekends, or sat,sun, or mon,fri: no polling, no fix, nothing rings until the next working day (none clears)
      --done-when go test ./...,pnpm lint   What finished means here, comma separated: commands run in the session's directory when it stops with changes — go test ./...,pnpm lint; a red one is typed back as the next message. Empty is off
      --from string                         Only comments and reviews from these people (comma separated); empty is anyone
      --hand-over                           Type a review comment, a red build or an asked-for review into the session already on that branch
      --headless                            Let a message for a session whose terminal is gone run as one headless turn (claude -p --resume, acceptEdits) in its own checkout, so the phone's chat keeps working
  -h, --help                                help for enable
      --interval string                     Poll interval, e.g. 3m; 0 means webhooks only
      --isolate                             Give every unattended run its own worktrees on a corgi/<ref> branch, so it never touches your checkout
      --labels string                       Only issues with one of these labels (comma-separated), e.g. bug,defect
      --lease                               Claim a ticket on the tracker before working it, so a second machine watching the same board leaves it alone
      --lessons                             Write what the workspace learned the hard way — a review on a PR of mine, a check that stayed red, a bot that failed — one line each for every new session to read (corgi agent lesson list)
      --max-per-day int                     With --action fix: at most this many fixes a day (default 10)
      --max-per-hour int                    With --action fix: at most this many fixes an hour (default 3); more are deferred
      --no-retry watch run                  Leave deferred fixes to a manual watch run instead of starting them when the budget returns
      --pickup string                       Column a ticket moves to when it is picked up, e.g. "In Progress"; empty writes nothing
      --project string                      Linear team key or Jira project key, e.g. ABC
      --prs                                 Also reviews and comments on pull requests I opened
      --prune-after watch undo              Remove an isolated run's worktrees this long after it finished, e.g. 7d; the branch stays, a dirty worktree stays (empty keeps them until watch undo or `watch prune`)
      --quiet string                        Local hours to stay quiet in, e.g. 23:00-07:00: no fix starts and nothing buzzes; one summary when it opens
      --rebase                              Rebase a session's branch onto main where it sits when the session stops behind main with a clean tree and no conflicts (a branch that would conflict is typed into the session under --hand-over)
      --repos string                        GitHub repos to watch for PR feedback, comma-separated owner/repo (default: any)
      --rerun-ci                            Rerun the failed jobs of a red build once before it is worked on or handed over; a second red on the same run goes the usual way (GitHub)
      --review-status string                Column a ticket moves to once a run opened a pull request for it, e.g. "In Review"
      --reviews                             Also pull requests someone asked me to review — theirs, not mine
      --silent                              Nothing about this workspace's watch rings — no toast, no phone push: fixes run, the inbox and the kanban fill, and you look when you like (--silent=false to ring again)
      --slots int                           How many unattended runs may go at once in this workspace (1 to 8); above 1 turns on --isolate so each has worktrees of its own (default 1)
      --states string                       Only issues in one of these states, e.g. Todo,Backlog
      --tracker string                      linear or jira (default: whichever has a token)
      --workspace string                    Workspace id to change; omitted means the one you are in
```

### Options inherited from parent commands

```
      --describe                  Describe contents of corgi-compose file
      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")
  -l, --exampleList               List examples to choose from. Click on any example to download it
  -f, --filename string           Custom filepath for for corgi-compose
      --fromScratch               Clean the .corgi/corgi_services folder before running
  -t, --fromTemplate string       Create corgi service from template url
      --fromTemplateName string   Create corgi service from template name and url
  -g, --global                    Use global path to one of the services
      --interactive               Force interactive prompts even when no TTY/agent detected
      --json                      Emit machine-readable JSON output
      --privateToken string       Private token for private repositories to download files
  -o, --runOnce                   Run corgi once and exit
```

### SEE ALSO

* [corgi agent watch](corgi_agent_watch)	 - Watch the tracker and your pull requests: new issues, new comments, reviews — notify, or fix

###### Auto generated by spf13/cobra on 17-Sep-2026
