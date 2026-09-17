# Evidence sweep — shapes, greps, failure scenarios

The eight items in SKILL.md P3, each with the grep that finds it and the scenario that
makes it a finding. Run them from the evidence worktree (P1); nothing here reads the
user's checkout. Every example below is a shape seen in a real review that the diff
alone did not show — a human reviewer with the whole tree open found it, the
diff-only review did not.

## 1. Twins and callers

Grep for the symbol, the key, the field:

```bash
git grep -n "<symbol>" -- ':!*.spec.*' ':!*.test.*'
```

Read every hit that *builds the same thing* the diff changed — a context object, a
`where` filter, a validation, an allowlist — and check it received the same change.

- **Read/enforce divergence.** A flag-evaluation *read* path gains two attributes
  (app version, platform); the eight *enforce* sites that evaluate the same keys
  still build their context without them. A version rule on that key shows the UI
  and breaks every server call behind it. `blocking`.
- **Write widened, reads not.** A create path now persists a second entity type; the
  three helpers that read the entity back still hard-filter the first type. The user
  produces the record once, inline from the mutation response, and can never read it
  again. `blocking`.
- **Policy at write time only.** A redirect-URI allowlist is checked when a client
  registers; the authorize endpoint only exact-matches the stored list and never
  re-applies the policy. The tell is internal to the diff: a second path into the same
  consent screen *does* re-validate. A host removed from the allowlist keeps
  authorizing forever — no revocation path. `blocking`.

## 2. Config delivery

```bash
git grep -nE '(^|[^A-Z_])<KEY>' -- .github .gitlab-ci.yml deploy charts k8s .aws '*.tf' '*.env*'
```

Anchored, because `grep BASE_URL` matches `DATABASE_URL` and reports a delivery that
is not there. Then name, per deployed environment, the file that sets the key.

- The diff adds `ISSUER_URL || BASE_URL || "http://localhost:3000"` and documents both
  in `.env.example` and the CI env. Neither reaches the deploy workflow's env list or
  the task definition, so every discovery document the service serves in staging and
  production advertises `localhost`. The routes are correct; the URL inside them is
  not. `blocking`, and the fix is three lines in this PR, not an infra ticket.
- Seven new knobs declared, none mapped in the deploy workflow — the defaults are what
  ships. Say what each default does in production before deciding the severity.
- A secret whose absence is silent (`Logger.warn`, then a random per-process value)
  is worse than one that fails the boot: tokens die on restart and never validate
  across replicas. Name it.

## 3. Library premise

```bash
cat node_modules/<pkg>/package.json | grep '"version"'
grep -rn "<function>" node_modules/<pkg>/dist/ | head
```

Open the function the diff relies on, at the version the lockfile pins. Or probe it:
a throwaway test in the repo's own runner, a REPL one-liner, a throwaway module that
boots only the piece in question.

- The PR's premise: an omitted attribute "matches nothing". True for `$in` / `$eq`;
  the version operators it is built on coerce a missing value to `"0"` and sort
  non-numeric tokens above every real version. A `$vlt <cutoff>` rule walls every
  caller with no version header. The premise is in the body; the refutation is in
  `dist/cjs/util.js`.
- A safe-area *context* override only reaches consumers of the hook; the
  `SafeAreaView` component applies insets natively in its own shadow node and ignores
  the context. Twenty screens double-pad while the banner is up.
- A server wrapper force-sets `extensions.code` *after* spreading the original
  extensions, so the custom code a validation rule attached cannot survive — every
  depth-limit rejection is reported to error tracking as schema drift.
- Reconciliation by position: a component that returns bare `children` on one branch
  and a three-level wrapper on the other changes the element type at that slot, so
  the whole subtree — including the blocking gate below it — unmounts and remounts on
  the normal launch path.

## 4. What the tests assert

Per test file in the diff, read the `expect` lines, not the `describe` names:

