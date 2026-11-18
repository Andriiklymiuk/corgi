# Watching the tracker and your pull requests

`corgi agent watch` makes the daemon notice work that arrives while nobody
is at the desk, and optionally start the fix:

```bash
cd ~/dev/acme-stack
corgi agent watch enable --tracker linear --project ABC --repos acme/api --labels bug,defect --prs   # notify: new issues assigned to me, reviews on my PRs
corgi agent watch enable --labels bug --prs --action fix      # and run the skill: /corgi:stories <id>, /corgi:review <pr>
corgi agent watch auth linear --token lin_api_…               # machine-wide; or LINEAR_API_KEY / JIRA_URL+JIRA_EMAIL+JIRA_API_TOKEN / GITHUB_TOKEN (gh auth) / GITLAB_TOKEN
corgi agent watch auth jira --url https://acme.atlassian.net --email me@acme.com --token … --local   # this workspace only, beats the machine-wide token and the env
corgi agent watch                                             # tokens, watched workspaces, last polls, events today, fix budget
corgi agent watch run                                         # one poll now; hands deferred fixes back to the daemon
corgi agent watch test issue.comment --body "still needed?"   # one made-up event through rules, dedupe, claim, caps; prints the prompt, runs nothing
corgi agent watch hooks [--install]                           # the workspace's webhook plan; --install creates/updates GitHub + GitLab repo hooks (polling stays on as the safety net; same comment = same key, runs once)
corgi agent restart
```

**Slack.** The watch reads Slack with the person's own user token (`xoxp-`),
issued by any Slack app they own in that workspace (Slack has no app-less
personal tokens; a free workspace caps apps at 10, so reuse an existing one
rather than creating a new one). Scopes go under **User Token Scopes**, never
Bot:

| Wanted | User Token Scopes |
|---|---|
| mentions of me → phone | `search:read`, `users:read` (the smallest token) |
| + a review channel read (a teammate's post with PR links = one review) | + `channels:read`, `channels:history` (`groups:*` for a private channel) |
| + DMs count as mentions | + `im:read`, `im:history` |
| + answer in the thread / `chat announce` / `chat edit` as me | + `chat:write` |
| + the ✅ on a reviewed post | + `reactions:write` |

Pitfalls: the granular `search:read.*` and any `admin.*` scope are
Enterprise-only on a user token - plain `search:read` is the one; a reinstall
keeps the token and the app's existing webhooks (a CI bot posting through the
same app keeps working; do not remove `incoming-webhook`). A token with fewer
scopes still works for what it can read (public channels alone, mentions
alone); `corgi agent watch` names the scope a failing call needs.

```bash
corgi agent watch auth sentry --token … --local               # new Sentry errors land in the inbox to pick: watch enable --sentry-org acme
corgi agent watch auth slack --token xoxp-… --local          # the person runs this; the token never goes through a session
corgi agent watch enable --mentions                           # notify: @me anywhere → phone
corgi agent watch enable --review-channel '#code-review' --reply-as me   # teammates' review posts in; chat announce out
corgi agent watch enable --action fix --auto-for reviews --approve --trust @teammate   # reviews run on their own; only --trust people may start one from a mention
```

**Unattended.** `corgi agent watch enable --auto` is `--action fix --prs
--comments`: it works on what arrives instead of only telling you. Draft PRs
only, never a merge, one run at a time per ticket, at most 3/hour and 10/day,
nothing in `--quiet` hours - no fix starts and nothing buzzes; what arrived is recorded, shown in the inbox, and delivered as one summary when the window opens - none above 95% of a usage window unless `--limit-ceiling` says otherwise. Nothing is done
twice - an event key is handled once across polls and webhooks, and a second
event for a ticket already being worked on is refused. A run the daemon was
killed in the middle of is closed on the next start and its event offered
again, so a reboot loses nothing and repeats nothing. What it opened outlives
the notification: `corgi agent watch` lists the last fixes with their PRs,
`corgi_watch_fixes` returns them, and the phone shows them under "worked on
for you". It needs `corgi agent init --dangerously-skip-permissions` to run
without stalling on a prompt - say that before turning it on.

**Asked what is tracked?** ("what do we watch?", "what is corgi tracking?".)
Read it, say it plainly, then say what is missing - that second half is the
part people are actually asking for.

1. `corgi_watch_status` (MCP), or `corgi agent watch`, for every workspace: the
   sources with a token, what each is watching, notify or fix, which kinds run
   unattended, the caps, quiet hours, and the last poll.
2. Say it per workspace in one line each - tracker and project, what reaches
   them, and whether corgi acts or only reports. Name what is **not** watched
   as plainly as what is: a workspace with no watch at all is the most useful
   thing in the answer.
