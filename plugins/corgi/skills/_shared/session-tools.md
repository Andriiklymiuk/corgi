# The session's own tools

A skill names a need. The session's tool list says what can meet it. Look at the list
**once, when planning**. Use a tool only when its name is in the list or in the list of
tools that load on demand; otherwise take the fallback. A tool that only has a name yet
is loaded first (the harness says how), then called. Never tell the person a tool is
missing before you read the list.

## Need, tool, fallback

| Need | Tool when present | Fallback |
|---|---|---|
| Wait for a build, CI run or deploy | The command in the background; the harness reports when it ends | `until <check>; do sleep 10; done` inside one background command |
| React to each new line of a log or job | `Monitor` | `corgi logs --service <svc> --wait-for '<regexp>'`, `corgi events --follow --json` |
| A question only a person can settle | `AskUserQuestion`: two or three options, the recommended one first; a phone can answer it | One plain question as the last line |
| The person walked away and the work is done or stuck | `PushNotification`, one line with the result or the question | `corgi agent later` rings the phone at a time; the daemon rings on its own for a waiting session |
| A file the person should see: a screenshot, a report, a recording | `SendUserFile` | The absolute path as the last line; on a ticket, `corgi agent watch comment <KEY> "<text>" --file <file>` |
| A report other people will open: a summary, a risk card, a before and after | `Artifact`: a private page with a link | Markdown in the reply; `corgi idea share <file>` for a page people comment on |
| Where a symbol is defined, called or implemented | `LSP` | `grep -rn` for the name, then read each hit |
| A fact newer than the model, or a library's current API | `WebSearch`, then `WebFetch` on the primary source | Say it is from memory, and name the version you know |
| Run again on a clock while the session is open | `/loop <interval> <command>` | `corgi agent routine add` (the daemon's clock, outlives the session) |
| Run on a clock with the terminal closed | `corgi agent routine add`, else `/schedule` | cron on the machine |
| A web page in a real browser | The Chrome tools (`mcp__claude-in-chrome__*`), else a Chromium target in argent | `npx playwright screenshot`, `curl` for the status code |
| A phone, tablet, TV or Mac app | argent (`mcp__argent__*`) | Maestro, `adb`, `xcrun simctl` |
| The local diff reviewed before a PR | `/code-review` | The `review` skill's passes in this session |
| A broad search across many folders | One `Explore` agent with a narrow brief | `grep` and read in this session |
| Many independent pieces at once | A workflow or parallel agents, **only when the person asked for them here** | One after another in this session |

## Rules that hold for all of them

- **A tool call is not proof.** Read what came back: the screenshot, the exit code, the
  page. A sent file nobody opened proves nothing.
- **One channel per message.** A push and a chat line that say the same thing are noise.
  Push when the person is away; write in chat when they are here.
- **A page or file that leaves the machine carries no secret.** Read it before you send
  or publish it: tokens, `.env` values, customer rows and private URLs come out first.
- **Never wait in the foreground.** A foreground poll blocks the conversation. Start the
  wait in the background and work the next item.
- **Independent calls go together.** Three reads that do not depend on each other are
  one step, not three turns.

## Models

- **A skill never pins a model id.** Ids change every few months. Name the tier: the
  small model for lookups and triage, the default for building, the largest for a plan
  or a review of a risky change. The workspace's `models:` policy decides the rest
  (`../agent/references/policy.md`).
- **Do not ask the model to think harder in words.** Effort is a setting of the session
  or the run, not a phrase in a prompt.
- **Plain instructions.** Current models follow a calm sentence with its reason better
  than capitals and repetition. When you write or fix a skill, state the rule once, say
  why, and delete the shouting.
- **Trust the context, then check it.** A long session is summarised and continues. A
  fact you need after a summary is re-read from disk or the forge, not recalled.
