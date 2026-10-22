# What a second reviewer finds

The findings that came back on agent-written PRs, round after round, sorted by kind.
A diff can pass its tests and its first review and still hold every one of these: each
lives outside the hunk - in another caller, in the deploy, in the clock, in the data
already in production.

Use it twice. **While writing** (`stories` Phase 3): before the first line of a change,
answer the questions that apply to it, and build the answer in. **While reviewing**
(`stories` Phase 3.5 / 5.5, `review` Phase 3, `review` Mode B): hunt each kind that
applies. Write every finding as a scenario - who does what, in which order, and what
breaks - never as a category name. "Possible race" is not a finding; "the sender
clicks Resend while the recipient has the link open → the open link is archived" is.

When the scenario is cheap to run, prove it: a throwaway spec or probe that fails on
the head commit. A finding with a failing probe is fixed, not argued about.

## 1. Two steps that should be one

A check, then a write, as separate statements: the state changes in between.

- A count or `findFirst` before a create/delete, without a row lock, an advisory lock
  or a conditional write (`updateMany ... where <the state you checked>`).
- Two writes that must both happen (deactivate the old version, create the new one)
  outside one transaction.
- A toggle (`!current`) where the caller means a value: retries and double clicks flip
  it back. Write the target value.
- A non-idempotent write (send, create, email) after a timeout or an aborted request:
  the retry does it twice. Pass the abort signal down and check it before the write.
- In a UI, two requests for the same data: the older answer can land last. Drop a
  response that is not the newest one.
- A slow lookup (a page through an external API) between the check and the write
  widens the window to seconds.

## 2. The old world keeps running

The new code meets the old during a rollout, and the old for months after.

- Installed mobile apps keep reading the old shape: a field that changed meaning, a
  list whose first item stopped being "the" one, an enum value they do not know.
- The previous web client calls the new API until it is redeployed: keep the old
  arguments working through the release window; validate them in place.
- A migration runs before the new code: `NOT NULL` with an ORM-side default fails the
  old code's inserts - put the default in the database. Data written before an old
  migration has no backfill - check what those rows hold, not what new rows hold.
- Migration files sort by name: a branch that waited behind newer migrations on the
  base must be renamed to sort after them.
- A version bump that another open PR also makes: say which merges first and who
  bumps again.
- Name the release order in the PR body when one side needs the other deployed.

## 3. The quiet fallback

A missing value that turns into a default nobody chose.

- An env var that is in `.env.example` but not in the deploy config: production gets
  `""`, and the code built on it fails or does nothing. Grep the deploy files.
- An error caught "best effort" that leaves the system in the state the error meant
  to prevent (rows deleted, files kept and now unreachable).
- `undefined`, `null`, a missing token, an unknown environment name treated as
  "allowed", "never active", "dev" or "no limit". Unknown means refuse, loudly.
- A lookup that returns nothing because of a page boundary, a case-sensitive compare
  or a deleted record, read as "does not exist" and acted on.

## 4. One meaning, many readers

The change redefines a field, a relation or a rule; something else still reads it the
old way.

- Grep every reader and writer of what changed: other resolvers, jobs, admin tools,
  AI/MCP tools, seeds, exports, notification templates, other repos.
- Snapshots copied from it (a name stored on another row, a cached label) do not
  follow a rename or a status change.
- "The client's X" when a client can now have several: every place that picks one
  has to name which.
- A new access rule applied where rows are written but not where they are read, or
  the reverse.
- A list that grew an item only in one of the places that hand-copy it.

## 5. Personal data where it does not belong

- Logs with raw ids that the rest of the system hashes, emails (masked counts too),
  names, phone numbers, storage keys that start with a person's id, policy or
  contract numbers, URLs with names or tokens in the query.
- Raw error text forwarded to a user, a third party or a model: it can carry hosts,
  env var names, SQL. Pass through only errors you wrote; a generic line for the rest.
- Request objects spread into a handler: headers carry bearer tokens.

## 6. What the person sees when it goes wrong

- Loading, empty and error are three states: a list that is loading or failed must
  not read "nothing here".
- An error object printed as `[object Object]`, a raw API code, or the same "not
  found" for offline, forbidden and missing. Map each error the call can return.
- A conflict (concurrent change, unique violation) surfacing as a 500.
- A disabled control with no reason, a tooltip a keyboard user never reaches, a
  button that stays enabled while its data loads.
- A message built from a name that can be blank ("Hello , ...").
- Copy in every language the app ships; legal or consent text only in the wording the
  ticket gives - a translation of it is a product decision, ask.

## 7. Delete means all of it

- Every store the thing lives in: rows, files in buckets (each bucket), auth users,
  search indexes, caches, third-party copies.
- Rows only reachable through what you delete become orphans; shared ones must stay.
- `onDelete: Cascade` on history or audit rows erases what must be kept.
- A cleanup job that reuses the new cascade loses its own safety checks.

## 8. Tests that would pass with the bug

- A spec that asserts the rule the ticket just struck, or a literal, or a mock's
  call arguments instead of the outcome.
- The module under test mocked away; a new public method covered only through mocks.
- Each changed rule needs a test that fails on the base commit. Put the bug back in
  your head: which test turns red?
- A migration or SQL the CI never runs against a real database: run it locally and
  say so.
- An eval or threshold that passes when one run does the wrong thing.

## 9. Claims the change makes false

- "CI enforces X" when the check is not required; "refuses to boot" when it only
  warns; a doc paragraph that now contradicts the one above it.
- A PR body that describes the code as it was before the last round, or another PR as
  it was before it changed. Re-read the body against the head commit before you ask
  for review.
- A script that prints success after a step failed.

## 10. The repo's own rules, and the questions that are not yours

- Read the repo's CLAUDE.md / CONVENTIONS and two neighbouring files before writing.
  The nits that come back every round are house rules nobody wrote down: an input
  type for a resolver with several arguments, an enum over a string union, an explicit
  return type, a pointer cursor on every clickable element. Once one is pointed out,
  it is a lesson (`corgi agent lesson`) - it must not come back on the next PR.
- A value the ticket does not give (a duration, a limit, a default, a flag's starting
  state, who owns an external account) is a product question. Pick, build it, and
  list it under `## Assumptions` in the PR body - the reviewer should not be the one
  to discover it.
