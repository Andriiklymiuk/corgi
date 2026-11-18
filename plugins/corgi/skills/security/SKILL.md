---
name: security
description: "Use for a security pass on a change, a repo or the whole workspace: \"security review\", \"is this safe to ship\", \"check auth on these routes\", \"are we leaking secrets\", \"threat model this\". Maps what an attacker can reach, proves each finding on the running stack, fixes worst first. NOT a general PR review (review), a quality pass (quality) or a risk score (risk)."
---

# Security pass

Read `../_shared/conventions.md` first (finish what you start, attribution, unattended runs).

**Done when:** every way in was listed, each finding has a proof and a fix or a reason it
was left, and the report says what was not checked.

This is defence of the user's own code, on their own stack. Every probe runs against
localhost or an environment the user owns. Never probe a host you were not given, never
run a scan against production, never use a real customer's data as the test case.

```
scope ──▶ map the ways in ──▶ hunt by kind ──▶ prove on the local stack ──▶ fix ──▶ re-probe
                                                       │
                                                       └── cannot prove ──▶ a question, not a finding
```

## 1. Scope and map

- **Scope:** a diff (`git diff <base>...HEAD`), a service, or the workspace. On a diff,
  `corgi surface --base <base>` gives the public slice first: routes, env keys, exports.
- **The ways in, from the compose file and the code:** every HTTP route and who may call
  it, every queue consumer, webhook, cron, upload, deep link, admin tool and CLI flag
  that takes outside input. `corgi context --json` lists the services, ports and tunnels.
- **What is worth stealing or breaking:** credentials, personal data, money, the ability
  to act as another user. A finding matters by what it reaches.
- **Trust lines:** where input from outside crosses into a query, a shell, a file path,
  a template, another service, a model prompt. Findings live on these lines.

## 2. Hunt by kind, worst first

1. **Who may do this.** A route with no check; a check on the page and none on the API;
   an id in the URL that is never tied to the caller (ask for another user's record with
   your own token); a role read from the request instead of the session.
2. **Input that becomes code.** SQL built with string joins, a shell command with a
   variable in it, a path joined from a parameter (`../`), HTML rendered unescaped, a
   URL the server fetches for the caller, an object deserialised from the request.
3. **Secrets.** In the repo, in the git history, in logs, in an error sent to the client,
   in a bundle shipped to the browser or the app. A key in a client bundle is public.
4. **Sessions and tokens.** No expiry, no rotation, a token in a URL, a cookie without
   `HttpOnly`, `Secure` and `SameSite`, a reset link that works twice, a webhook that
   never checks its signature.
5. **Abuse without a bug.** No rate limit on login, signup, reset or anything that sends
   mail or costs money; an action that can be replayed; a list with no page size.
6. **Dependencies and build.** A known hole in a version in use (`npm audit`,
   `govulncheck ./...`, `pip-audit`, `bundle audit`); an install script from a package
   nobody chose; a CI job that prints or exposes a secret to a pull request from a fork.
7. **The stack itself.** A database or admin port bound to every interface; a debug
   route left on; a public tunnel onto seeded or real data; CORS that reflects any origin
   with credentials.
8. **Agents and models.** Text from a ticket, a web page or a file that reaches a prompt
   is input: it must not be able to start a tool, read a secret or post under the user's
   name. An agent's permissions are the least that does the job.

Candidates, adapt to the stack. Read each hit: some are fine.

```bash
grep -rnE '(password|secret|token|api_?key) *[:=] *["'"'"'][^"'"'"']{8,}' . --include='*.{ts,js,go,py,rb,swift,kt}'
grep -rnE 'exec\(|execSync\(|os/exec|subprocess\.|eval\(|dangerouslySetInnerHTML|innerHTML *=' .
grep -rnE '"SELECT .*" *\+|`SELECT [^`]*\$\{|fmt\.Sprintf\("(SELECT|UPDATE|DELETE|INSERT)' .
git log -p --all -S 'BEGIN PRIVATE KEY' --oneline | head
```

Never open an `.env` file to look for secrets: read `.env.example` and
`corgi env <service>` (values masked). `corgi env check` names keys a service misses.

## 3. Prove it, on the local stack

A finding is a request and its answer, not a suspicion.

- Bring the stack up (`run` skill), make two test users, and send the request that
  should be refused: `curl` with user A's token for user B's record. A 200 with B's data
  is the proof; a 403 or 404 closes the finding.
- Write the proof as a test that fails on the base branch. It is the regression test for
  the fix.
- **Cannot be proved here** (needs production data, a real payment, a third party) → it
  goes in the report as a question with what to check, not as a finding.
- A proof never leaves the machine: no real exploit against a live host, no payload
  posted in a public issue or PR. A serious hole in a public repo is reported to the
  maintainers privately; tell the user and let them send it.

## 4. Fix worst first

Rank by what the attacker gets, times how easy, times how exposed (public route before
admin tool).

- **Fix the class, not the line.** One missing owner check means every sibling route is
  read too. A helper that makes the safe way the short way beats ten patches.
- **A leaked secret is rotated, not only deleted.** Removing it from the code leaves it
  in the history and in every clone. Rotation is the person's step: put it under
  `## You do` with the exact place to rotate it.
- **Refuse by default.** A new route starts closed; an allow list beats a block list.
- **Smallest change.** No new auth framework inside a fix. Anything over a day, or a
  change to a public contract, is listed with its scenario and cost, not started.
- **The agent's own house:** `corgi agent harden` writes deny rules for secrets and
  destruction into the workspace's Claude settings; `corgi agent doctor --security`
  says what is still loose. Suggest both when the pass covers the workspace.

## 5. Report

```
high   GET /api/orders/:id returns any user's order: no owner check      fixed, test added
high   Stripe key in web bundle (public)                                 removed; rotate it (You do 1)
med    login has no rate limit                                           left: needs the gateway, half a day
none   npm audit: 2 high → 0; govulncheck: clean
not checked: production config, the mobile app, load
```

Severity is a word (high, medium, low) with the reason, never a bare number. Say plainly
what was not checked. No finding without its proof; no "looks secure".

## See also

- **[`risk`](../risk/SKILL.md)** scores how much review a change needs; **[`review`](../review/SKILL.md)** is the whole PR.
- **[`quality`](../quality/SKILL.md)** for defects that are not attacks, **[`deps`](../deps/SKILL.md)** to move off a version with a hole.
- `/security-review`, when the session has it, is a second read of the local diff.
