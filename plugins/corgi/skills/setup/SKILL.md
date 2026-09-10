---
name: setup
description: Use when the user wants corgi set up end to end: "set up corgi for me", "I just installed corgi, what now", "get corgi agent running", "connect my phone, Telegram, menu bar", "connect Jira, Linear, GitHub to corgi", "onboard me to corgi". Runs every step a program can; lists the manual clicks. NOT for authoring compose or running the stack.
---

# Set corgi up, end to end

One pass, from nothing to a phone that can start a session on this laptop.
Every step is idempotent: run the whole thing again and it only fixes what
is missing. Do the automatic steps yourself; collect the manual ones and
print them at the end as a checklist, with the exact command or click.

## Guardrails

- Never paste a token into chat or a commit. Tokens go straight into a
  command flag or the environment. Ask for them once, by name.
- `brew`, `code`, `open`, `gh` are the tools. If one is missing, say so and
  give the install line; do not invent a workaround.
- macOS only for corgi-bar and the Stream Deck plugin; the rest works on
  Linux too. Check `uname -s` before those steps.
- Do not start a Claude session on the user's behalf. The last step is the
  link; the user starts sessions.

## Steps

### 1. The CLI

```bash
command -v corgi || brew install andriiklymiuk/homebrew-tools/corgi
brew trust andriiklymiuk/tools 2>/dev/null || true   # Homebrew 5 asks once; harmless otherwise
corgi update                                          # or corgi upd
corgi doctor                                          # required tools, Docker, ports
```

`corgi doctor --fix --yes` starts Docker and frees ports without asking;
installing a missing tool asks unless `--yes`.

### 2. The workspace

In the project directory:

- No `corgi-compose.yml`? Offer `/corgi-new` (writes one with Claude) or
  `corgi create` (a starter), or `corgi run -l` for an example. Do not go on
  to agent mode without a directory the user actually works in; agent mode
  works in any git repo, compose or not.
- `corgi agent init` registers the directory and enables supervision
  (autostart). `corgi agent init --config-dir ~/.claude-work` for a repo on
  another Claude account. `corgi agent scan ~/dev` registers every stack
  under a folder without enabling them.
- Everything corgi generates lives in **`.corgi/corgi_services/`** beside
  `.corgi/agent.yml`. A checkout with a top-level `corgi_services/` is moved
  there by any corgi command; `corgi migrate` does it out loud (`--dry-run`
  to look first). Nothing moves while services are up — `corgi stop` first.
  Git worktrees under it are repaired, so the source repo still points at
  them. The move also touches `.gitignore` — before committing that, run
  `git check-ignore -v .corgi/corgi_services` — a broader rule already in
  the file (a repo that ignores all of `.corgi/`) covers the new path, and
  the commit would be pure noise.

### 3. The daemon, the endpoint, the tunnel, the QR

```bash
corgi agent up --at-login
```

One command: daemon at login (launchd / systemd), MCP endpoint, a
Cloudflare quick tunnel, a single-use pairing QR, the launcher URL. Print
the QR block and the `or open:` link to the user; that is the link they
asked for. `corgi agent up` again reprints a fresh pairing window; `corgi
agent status --json` has `dashboardUrl`.

**Stable URL** (the quick tunnel changes on every restart and un-pairs the
phone). Pick one, once:

| choice | commands | manual part |
|---|---|---|
| same Wi-Fi, no tunnel | `corgi agent up --http 0.0.0.0:8765 --at-login` | none; phone opens `http://<lan-ip>:8765/app` |
| Cloudflare named tunnel | `brew install cloudflared` · `cloudflared tunnel login` · `corgi agent tunnel setup corgi.yourdomain.com` | a domain on Cloudflare; the login opens a browser |
| ngrok static domain | `brew install ngrok` · `ngrok config add-authtoken <token>` · `corgi agent tunnel setup <name>.ngrok-free.dev --provider ngrok` | free account, copy the dev domain from dashboard.ngrok.com/domains |

After `tunnel setup`, `corgi agent restart` keeps the same URL.

### 4. Session tracking (the board)

```bash
corgi agent track enable          # hooks into ~/.claude and every profile's config dir
corgi agent hooks enable --all    # "needs you" notifications for every registered workspace
corgi agent board --slots 6       # 6 for a Stream Deck Mini, 15 for an MK.2
```

Sessions started before this report on their next event; `corgi agent
rescan` picks up running ones. Run `track enable` again after every `corgi
upd`: a new version can add hooks, and `corgi agent doctor` flags the old
set as "from an older corgi".

### 5. Notifications on the phone

Telegram (free on iOS, unlike ntfy):

1. Ask the user to message **@BotFather**, send `/newbot`, follow it, and
   hand you the token (into the command, not the chat).
2. `corgi agent notify telegram --token <TOKEN>` — checks the token, waits up
   to three minutes for the user to message the bot, reads the chat id,
   writes `notifyUrl`, sends a test. Tell the user to open the bot and send
   it any message when the command says so.