- `Reflect.getMetadata("path", handler)` asserts the decorator literal, not that the
  route resolves, returns 200, or returns the same body as the sibling path the
  criterion names. A route-order or prefix change keeps it green and breaks the
  client again. The repo already had an integration spec that boots the app — that
  was the pattern to use.
- The resolver spec grew by 158 lines and mocks the service — so it cannot see the
  new branch *inside* the service, and the service spec is not in the diff at all.
- `useQuery` mocked wholesale, so `skip: !isNative` is never exercised and nothing
  pins the operation document.
- The component the whole fix rests on: **0% functions** in the PR's own coverage
  run. The repo gates changed files at 80% on every PR, so this fails CI as soon as
  the version gate (item 6 of P1) stops failing first.

Run the gate:

```bash
corgi exec <svc> --service-dir <svc>=/tmp/corgi-review/<repo>-<n> -- <coverage script>
```

Quote the numbers in the finding. "Weak tests" without a number is an opinion.

## 5. Deployed contract

```bash
git show origin/<base>:<schema file> | grep -n "<field>"          # what main has
# or introspect the deployed producer's schema when a staging URL is known
```

- A consumer selection adds `company { logoUrl }`; the deployed producer's type has
  no such field and the consumer has no unknown-field tolerance, so the whole query
  fails with `graphql_validation_failed` until the producer deploys. Blocker on
  release ordering, no code change needed — say exactly that.
- "Merge order: api first" in the body does not bind: the api deploys by manual
  dispatch, the mobile repo auto-publishes an OTA to every installed device on merge.
  Name the mechanism per side and the window where the new consumer runs against
  the old producer.

## 6. Claims the diff makes false

```bash
corgi docs check --branch <branch>
git grep -n "<count or phrase the diff changed>" -- '*.md' .env.example
```

- The docs state a tool count and a resolver count; the diff changes both and touches neither sentence.
- `.env.example` says the app refuses to boot without the secret; the code only
  warns. That is a safety property described wrong — `blocking`.
- A CLAUDE.md sentence promises read/enforce parity that item 1 just showed is false.

## 7. Edges the happy path hides

- `Date.now() - dismissedAt < TTL` — a `dismissedAt` in the future (clock skew, a
  moved device clock) is negative, always under the TTL, and suppresses the banner
  permanently instead of for three days. Also require `dismissedAt <= Date.now()`.
- `businessInfo?.companyName ?? personName` — `??` does not catch `""`; and the
  relation is optional, so a business client with no info row gets a corporate
  report titled with their personal name.
- `onError` on the image nulls the derived URL: one cold-CDN blip hides the "Remove"
  button and relabels "Replace" to "Upload" for the rest of the session. Transient
  failure conflated with absence.
- A constant hand-copied from two other lists, with the test that would catch drift
  deliberately excluding those ids.
- A visited-set that is *path*-scoped is cycle-safe but not amplification-safe: a
  fragment spread twice in sibling positions is fully re-traversed, so a chain of N
  fragments costs `2^N`. Transcribe the function, run it on N = 20, put the number
  in the finding.

## 8. Failure domain and structure

- A non-critical banner mounted as the *parent* of the blocking update gate: any
  throw in the banner — its query, its storage read, its render — unmounts the gate
  with it. Structurally the same coupling the previous round removed at the query
  level, reintroduced at the tree level.
- Two independent bars both `position: absolute; top: 0`, opaque, one `zIndex`
  higher: both can be active at once and one always hides the other.
- A field added to the *shared* query document the blocking gate reads: a schema
  skew now rejects the whole operation, wall included, not just the new field.

## What goes in the summary

The acceptance-criteria rows (P3): one line per criterion the ticket enumerates —
`met`, `unmet → I<n>`, or `not verifiable` — after the prose. It is what tells the
author which findings stand between the PR and done, and which are on the record
for later. Findings that are real but not posted inline (lower tier, off the ticket's
path) get one line each under the rows, so nothing found is lost.
