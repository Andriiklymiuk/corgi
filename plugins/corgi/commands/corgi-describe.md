---
description: Read corgi-compose.yml and produce a detailed Markdown description of every service plus a Mermaid relation diagram. Writes to docs/corgi-services.md by default.
---

You are producing a human-readable description of the corgi project in the current working directory: every db_service, service, required tool, and the relationships between them.

This is **read-only analysis** — do not modify `corgi-compose.yml`, do not run `corgi run`, do not start any container. Parsing only.

## Inputs

- `corgi-compose.yml` in cwd (or path supplied as `$ARGUMENTS`). If `$ARGUMENTS` looks like a filename ending in `.yml` / `.yaml`, treat as the input file. Otherwise treat as the output path (and use the default input).
- Default output path: `docs/corgi-services.md`. Create `docs/` if missing. If output file already exists, overwrite it but tell the user.

If no compose file exists, stop. Tell the user to run `/corgi-new` first.

## Step 1 — Parse

Read the compose file. Identify:

- `name`, `description`, top-level `useDocker`, `useAwsVpn`, `init`, `beforeStart`, `afterStart`.
- Every `required.<tool>` (why, install, checkCmd, optional).
- Every `db_services.<name>` — driver, port(s), credentials, version, healthCheck, seed source, `additional.*` (queues/buckets/services/jwtSecret/authUsers/image/etc.), manualRun.
- Every `services.<name>` — path/cloneFrom, branch, port, portAlias, healthCheck, manualRun, ignore_env, autoSourceEnv, copyEnvFromFilePath, environment, exports, runner, beforeStart/start/afterStart, scripts, tunnel block, depends_on_db, depends_on_services.

