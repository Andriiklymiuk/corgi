# corgi

Skills and commands that teach Claude the [corgi](https://github.com/Andriiklymiuk/corgi)
CLI: a Go tool that starts the databases, services and tools of a multi-repo stack from
one `corgi-compose.yml`. With the plugin, Claude can write that file, bring the stack up
and wait until it is healthy, debug it, turn tracker tickets into draft pull requests,
review pull requests, and keep the work moving while you are away from the desk.

## Requirements

- The `corgi` CLI on `PATH` (`brew install andriiklymiuk/homebrew-tools/corgi`, or a
  release binary from GitHub). Most skills run it.
- `git`, and `gh` (GitHub) or `glab` (GitLab) for the skills that read or open pull
  requests.
- Optional: a Linear or Jira MCP server, for the skills that read or write tickets.

## Install

```
/plugin marketplace add Andriiklymiuk/corgi
/plugin install corgi@corgi
```

## Skills

| Skill | What it does |
|---|---|
| `corgi` | Writes and explains `corgi-compose.yml` and the CLI |
| `run` | Starts the stack detached, waits until healthy, reports URLs |
| `debug` | Finds why a service is down, or pulls logs for a bug |
| `ci` | Generates a CI pipeline that boots the whole stack for end-to-end tests |
| `stories` | Turns tracker tickets or a feature description into draft PRs/MRs |
| `review` | Reviews existing PRs/MRs, or addresses the feedback on your own |
| `risk` | Scores how much human review a change needs, 1 to 10 |
| `complexity` | Measures and lowers the complexity of changed code |
| `prep-pr` | Gets a branch ready for review and opens the PR |
| `tracker` | Status, triage and planning on Linear or Jira |
| `autopilot` | A supervised loop that drains build-ready tickets into draft PRs |
| `suggest`, `suggest-proactive` | Ranked improvement ideas; one a week on the board |
| `align` | Settles a request that can be read two ways before building |
| `agent` | Remote Control, the daemon, ticket and review watches, handoffs |
| `setup` | Sets corgi up end to end on a new machine |
| `resume`, `standup`, `summary`, `budget` | Where you were, what you did, where a batch stands, how much usage is left |
| `mobile`, `ship`, `purchases`, `mobile-screenshots` | Expo / React Native: verify on a device, ship to the stores, in-app purchases, store screenshots |
| `before-after`, `design-parity`, `showcase` | Visual proof: before/after captures, design comparison, README pictures |
| `memory`, `improve-skill` | The workspace memory store; refining a skill from a session |

Each skill has a slash command under `commands/` (`/corgi-run`, `/corgi-review`, ...).

## What the plugin runs and sends

The plugin ships Markdown instructions and one monitor; it has no server, no hooks and
no telemetry of its own. Everything it does goes through tools on your machine, under
your own accounts:

- **Local commands:** `corgi`, `git`, `gh` / `glab`, and the test, build and lint
  commands of your own repositories. The `stack-errors` monitor runs
  `corgi logs --all` while the `debug` skill works, to show error lines from your
  running services.
- **Your forge (GitHub / GitLab), with your `gh` / `glab` login:** `review` posts review
  comments on other people's pull requests; `stories`, `prep-pr` and `review` push
  branches and open draft pull requests in your repositories. They never force-push;
  they merge or approve only when you ask, or when you turn on auto-merge for your own
  pull requests in the `agent` watch.
- **Your tracker (Linear / Jira), through the MCP server you connected:** ticket reads,
  spec comments and status changes.
- **Jira attachments:** when a ticket's screenshot cannot be read through the MCP
  server, `stories` and `review` download it with `curl` from your own Jira site, using
  `$JIRA_EMAIL` and `$JIRA_API_TOKEN` if you have set them. The token is sent only to
  that Jira site.
- **Health checks:** the `agent` skill can `curl` your own local or tunnelled app URL.

Nothing is sent anywhere else.

## Privacy

The plugin does not collect, store or share personal data, and its author receives
nothing from it: there is no server, analytics or telemetry. While it works, Claude reads
what your own tools return - code, commit authors, pull request threads, ticket text -
and that stays between your machine, Claude and the services listed above, under your own
accounts. Files the skills write (specs, handoffs under `.corgi/`, workspace memory) stay
in your repositories. Questions: open an issue at
https://github.com/Andriiklymiuk/corgi/issues.

## Evals

`evals/` holds the cases `claude plugin eval` runs with and without the plugin; see
`evals/README.md`.

## License

MIT
