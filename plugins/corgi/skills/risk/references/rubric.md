# Risk rubric — points per dimension

Each dimension scores 0–4 from the evidence. **Score = the highest dimension, plus 1
for every other dimension at 2 or more, capped at 10, then raised to any floor** in
SKILL.md, **plus 1 when a floor applied and verification is 3 or more** (the
escalation rule). A story target scores verification 3 and takes its floors from what
the ticket says it will touch. Points are awarded for what the diff shows, never for
what the title claims.

## Blast radius
| Points | Evidence |
|--------|----------|
| 0 | one file, or test/docs/copy only |
| 1 | a few files in one module of one service |
| 2 | several modules of one service, or a shared helper with a handful of callers |
| 3 | two or more services, or a shared module every request passes through (middleware, router, client SDK, design-system component used app-wide) |
| 4 | a public API, an SDK consumers pin, a package published to a registry, or a change to how every service is built or started |

Count callers with a grep, not by feel: `rg -n '<symbol>' --type <lang> | wc -l`.

## Reversibility
| Points | Evidence |
|--------|----------|
| 0 | revert of the commit restores the old behaviour completely |
| 1 | revert restores it but needs a redeploy of one service |
| 2 | behind a flag or config that can be turned off without a deploy, **or** additive migration with a working down path |
| 3 | irreversible side effect once run — sends email/push/SMS, charges, writes to a third party, backfills data — with no dry-run or idempotency guard |
| 4 | destructive migration (drop, rename, type narrowing) or a binary release that reaches users and cannot be pulled (a store build, a firmware push) |

A flag that exists but is hard-wired on counts as no flag.

## Data & access
| Points | Evidence |
|--------|----------|
| 0 | no user data, auth, or secrets involved |
| 1 | reads non-sensitive user data through existing, unchanged paths |
| 2 | new or changed input validation, new query over user data, new logging that could include user fields |
| 3 | permission or role check changed, session or token lifetime changed, PII stored or sent somewhere new, encryption or hashing touched |
| 4 | auth flow, credential storage, secret rotation, or anything that decides *who* can do *what* |

Look for: `auth`, `session`, `token`, `jwt`, `permission`, `role`, `scope`, `password`,
`secret`, `crypto`, `hash`, `pii`, `email`, `phone`, `ssn`, `card`. A dependency that
handles any of these counts as touching it.

## Verification
| Points | Evidence |
|--------|----------|
| 0 | tests in the diff cover the changed lines; CI green; bug fix has a FAILS-on-base test; UI change has a screenshot or recording |
| 1 | tests exist for most changed lines; one gap named |
| 2 | tests touch the area but not the new branch/edge; or CI has no check for this service |
| 3 | no test for the changed lines; or a test was deleted, skipped, or loosened (`.skip`, `t.Skip`, weakened assertion) |
| 4 | CI red on head, or a test modified to pass without a behaviour reason, or manual-only claim with no test plan |

A `complexity` gate fail (SKILL: `complexity gate: fail`) adds 1 here, once.

## Contract
| Points | Evidence |
|--------|----------|
| 0 | no cross-boundary shape touched |
| 1 | internal type change with all callers in the same diff |
| 2 | additive field on a request/response/event, consumers tolerate it |
| 3 | field removed, renamed, type changed, or made required; consumers exist in another repo |
| 4 | breaking change with a merge-order dependency across repos, or a versioned API without a version bump |

Producer/consumer detection: shared types in `shared/`, `packages/`, `proto/`,
OpenAPI, GraphQL SDL, event names; the other repos' `rg` for the field name.

## Operations
| Points | Evidence |
|--------|----------|
| 0 | nothing runtime-shaped changed |
| 1 | a log line, a metric, a non-hot-path timeout |
| 2 | env var added (documented in the env template), a cron/job schedule, cache key or TTL, retry policy |
| 3 | CI/build/Dockerfile/compose change, a hot path made slower or unbounded (per-item network call, unbounded scan, missing timeout), concurrency added (goroutine/worker/lock), a background job that writes |
| 4 | deploy/release pipeline, infrastructure as code, a resource limit, anything that can take the service down for everyone at once |

## Mobile & native
Only when a mobile app is in the set. See `checklists.md` for the specific checks.
| Points | Evidence |
|--------|----------|
| 0 | JS/TS-only change to a screen with no navigation, persistence, or platform API |
| 1 | new screen or navigation route; a copy or style change with a screenshot per platform |
| 2 | persistence or offline state change; a platform API used through an existing module; deep-link handling; a new permission *prompt* |
| 3 | new native module or native SDK bump; `app.json` / `eas.json` / entitlements / manifest change; a new permission *entry*; push or background-fetch handling; IAP or store metadata |
| 4 | Expo SDK / React Native / Gradle / Xcode toolchain bump, a binary-only change (cannot ship OTA), or anything in the app's startup/crash path (native splash, root navigator, error boundary, hydration) |

## Novelty
| Points | Evidence |
|--------|----------|
| 0 | follows an existing pattern in the same repo, author has touched the area before |
| 1 | new pattern in this repo, or the first change to this area in months |
| 2 | new dependency (any), or a large generated diff (hundreds of lines with uniform style) in one go |
| 3 | dependency major bump, or a new dependency handling data/auth/network |
| 4 | a new dependency with native code, an unpinned or pre-release version, or a fork of a dependency |

## Worked examples

**Copy fix in one screen, screenshot attached, CI green.**
blast 0 · reversibility 0 · data 0 · verification 0 · contract 0 · ops 0 · mobile 1 ·
novelty 0 → score **1** → trivial, auto-approve **yes** (all seven conditions visible).

**Add `address` to user API + read it in web, tests on api only.**
blast 3 (two services) · reversibility 2 (additive migration with down) · data 2 (new
user field) · verification 2 (web reader untested) · contract 2 (additive) · ops 0 ·
novelty 0 → highest 3 + four others at ≥2 = **7**; floor "contract changed" is 5, no
change → **7 high**. Two reviewers, merge order api → web, auto-approve **no — cross-service contract**.

**Rotate the JWT signing key handling.**
data 4 · verification 0 (tests cover it, CI green) · everything else under 2 → sum 4,
floor "secret handling" → **8 high**. The same change with CI red: verification 4 →
sum 5, floor 8, escalation +1 → **9 critical**.

**Expo SDK 52 → 53 bump, lockfile only, no screenshots.**
mobile 4 · reversibility 4 (binary release) · novelty 3 · verification 3 (no device
proof) → highest 4 + three others = 7; floor "native SDK bumped" 6 applies, and
verification is 3, so escalation adds 1 → **8 high**. Check list must include a device
launch log or screenshot per platform before submit (`mobile` skill).

**Story target: "Let users delete their account."**
Forecast: data 4 (PII deletion) · reversibility 4 (destructive) · contract 3 (every
service holding user rows) · verification 3 (a story) → 4 + 3 = 7; the ticket says it
deletes user data, so the destructive-data floor 8 applies, and verification 3 escalates
→ **9 critical, confidence: low**. Before building: which
services own user data, soft- vs hard-delete, retention law, the flag. Never
auto-approve; an agent may draft it only with a human owning the spec gate.