3. **Then suggest, concretely.** Read `whatIsMissing[]` first - it is the
   ordered list of what that workspace still needs. Beyond it, the suggestions
   worth making, in this order:
   - a registered workspace with **no watch** and a tracker key in its commits
   - `--prs` off, when the person opens pull requests in those repos
   - `--ci` off - a red build is the safest thing to hand over, since it comes
     with its own test for "done"
   - `--reviews` off, when other people request their review
   - `--action fix --auto-for reviews,comments` on a workspace that only
     notifies and has been quiet enough to trust
   - `--from <person>` when they keep saying they are waiting on someone
   - `--pickup "In Progress"` so a picked-up ticket moves itself
   - `--lease` when more than one machine watches the same board
4. **Offer the smaller version first.** Watching a portion is a real answer:
   one project key, one label (`--labels bug`), a few repos, a handful of
   states (`--states "Ready,In Progress"`), or one person. Say so - people
   refuse "watch everything" and accept "watch the bugs assigned to you".
5. Anything unattended: point at `corgi agent watch replay` before turning it
   on, and `corgi agent watch undo` for after. Neither is a detail; they are
   what make it reversible.

Never turn any of this on unasked. Say what you would run, and run it when
they say yes.

**Asked to watch something?** ("watch the jira issues here", "tell me when
someone comments on my MRs".) Never guess the settings - read them, then act:

1. `corgi_watch_status` (MCP) or `corgi agent watch` says what this workspace
   already has and what it is missing. Do that first; half the answer is
   usually already configured.
2. **Derive the key and the repo list, never invent them.** A key one letter
   off routes nothing, silently, and looks exactly like a quiet week:
   ```bash
   for d in */; do git -C "$d" log --oneline -30; git -C "$d" branch -a; done 2>/dev/null \
     | grep -oE '\b[A-Z][A-Z0-9]{1,9}-[0-9]+\b' | sort | uniq -c | sort -rn | head
   for d in */; do git -C "$d" remote get-url origin 2>/dev/null; done \
     | sed -E 's#.*[:/](.+)\.git#\1#' | sort -u | paste -sd, -
   ```
3. Missing a token? Print the command for the person to run - **never put a
   token in a tool call or in chat.** Jira wants the plain API token (Basic
   auth), GitLab a legacy one scoped `read_api`; both are a coin flip in their
   UIs. `--local` keeps it to this workspace.
4. `corgi_watch_enable`, or `corgi agent watch enable`, then
   `corgi agent restart`. Say that the first tracker round looks 24 hours back,
   so a day of tickets arrives once - otherwise it reads as a bug.
5. Backlog noise → `--states` with the tracker's **real** status names.
   `corgi_watch_events` answers "what came in?"; `corgi agent watch test
   <kind> --ref <KEY>` answers "why did that fire?" and runs nothing.

How it stays cheap: a saved cursor per source (Linear/Jira `updated >`,
GitHub notifications with `If-Modified-Since` → 304, GitLab todos by id), a
seen list across polls and webhooks, the first round only sets the bookmark,
a failing token backs off, and a source the rules take nothing from (GitHub
or GitLab with `prs` off) is not polled at all. Nothing spends agent tokens
unless a new event matched the rules; with `fix`, one headless `claude -p`
per issue or PR at a time, thirty minutes at most, the process group killed
at the deadline, draft PRs only. Per kind: a new issue runs
`/corgi:stories <key>`; a comment on your issue is read and either answered
on the ticket through the tracker (no PR) or applied on the ticket's
existing branch (else `/corgi:stories`); a review or comment on your PR
runs `/corgi:review <url>` in address-feedback mode, never a fresh review.
Budget: at most 3 fixes an hour and 10 a day (`--max-per-hour`,
`--max-per-day`), none in `--quiet 23:00-07:00`, none at 95 % of a usage
window; a fix past that is deferred with the reason in the notification,
and starts on its own once the cap, the quiet hours or the budget has
passed - most urgent first (labels `urgent`, `p0`, `p1`, `critical`, then
`high`, `p2`), one per poll round; `--no-retry` leaves them to
`corgi agent watch run`. A fix runs unattended only for a workspace enabled with
`corgi agent init --dangerously-skip-permissions`. Rules live in the user
config under `workspaces.<id>.watch` (`labels`, `states`, `assignee`,
`comments`, `prs`, `repos`, `project`, `interval`, `action`,
`maxFixesPerHour`, `maxFixesPerDay`, `quiet`).

Routing and tokens across companies: `--project` (issue key prefix) and
`--repos` (owner/name) are what assign an event to a workspace; without them
it falls through to the first watched workspace whose rules merely match, so
always set them. Tokens are machine-wide by default and per-workspace with
`--local` / `--workspace <id>`, which beats both the machine-wide token and
the environment - one Jira site per client, a second Linear key, a
self-hosted GitLab. They are stored in the user-level agent directory at
0600 and never in the repository; `corgi agent watch` prints a row per
workspace holding an override. `--clear --local` drops one.

When the user asks "can corgi listen to new Jira/Linear issues or PR
comments and fix them", this is the answer: enable watch with `--action fix`,
add tokens, restart the daemon; webhooks for instant reaction.
