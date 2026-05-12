---
description: Read corgi-compose.yml and produce a compact Markdown description of every service plus a Mermaid relation diagram. Writes to docs/corgi-services.md by default.
---

Produce a **compact** human-readable description of the corgi project in the current working directory: db_services, services, required tools, relationships. Output must stay small — skip empty fields, no boilerplate, no placeholders.

Read-only. Do not modify `corgi-compose.yml`, do not run `corgi run`, do not start containers.

## Inputs

- `corgi-compose.yml` in cwd (or path via `$ARGUMENTS`). If `$ARGUMENTS` ends in `.yml`/`.yaml`, treat as input file. Else treat as output path.
- Default output: `docs/corgi-services.md`. Create `docs/` if missing. Overwrite existing; tell the user.

No compose file → stop, tell user to run `/corgi-new`.

## Step 1 — Parse

Identify in compose:

- Top level: `name`, `description`, `useDocker`, `useAwsVpn`, `init`, `beforeStart`, `afterStart`.
- `required.<tool>`: `why`, `install`, `checkCmd`, `optional`.
- `db_services.<name>`: driver, port(s), credentials, version, healthCheck, seed source, `additional.*`, manualRun.
- `services.<name>`: path/cloneFrom, branch, port, portAlias, healthCheck, manualRun, ignore_env, autoSourceEnv, copyEnvFromFilePath, environment, exports, runner, beforeStart/start/afterStart, scripts, tunnel, depends_on_db, depends_on_services.

Only describe what is present. See `references/yml-schema.md` for unknown fields.

## Step 1.5 — README scrape (best-effort, terse)

For each `services.<name>`, resolve working-copy dir:

- `path:` set → `<path>`.
- Else `cloneFrom:` set → `corgi_services/services/<name>/`.

If dir exists, find first match (case-insensitive): `README.md`, `README.markdown`, `README.rst`, `README.txt`, `README`. Skip if > 4000 lines / 200 KB.

Extract **only these three things** (no full badge dump, no useful-links section):

1. **Tagline** — first non-heading, non-badge paragraph. Strip to one sentence ≤ 200 chars.
2. **SonarCloud project key** — if any badge URL matches `sonarcloud.io/.../?project=<key>` or `sonarcloud.io/api/project_badges/.../?project=<key>`, capture `<key>` (one).
3. **Repo URL** — first GitHub/GitLab `https://` link in first 30 lines, or `Repository:` line.

That's it. Do not extract full badge lists or useful-links sections — they bloat the doc.

No working-copy dir, or no README → skip the README line for that service. Don't emit "readme: not found".

Per-service only — never scrape `db_services` paths.

## Step 2 — Build relationship graph

- **DB edges**: `service --(envAlias or driver prefix)--> db_service` for each `depends_on_db`.
- **Service edges**: `service --(envAlias)--> service` for each `depends_on_services`. Label `envAlias` (or service name if blank); append `(suffix)` if set.
- **Export edges**: when consumer `environment:` references `${producer.VAR}` where `producer` matches another service's yaml key, draw `producer -.->|VARS| consumer` listing the var(s) referenced.
- **Tool nodes**: each `required:` entry, no edges (project-wide).
- **Tunnel**: services with `tunnel:` block get a `🌐` node.

Detect cycles in `depends_on_services`; surface in warnings.

## Step 3 — Write the doc

Follow `references/describe-output.md` exactly. **Compactness rules (mandatory):**

1. **Skip empty/default fields.** No `_not set_`, no `none`, no `—` rows for missing fields. Field absent → omit line.
2. **No project metadata table** unless `useDocker` or `useAwsVpn` is set. Title + optional blockquote description is enough.
3. **Required tools** → single table, only if non-empty. Drop "Why" column unless any tool has `why:` set.
4. **Databases** → single shared table (`name | driver | host:port | db / user`). Only emit a per-DB subsection for a db that has `version`, `healthCheck`, seed source, or `additional.*`. Plain databases stay in the table only.
5. **Services** → `### <name> `:<port>`` header, then compact bullets and one combined **Deps** table (db + svc deps merged with a `kind` column). No `#### Lifecycle commands` / `#### DB dependencies` / `#### Service dependencies` / `#### Exports` subheadings — use bold inline labels (`**Deps**`, `**Exports**`, `**Lifecycle**`, etc.).
6. **README block per service** → one line max: `**README** > <tagline>. [Sonar: <key>](url). [Repo](url).` Omit any of the three parts that aren't present. Omit the whole line if all three missing.
7. **Drop "Inbound env references" / "Consumed by"** — the diagram already shows reverse edges.
8. **Drop the "Environment" prose section.** Only surface env entries that reference `${producer.VAR}` (export consumers) under `**Cross-service env refs**`, since those drive dotted graph edges. Plain `${DB_HOST}`-style refs are implied by the deps table — don't list them.
9. **Lifecycle blocks** → single fenced `sh` block per service with `# beforeStart` / `# start` / `# afterStart` comment markers; omit any sub-section that is empty or `echo`-only; omit the whole block if all three are empty/echo. Same rule for project-level `## Lifecycle hooks (project)` — skip the section if all hooks are echo-only or empty.
10. **Cycles & warnings** → only if non-empty. No "None." line.
11. **Mermaid subgraphs** → wrap a group only when it has ≥ 2 nodes. Single service or single db → render the node bare, no `subgraph` wrapper.

Tables > lists > paragraphs. Describe **this** project — don't restate the schema.

## Step 4 — Hand off

Tell the user:

- Path written (overwrite warning if pre-existing).
- Counts: `N services, M databases, K required tools, R READMEs scraped (L missing)`.
- Any cycles/warnings, copied verbatim.
- Open the file or paste the Mermaid block into mermaid.live / a PR for render.
- If missing READMEs are from `cloneFrom:` services with no working-copy dir → suggest `corgi init`.

Do not run `corgi doctor` / `corgi status` — this is doc-only. Point users at those for live state.