3. The bot then answers commands too: reply to a "needs you" message to type
   into that session, `/sessions`, `/usage`, `/allow`, `/deny`, `/focus`.

Slack, Discord, ntfy: `corgi agent notify set <webhook-url>`; `corgi agent
notify test` sends one. A daily digest: `digestAt: "18:00"` in the user
config, `corgi agent digest --send` to try it.

### 6. The desktop surfaces (macOS)

```bash
code --install-extension Corgi.corgi --force          # VS Code: Agent sessions view, status bar, focus by tab
brew install --cask andriiklymiuk/tools/corgi-bar     # menu bar
open -a corgi-bar
```

Then the person: reload each VS Code window once (`Developer: Reload
Window`), grant corgi-bar **Accessibility** (Talk and typed prompts press
keys) and **Notifications** when macOS asks, turn on Launch at login in its
Settings.

Stream Deck: `gh release download -R Andriiklymiuk/corgi-agent-deck -p
'*.streamDeckPlugin' -D /tmp && open /tmp/*.streamDeckPlugin` installs the
plugin; the person drags **Session** keys, a **Talk** key, a **Prompt** and a
**Budget** key onto the deck.

Voice: in Claude Code run `/voice tap` once; bind `voice:pushToTalk` to
`ctrl+y` in `~/.claude/keybindings.json` (and in each profile's config dir).
The Claude Code panel uses its own `cmd+d`.

### 7. The tracker and code-host watch (optional)

Ask whether they want the daemon to notice new bugs and PR reviews while
they are away. Then, per service they use, get the token into the command
(never into the chat):

| service | where the token comes from | command |
|---|---|---|
| Linear | linear.app → Settings → Security & access → Personal API keys → New key (read is enough) | `corgi agent watch auth linear --token lin_api_…` or `LINEAR_API_KEY` |
| Jira Cloud | id.atlassian.com → Security → API tokens → **Create API token**, the plain one. Not "create with scopes": corgi authenticates with Basic auth (email + token), which a scoped bearer token is not. Plus the site URL and the login email | `corgi agent watch auth jira --url https://you.atlassian.net --email me@x.io --token …` or `JIRA_URL`, `JIRA_EMAIL`, `JIRA_API_TOKEN` |
| GitHub | `gh auth login` is enough (the watch reads the notifications feed through it); or a fine-grained PAT with Notifications read | nothing, or `corgi agent watch auth github --token ghp_…` / `GITHUB_TOKEN` |
| GitLab | Preferences → Access tokens → **legacy** token, scope `read_api` only. A fine-grained token is scoped to groups and projects; corgi reads `/api/v4/todos`, which belongs to the user and sits outside that scoping. Self-hosted needs the URL | `corgi agent watch auth gitlab --token glpat-… --url https://gitlab.example.com` or `GITLAB_TOKEN`, `GITLAB_URL` |

Those are the machine-wide tokens: every watched workspace falls back to
them. **A workspace at another company gets its own** — a second Jira site,
a different Linear key, a self-hosted GitLab. Add `--local` inside it (or
`--workspace <id>` from anywhere) and that token beats both the machine-wide
one and the environment:

```bash
cd ~/dev/acme-api
corgi agent watch auth jira --url https://acme.atlassian.net --email me@acme.com --token … --local
corgi agent watch auth gitlab --token glpat-… --url https://gitlab.acme.com --local
corgi agent watch auth linear --clear --local     # drop this workspace's override again
```

Tokens never go into the repository. `--local` writes to the user-level
agent directory, mode 0600, beside the machine-wide ones. `corgi agent
watch` prints one row per workspace holding an override.

Then, in each workspace:

```bash
corgi agent watch enable --tracker jira --project ABC --repos acme/api,acme/web --labels bug,defect --prs
corgi agent watch enable --tracker linear --project ABC --prs --action fix   # also run the fix skill, draft PRs only
corgi agent watch run --dry-run                                      # prove the tokens work: no bookmark moved
corgi agent restart
corgi agent watch                                                    # tokens per workspace, watched workspaces, last polls
```

**Always set `--project` and `--repos`.** They are what routes an event to a
workspace — an issue by its key prefix, a PR by its repo. With neither, an
event falls through to the first watched workspace that merely matches the
rules, so a review on one client's PR can start an agent in another's
checkout. `--tracker linear|jira` picks the tracker when both have tokens.

**Never guess either one — read them off the repos.** A key that is one
letter off routes nothing, silently, and looks exactly like a quiet week:

```bash
# the tracker key, from the ids the team already writes
for d in */; do git -C "$d" log --oneline -30; git -C "$d" branch -a; done 2>/dev/null \
  | grep -oE '\b[A-Z][A-Z0-9]{1,9}-[0-9]+\b' | sort | uniq -c | sort -rn | head
# the repo list, from the remotes
for d in */; do git -C "$d" remote get-url origin 2>/dev/null; done \
  | sed -E 's#.*[:/](.+)\.git#\1#' | sort -u | paste -sd, -
```

**What the first round actually does depends on the source.** GitHub sends
`If-Modified-Since` and starts from now. Linear and Jira start from
**24 hours ago** on an empty cursor, so the first poll reports a day of
tickets. Say so before enabling, or the first notification looks like a bug.

**Filter new issues by state, or the backlog arrives every time.** Ask the
tracker for its real status names instead of inventing them — a state that
does not exist matches nothing:

```bash
# Jira: the project's own statuses (Basic auth, same token)
curl -su "$EMAIL:$TOKEN" "$JIRA_URL/rest/api/3/project/<KEY>/statuses" \
  | python3 -c 'import json,sys;print(sorted({s["name"] for t in json.load(sys.stdin) for s in t["statuses"]}))'

corgi agent watch enable --states "Ready for dev,In Progress,In Review"
```

The state filter applies to **new issues only**. Comments on your issues and
reviews on your PRs come through whatever the state is — those are already
about you.

**When something fires that should not have, or does not fire at all**, ask
rather than theorise. It walks routing, rules, dedupe, caps and quiet hours
and prints the gate that stopped it. Nothing runs:

```bash
corgi agent watch test issue.new --ref <KEY>-123
```

**Unattended.** `corgi agent watch enable --auto` is `--action fix --prs
--comments`: it works on what arrives instead of only telling you. Say what
that means before turning it on — it opens draft pull requests on their real
repositories without being asked:

```bash
corgi agent watch enable --auto --quiet 18:00-09:00
```

Draft PRs only, never a merge. One run at a time per ticket, nothing done
twice (an event key is handled once across polls and webhooks), at most
3/hour and 10/day, and nothing above 95% of a usage window. `--quiet
HH:MM-HH:MM` closes a window completely: no fix starts **and nothing
buzzes** — what arrived is recorded, shows in the inbox, and is delivered as
one summary when the window opens. A run the daemon was killed in the middle
of is closed on the next start and its event offered again, so a reboot
loses nothing and repeats nothing.

What it did outlives the notification: `corgi agent watch` lists the last
fixes with their pull requests, and so do the phone ("worked on for you")
and the VS Code status bar.

`--action fix` (and so `--auto`) runs unattended only after `corgi agent init
--dangerously-skip-permissions`; say that before enabling it. Suggest quiet
hours at the same time — an unattended agent with no window acts at 3am.

**Webhooks** (instant instead of every three minutes): `corgi agent watch
hooks` prints one URL per service and a shared secret. A named tunnel
(step 3) keeps those URLs alive across restarts; with a quick tunnel they
change on every restart and must be re-entered. `--interval 0` on a
workspace turns polling off once its webhooks work. The clicks, per
service, go on the manual checklist:

- Linear: Settings → API → Webhooks → New: URL `<base>/hooks/linear`, the
  secret, events Issues and Comments.
- GitHub: repo Settings → Webhooks → Add: URL `<base>/hooks/github`, content
  type `application/json`, the secret, events Pull request reviews, Pull
  request review comments, Issue comments.
- GitLab: project Settings → Webhooks → Add: URL `<base>/hooks/gitlab`, the
  secret in Secret token, trigger Comments.
- Jira: Settings → System → WebHooks → Create: URL
  `<base>/hooks/jira?token=<secret>`, events Issue created, Comment created.

### 8. Verify, then hand over

```bash
corgi agent doctor      # every check green: claude binary, daemon, login, trust, tracking, board
corgi agent status      # workspaces online as devices
corgi agent sessions    # the board
corgi agent notify test # the phone buzzes
```

Print, in this order: the launcher link, the pairing QR (or the `or open:`
URL), and the manual checklist that is still open.

## The manual checklist (print only what is still open)

- Scan the QR on the phone (single use, ten minutes; `corgi agent up`
  prints a new one). Save `<url>/app` to the home screen.
- Open the Telegram bot and send it one message while `notify telegram`
  waits.
- Cloudflare: `cloudflared tunnel login` opens a browser. ngrok: copy the
  authtoken and the dev domain from the dashboard.
- macOS dialogs: Accessibility and Notifications for corgi-bar (and for the
  Stream Deck app if Talk should work from the deck); Docker if doctor
  started it for the first time.
- Reload each VS Code window once.
- Drag keys onto the Stream Deck.
- `/voice tap` and the `ctrl+y` binding inside Claude Code.
- Trust: run `claude` once in each workspace and accept the trust dialog,
  or `corgi agent doctor` keeps saying `trust · <id>`.
- Tokens for the watch: Linear API key, Jira API token, `gh auth login`,
  GitLab access token (table in step 7). Webhook entries in each service,
  from `corgi agent watch hooks`.

## Done when

- `corgi agent doctor` has no ✗.
- `corgi agent status` lists the workspace online.
- The user has the launcher link and, if they wanted it, got a test
  notification on the phone.
- The remaining manual steps are listed, each with its command or click.
