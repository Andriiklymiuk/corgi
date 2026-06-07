# Tracker + forge reference

The exact calls the `tracker` skill uses. Tool names vary by MCP version — if one
isn't present, list the server's tools and pick the closest. Read-only except the
calls under **write** (all behind the skill's gate).

## Tracker — Linear (`mcp__linear-server__*`)

| Need | Tool |
|------|------|
| Active cycle + its issues | `list_cycles` (team, current) → `list_issues` (cycle id) |
| Backlog (planning) | `list_issues` filtered to unstarted, ordered by priority |
| Untriaged (triage) | `list_issues` filtered to no-status / Triage / no-label |
| One issue (+ git links) | `get_issue` → read `attachments[]` for linked PR URLs |
| **write** | `create_issue`, `update_issue` (state/assignee/labels/priority/cycle), `create_comment` |

Issue key = `identifier` (`ABC-123`). No cycle configured → group by `state` /
project instead; there's no burn line.

## Tracker — Jira (`mcp__atlassian__*`)

`getAccessibleAtlassianResources` first for the `cloudId`.

| Need | Tool |
|------|------|
| Active sprint (Scrum) | `searchJiraIssuesUsingJql` — `sprint in openSprints() AND project = <P>` |
| **Kanban (no sprint)** | `searchJiraIssuesUsingJql` — `project = <P> AND statusCategory != Done` (group by status column) |
| Backlog | JQL `project = <P> AND sprint is EMPTY ORDER BY Rank` |
| Untriaged | JQL `project = <P> AND statusCategory = "To Do" AND labels is EMPTY` (or the team's triage filter) |
| One issue (+ git links) | `getJiraIssue`; PR links via `getJiraIssueRemoteIssueLinks` / the dev-panel if exposed |
| **write** | `createJiraIssue`, `editJiraIssue`, `transitionJiraIssue`, `addCommentToJiraIssue` |

Key = `key` (`PROJ-123`). `openSprints()` only works on Scrum boards → use the
Kanban row for everything else.

## Forge — correlate a key to its PRs (read-only, no checkout)

Prefer the tracker's own git links (above). Fallback: search PRs by the key, across
each non-`manualRun` service repo.

**GitHub (`gh`)** — run from the repo dir or pass `--repo <o>/<r>`:
```
gh pr list --state all --search "<KEY>" \
  --json number,headRefName,state,isDraft,url,statusCheckRollup
gh pr checks <n>          # CI detail for one PR
```
`statusCheckRollup` gives CI without a second call; `state`+`isDraft` →
none/draft/open/merged/closed.

**GitLab (`glab`)** — `-R <host>/<group>/<proj>` for cross-repo:
```
glab mr list --source-branch "*<KEY>*" -R <repo> -F json   # or search the title
glab mr view <iid> -R <repo> -F json                       # state, draft, sha
glab ci status -R <repo>                                    # CI for the branch
```

Map each ticket → `{ pr: none|draft|open|merged|closed, link, ci: pass|fail|pending }`
and apply the drift table in the skill's Phase 1.

## Status transitions (set when work starts — done by `stories`, not here)

Pickup hands off to `stories`, which moves each ticket to the team's **in-progress**
state as its branch is created (`stories` Phase 3). **Resolve the state, never
hardcode the name** — teams rename it ("In Progress", "Doing", "Started"):

- **Linear** — states have a `type` (`backlog`/`unstarted`/`started`/`completed`/
  `canceled`). List the team's states (`list_issue_statuses`), pick the `started`
  one, `update_issue({ id, stateId })`. Review state = a later `started`/custom one.
- **Jira** — transitions are workflow-specific. `getTransitionsForJiraIssue` →
  pick the transition whose target status has `statusCategory = "In Progress"` →
  `transitionJiraIssue({ issueIdOrKey, transitionId })`.

Idempotent: skip if the issue is already in that state. This keeps a looping
`/corgi-queue` from re-picking an in-flight ticket (auto-pick takes only
not-In-Progress tickets).
