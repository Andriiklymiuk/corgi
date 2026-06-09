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
| Agent queue (pickup) | `list_issues` `label:"agent"`, then keep states of type `backlog`/`unstarted` (drop `started`/`completed`/`canceled`) — no negative state filter, so filter client-side |
| One issue (+ git links) | `get_issue` → read `attachments[]` for linked PR URLs |
| **write** | `save_issue` (create: `title`+`team`, no `id`; update: pass `id` — state/assignee/labels/priority/cycle), `save_comment` (create: `issueId`+`body`; update: pass `id`) |

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
| Agent queue (pickup) | JQL `project = <P> AND labels = agent AND statusCategory = "To Do"` |
| One issue (+ git links) | `getJiraIssue`; PR links via `getJiraIssueRemoteIssueLinks` / the dev-panel if exposed |
| **write** | `createJiraIssue`, `editJiraIssue`, `transitionJiraIssue`, `addCommentToJiraIssue` |

Key = `key` (`PROJ-123`). `openSprints()` only works on Scrum boards → use the
Kanban row for everything else.

## Pickup scope (Job 4) — resolve from the user's words

Default = the **agent queue** (rows above). Other scopes the user may say, all under
the same floor (**not In Progress / not Done / not blocked**):

| User says | Linear | Jira |
|-----------|--------|------|
| (nothing) | `list_issues` `label:"agent"`, unstarted/backlog states | JQL `labels = agent AND statusCategory = "To Do"` |
| "in ready" / a column | `list_issues` `state:"Ready"` (the column name) | JQL `status = "Ready"` |
| "from backlog" | the Backlog row above | JQL `sprint is EMPTY ORDER BY Rank` |
| "most impactful" / "highest priority" | take the candidate set, **order by `priority`** (1=urgent…4=low), top first | append `ORDER BY priority DESC` (or sort the result) |
| "most ROI" / "most valuable" (existing) | same as most impactful — **order by `priority`**, top first | same — `ORDER BY priority DESC` |
| "bugs" / "bugs to fix" | `list_issues` filtered to the **Bug** type/label, ready states | JQL `issuetype = Bug AND statusCategory = "To Do"` |

"Most impactful" / "most ROI" **of tickets we already have** = **priority ordering**,
not a business-impact score. Real impact/ROI of **new, untracked** ideas → `suggest`,
not pickup.

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

**GitLab (`glab`)** — `-R <host>/<group>/<proj>` for cross-repo. Prefer the tracker's
own dev-panel links; fall back to searching by key:
```
glab mr list -R <repo> -F json --search "<KEY>"            # match KEY in title/desc (most reliable)
# if --search is unsupported on your glab: --source-branch "*<KEY>*" (glob is often exact-only — verify)
glab mr view <iid> -R <repo> -F json                       # state, draft, sha
glab ci status -R <repo>                                    # CI for the branch
```

Map each ticket → `{ pr: none|draft|open|merged|closed, link, ci: pass|fail|pending }`
and apply the drift table in the skill's Phase 1.

## Status transitions + assignment (done by `stories`, not here)

`stories` moves each ticket on two events: **branch created** → in-progress + assign
to the mover (Phase 3); **draft PR opened** → review (Phase 5). **Resolve the state,
never hardcode the name** — teams rename it ("In Progress", "Doing", "Code Review",
"In Review"):

- **Linear** — states have a `type` (`backlog`/`unstarted`/`started`/`completed`/
  `canceled`). `list_issue_statuses` → the `started` state for in-progress,
  `save_issue({ id, state })` (`state` takes a type/name/id). **Review** = a later `started`/custom state named
  Review / Code Review (same list). **Assign:** `save_issue({ id, assignee: "me" })`
  with the current user (the viewer).
- **Jira** — transitions are workflow-specific. `getTransitionsForJiraIssue` → the
  transition whose target `statusCategory = "In Progress"` (in-progress) or whose
  target is named *In Review* / *Code Review* (review) →
  `transitionJiraIssue({ issueIdOrKey, transitionId })`. **Assign:** `editJiraIssue`
  assignee = current user (id via `atlassianUserInfo`).

Idempotent: skip if already in that state / already assigned. **Don't steal an
existing assignee** — self-assign only when unassigned. **No review-type state on the
team → leave it In Progress.** The in-progress move keeps a looping `/corgi-queue`
from re-picking an in-flight ticket (auto-pick takes only not-In-Progress tickets).
