# What to check, per area

The **Check** lines on a card must be specific to the change. Use these lists to pick
the checks that apply; cite the file and line for each one you put on the card. A
line here that the diff does not touch does not go on the card.

## Backend / API
- **Input at the boundary**: every new request field validated (type, length, range,
  enum), and the error path returns the repo's standard error shape.
- **Authorization before work**: the permission check sits before the query/mutation,
  on the same identity the route uses elsewhere; no new route without the middleware
  the others have.
- **Idempotency of side effects**: retries, webhooks and queue consumers cannot double
  a charge, an email, or an insert. Look for a key or a unique constraint.
- **Timeouts and bounds**: every outbound call has a timeout; every list endpoint has
  a limit; no per-item network call inside a loop.
- **Migrations**: additive first; a down path that actually reverses; backfills
  batched; column drops in a later PR after the code stops reading them.
- **Transactions**: the multi-write sequence is atomic or explicitly tolerant of a
  partial write.
- **Logging**: no user field, token or secret in a new log line; error logs keep the
  request id.
- **Concurrency**: shared maps guarded, goroutines/workers have a stop path, a lock is
  not held across I/O.
- **Backwards compatibility**: old clients still parse the response; a removed field
  had its consumers checked in the other repos.
- **Feature flag**: the new path is off by default where the blast radius is wide.

## Web (SPA / SSR)
- **Null and empty states** for every field the API may omit; the loading state does
  not flash the empty state.
- **Auth-gated routes** still redirect unauthenticated; server components do not leak
  data the client should not see.
- **Forms**: validation mirrors the server's; submit is disabled during the request;
  errors are shown next to the field.
- **Accessibility**: labels on new inputs, focus order, contrast on new colours,
  keyboard path for new interactive elements.
- **Bundle**: a new dependency's size; lazy loading for a route-level addition.
- **Caching**: query keys include every input; mutations invalidate what they change.
- **Analytics/tracking**: events fire once, with no PII in properties.
- **Visual proof**: screenshot at the narrowest supported width and in dark mode when
  the repo has one.

## Mobile (Expo / React Native, native)
- **Both platforms**: a screenshot or recording per platform in the PR body when UI
  changed — one platform's proof says nothing about the other.
- **OTA vs binary**: does this need a store build (native module, SDK bump, config
  plugin, entitlement, permission string)? If yes, the card says so and the review tier
  cannot be below high — an OTA cannot fix it after release.
- **Permissions**: a new `Info.plist` usage string / manifest permission has a user-
  facing reason and is requested at the moment of use, not on launch; ATT prompt
  order preserved.
- **Push / background**: token registration handles denied → later-allowed; background
  handlers do not assume the app is foregrounded; notification taps deep-link
  correctly from a cold start.
- **Deep links / navigation**: the new route is reachable from a cold start and from
  the background; back behaviour is defined; the navigator's state persistence does
  not resurrect a removed screen.
- **Persistence and offline**: storage schema versioned; migration path for existing
  installs; behaviour with no network is defined, not a spinner forever.
- **Startup path**: nothing new runs before the root navigator mounts that can throw;
  an error boundary catches it; splash hides on both success and failure.
- **Native modules**: version pinned exact (a `~` range re-resolves upward),
  `npx expo install --check` output in the PR body, and a device launch log or
  screenshot per platform linked (a DYLD symbol crash passes every CI check).
- **Rendering that CI cannot see**: shaders, particles, 3D, Skia, video — verified by a
  screenshot read by a person; a green build proves nothing here.
- **IAP / store**: product ids match the stores, restore-purchases path exists, receipt
  validation server-side, sandbox tested on both stores.
- **App config**: `app.json` / `eas.json` / bundle id / version / build number changes
  called out; a version bump lands once (see `ship` skill's no-double-bump rule).
- **Text**: localized strings for the new copy in every supported locale; a nil value
  never formats into a sentence with a gap; long-language layouts (German) checked.
- **Performance**: lists virtualized; images sized; no synchronous storage read on the
  render path; animations on the native driver.
- **Preview constants**: a "force flag on" or fake-data constant added for driving the
  UI was reverted — grep for it.

## Data / analytics / ML
- **Schema evolution**: new columns nullable or defaulted; downstream jobs tolerate the
  new shape; the dashboard that reads it was checked.
- **Backfills**: batched, resumable, idempotent, with a row-count check before and after.
- **PII**: no new field crosses a boundary it did not before; retention/deletion paths
  updated.
- **Correctness**: a query change comes with a before/after count or a fixture the
  reviewer can run.

## Infra / CI / build
- **Blast radius**: a shared workflow or base image change is felt by every repo; say
  which.
- **Secrets**: none echoed in logs, none moved into a committed file, scoped to the job
  that needs them.
- **Failure behaviour**: a red step fails the job (no `|| true`), logs are dumped on
  failure, health waits are bounded (`ci` skill rules).
- **Rollback**: the previous image/tag is still deployable; a link to one green run of
  the changed pipeline on this branch.
- **Cost**: per-run wall clock and what triggers it, when either changed.

## Cross-service (any set spanning repos)
- **Merge order**: producer first, consumers after, stated in every PR body.
- **Tolerance**: the consumer handles the field's absence until the producer deploys.
- **Contract test**: a test on the consumer side that reads a fixture of the new shape.
- **Run line**: the one `corgi run --with-deps --service-branch …` line that brings the
  whole change up so a reviewer can try it end to end.