Do not invent fields. Only describe what is present. Use `references/yml-schema.md` (in this same plugin's skill) when a field is unfamiliar.

## Step 1.5 — Scrape each service's README (best-effort)

For every `services.<name>`, resolve a working-copy directory:

- If `path:` is set → `<path>`.
- Else if `cloneFrom:` is set → `corgi_services/services/<name>/` (corgi's clone target; see `RootServicesFolder`).

If the directory exists, look for the first hit (case-insensitive) of: `README.md`, `README.markdown`, `README.rst`, `README.txt`, `README`. Read at most 4000 lines / 200 KB; skip if larger.

From the README extract:

- **Tagline** — first non-heading, non-badge paragraph. Strip to one sentence (≤ 200 chars).
- **Badges** — every inline `[![label](image-url)](link-url)` (or `<a href="..."><img src="..."></a>`) whose **image URL** matches any of: `shields.io`, `img.shields.io`, `sonarcloud.io`, `codecov.io`, `coveralls.io`, `circleci.com`, `travis-ci.{org,com}`, `github.com/.+/actions/.+/badge.svg`, `goreportcard.com`, `pkg.go.dev/badge`, `npm`/`npmjs`, `docker.io`/`hub.docker.com`, `gitlab.com/.+/badges`, or **any URL with `/badge` in the path**. Capture `(label, image-url, link-url)`. Always keep the link target, not just the image.
- **Useful links** — any list items under headings matching `^#+\s*(Links|Useful Links|Resources|References|Documentation|Docs|See also)\b` until the next heading. Capture `[text](url)` pairs.
- **Repo / homepage hints** — top-of-file `Homepage:`, `Repository:`, `Docs:` lines, or first `https://` URL in a Markdown link inside the first 30 lines.
- **SonarCloud project key** — if any badge URL matches `sonarcloud.io/.../?project=<key>` or `sonarcloud.io/api/project_badges/.../?project=<key>`, surface the `<key>`.

Sanitize:

- Drop badges with broken markdown (missing parens, no link).
- Deduplicate by link URL.
- Cap to 10 badges + 10 useful links per service. If truncated, note `(+N more in README)`.
- Never embed raw HTML beyond `<br/>`; convert to Markdown links.

If a service has no working-copy dir, or no README, record `readme: not found` and continue. This is best-effort — do not fail the whole command if one README is malformed.

Do **not** read README.md from `db_services` paths or from the project root unless that's also a service's `path:`. README scraping is per-service only.

## Step 2 — Build the relationship graph

For each service collect:

- **DB edges**: `service --(envAlias or driver prefix)--> db_service` for every entry in `depends_on_db`.
- **Service edges**: `service --(envAlias)--> service` for every entry in `depends_on_services`. Edge label `envAlias` (or service name if blank), plus any `suffix` in parens.
- **Export edges**: when a consumer references `${producer.VAR}` inside its `environment:`, draw a **dotted** arrow `producer -.->|VARS| consumer` (Mermaid syntax `-.->|...|`) listing the var(s) the consumer actually references. Detect by scanning each service's `environment` list for the `${name.VAR}` pattern where `name` matches another service's yaml key.
- **Tool edges**: every entry in `required:` is a dependency of the whole project, not a single service. Render as a separate "Required tools" cluster.
- **Tunnel edges**: a service with `tunnel:` block gets a `🌐 public` node attached.

Detect cycles in `depends_on_services` and call them out explicitly (corgi errors on cycles at runtime; surface them here too).

## Step 3 — Write the doc

Use the structure in `skills/corgi/references/describe-output.md`. Sections in order:

1. Title + one-line `description` + project metadata table (name, useDocker, useAwsVpn).
2. **Relationship diagram** — a single Mermaid `graph LR` block. Conventions:
   - Service node: `svc_<name>(["<name><br/>:<port>"])`
   - DB node: `db_<name>[("<name><br/>(<driver>)<br/>:<port>")]`
   - Tool node: `tool_<name>{{<name>}}` inside `subgraph required["Required tools"]`
   - DB edge: solid arrow with label `-->|envAlias or driver prefix|`
   - Service edge: solid arrow with label `-->|envAlias|`
   - Export edge: dotted arrow `-.->|VAR1, VAR2|`
   - Tunnel: `tun_<svc>(((🌐 hostname))) --> svc_<svc>`
   - Group all services in `subgraph services` and all dbs in `subgraph databases`.
3. **Required tools** — table: tool, why, checkCmd, optional.
4. **Databases** — one subsection per `db_service`. Include driver, host:port (+ port2 if set), credentials placeholder (`user`/`***`), databaseName, version, healthCheck, seed source (one line: `seedFromFilePath` / `seedFromDbEnvPath` / inline `seedFromDb`), and any `additional.*` keys present.
5. **Services** — one subsection per service. Include:
   - Source: `path:` or `cloneFrom:` + branch. Note resolved working-copy dir if different.
   - Port + portAlias.
   - Healthcheck URL (compose `http://localhost:<port><healthCheck>` when both present).
   - **From README** (omit block entirely if scrape returned nothing):
     - Tagline (one line).
     - Badges as a bullet list of Markdown links.
     - Useful links as a bullet list.
     - Repo / homepage / docs lines (only the ones found).
     - SonarCloud project key (with link `https://sonarcloud.io/project/overview?id=<key>`).
   - Lifecycle commands: beforeStart / start / afterStart as fenced shell blocks.
   - **DB dependencies** table: db name, envAlias, generated env-var prefix.
   - **Service dependencies** table: target service, envAlias, suffix, resolved URL hint (`http://<localhostNameInEnv|localhost>:<targetPort><suffix>`).
   - **Exports** list, marking each as re-export vs inline literal.
   - **Inbound env references** list — other services that consume this one's exports (so the reader sees the flow both directions).
   - **Tunnel** block if present: provider, hostname, name.
   - **Scripts** if present.
   - Flags actually set on this service (manualRun, ignore_env, autoSourceEnv=false, interactiveInput, useDocker effect, runner.name).
6. **Lifecycle hooks** — project-level `init`, `beforeStart`, `afterStart` as fenced blocks.
7. **Cycles & warnings** — empty if none. List detected `depends_on_services` cycles, exports referencing non-`depends_on_services` producers, `${producer.VAR}` where `VAR` is not in `exports`, missing `path`/`cloneFrom`, etc.

Keep prose tight. Tables and lists over paragraphs. Don't restate the schema — describe **this** project.

## Step 4 — Hand off

Report to the user:

- Path written.
- Counts: N services, M databases, K required tools, R READMEs scraped (and L services with no README found).
- Any warnings from Step 3 §7, copied verbatim.
- Suggest: open the file, or paste the Mermaid block into a GitHub PR / mermaid.live for rendering.
- If services missing READMEs were cloned ones, suggest `corgi init` (if dirs don't exist) or note the upstream repo just has no README.

Do not run `corgi doctor` or `corgi status` as part of this command — it's a documentation command, not a verification one. If the user wants live state, point them at `corgi doctor` / `corgi status` separately.
