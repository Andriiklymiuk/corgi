// Draws the README pictures: terminal sessions of the commands that matter,
// as HTML for Chrome to screenshot, one file per animation frame. Every line
// is the format the code prints (cmd/*.go, utils/*.go), so the pictures do
// not drift from the CLI. scripts/capture-showcase.sh turns the frames into
// docs/media/*.png and *.gif.
import { mkdirSync, writeFileSync, rmSync, readFileSync } from "node:fs";

// Colours: the ANSI palette of utils/art, on a dark terminal.
const c = { g: "#3fb950", r: "#ff7b72", y: "#e3b341", b: "#79c0ff", c: "#56d4dd", d: "#8b949e", w: "#e6edf3", m: "#d2a8ff" };
const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;");
/** {g}green{/} {r}red{/} {y}yellow{/} {b}blue{/} {c}cyan{/} {d}dim{/} {m}magenta{/} {B}bold{/} */
const mark = (s) => esc(s).replace(/\{([grybcdmB])\}/g, (_, k) => (k === "B" ? `<b>` : `<span style="color:${c[k]}">`)).replace(/\{\/\}/g, "</span>").replace(/<\/span>(?=[^<]*<\/b>)/g, "</b>");
const line = (s) => s.startsWith("{Q}") ? `<div class="qr">${esc(s.slice(3))}</div>` : `<div class="l">${s === "" ? "&nbsp;" : mark(s)}</div>`;

const css = `
  *{box-sizing:border-box}
  html,body{margin:0;background:#0d1117;font-family:-apple-system,"SF Pro Text",Inter,Helvetica,Arial,sans-serif;-webkit-font-smoothing:antialiased;color:#e6edf3}
  .stage{position:absolute;left:0;top:0;padding:28px;display:flex;gap:28px;align-items:flex-start}
  .term{width:900px;background:#161b22;border:1px solid #30363d;border-radius:12px;box-shadow:0 24px 70px rgba(0,0,0,.6);overflow:hidden}
  .bar{height:38px;display:flex;align-items:center;padding:0 14px;background:#21262d;border-bottom:1px solid #30363d;font-size:12px;color:#8b949e;position:relative}
  .bar i{width:12px;height:12px;border-radius:50%;background:#ff5f57;margin-right:8px}.bar i+i{background:#febc2e}.bar i+i+i{background:#28c840}
  .bar span{position:absolute;left:0;right:0;text-align:center;pointer-events:none}
  .body{padding:14px 18px;font:13.5px/1.55 "SF Mono",ui-monospace,Menlo,Consolas,monospace;white-space:pre;color:#e6edf3}
  .l{min-height:21px}
  .cur{display:inline-block;width:8px;height:16px;background:#e6edf3;vertical-align:-3px;margin-left:2px}
  .body b{font-weight:600;color:#fff}
  .qr{font:13.5px/1 "Menlo",monospace;letter-spacing:0;color:#fff;white-space:pre;margin:2px 0}
  /* phone */
  .phone{width:300px;height:620px;border-radius:44px;background:#0b0b0d;border:3px solid #2a2a2e;box-shadow:0 30px 80px rgba(0,0,0,.6),inset 0 0 0 2px #000;position:relative;overflow:hidden;flex:none}
  .notch{position:absolute;left:50%;top:10px;transform:translateX(-50%);width:100px;height:26px;border-radius:14px;background:#000;z-index:3}
  .screen{position:absolute;inset:0;overflow:hidden;font-family:-apple-system,system-ui,sans-serif}
  .clock{color:#fff;text-align:center;margin-top:90px;font-size:64px;font-weight:200;letter-spacing:-2px}
  .date{color:#fff;text-align:center;font-size:17px;opacity:.85}
  .lock{background:linear-gradient(160deg,#1a2352,#4b2a6e 60%,#a04a4a)}
  .cam{background:#111;position:relative}
  .cam .view{position:absolute;inset:0;display:flex;align-items:center;justify-content:center;background:#2b2b2b}
  .cam .view .qr{color:#111;background:#fff;padding:10px;font-size:8px;line-height:1;border-radius:4px;transform:rotate(-3deg)}
  .cam .frame{position:absolute;left:40px;right:40px;top:170px;bottom:250px;border:2px solid rgba(255,225,0,.9);border-radius:12px}
  .cam .pill{position:absolute;left:50%;bottom:150px;transform:translateX(-50%);background:#ffd60a;color:#111;font-size:13px;font-weight:600;padding:8px 14px;border-radius:20px;white-space:nowrap;box-shadow:0 6px 20px rgba(0,0,0,.4)}
  .cam .shutter{position:absolute;left:50%;bottom:40px;transform:translateX(-50%);width:66px;height:66px;border-radius:50%;background:#fff;box-shadow:0 0 0 4px #111,0 0 0 6px #fff}
  /* the launcher page, its own palette (cmd/mcp_launcher.go) */
  .app{background:#0a0b0e;color:#eceef2;position:absolute;inset:0;padding:54px 16px 0;font-size:13px}
  .brand{display:flex;align-items:center;gap:10px;margin-bottom:8px}
  .logo{width:36px;height:36px;border-radius:12px;background:linear-gradient(135deg,#1e2634,#131824);border:1px solid #22242b;display:flex;align-items:center;justify-content:center;font-size:19px}
  .brand h1{font-size:17px;margin:0}.brand small{display:block;color:#8f94a3;font-size:11px}
  .ws{background:#141519;border:1px solid #22242b;border-radius:11px;padding:11px 11px 8px;margin:8px 0}
  .head{display:flex;align-items:center;gap:9px}
  .dot{width:8px;height:8px;border-radius:50%;background:#3a4152;flex:none}.dot.live{background:#6ee787}.dot.starting{background:#6ee787;opacity:.45}.dot.attention{background:#ffa657}
  .name{font-weight:600;font-size:14px;flex:1}
  .path{color:#63677a;font-size:10px;font-family:ui-monospace,Menlo,monospace;margin:2px 0 0 17px}
  .meta{font-size:11px;color:#63677a;margin:4px 0 0 17px}.meta .live{color:#6ee787}.meta .warn{color:#ff7b72}.meta .why{color:#8f94a3}
  .meta span+span::before{content:"·";margin:0 5px;color:#63677a}
  .actions{display:flex;gap:5px;margin-top:9px;align-items:center;flex-wrap:wrap}
  .go{background:#6ee787;color:#05110a;font-weight:700;font-size:12px;padding:6px 14px;border-radius:8px}
  .go.tap{box-shadow:0 0 0 4px rgba(110,231,135,.35)}
  .chip{border:1px solid #22242b;color:#8f94a3;font-size:10.5px;padding:5px 8px;border-radius:8px}
  .top{display:flex;justify-content:space-between;margin-top:8px;padding-top:7px;border-top:1px solid #1a1c22;font-size:12px}
  .top .when{color:#63677a;font-size:10.5px}
  .sum{color:#8f94a3;font-size:11px;margin:0 0 6px 2px}.sum.hot{color:#ff7b72;font-weight:600}
`;

const term = (title, lines, { rows = lines.length, cursor = false, extra = "" } = {}) => {
	const body = lines.map(line).join("") + (cursor ? `<div class="l"><span class="cur"></span></div>` : "");
	const pad = Math.max(0, rows - lines.length - (cursor ? 1 : 0));
	return `<div class="term"><div class="bar"><i></i><i></i><i></i><span>${esc(title)}</span></div><div class="body">${body}${`<div class="l">&nbsp;</div>`.repeat(pad)}</div></div>${extra}`;
};

const page = (inner, width = 960) => `<!doctype html><meta charset="utf-8"><title>corgi</title><style>${css}</style><div class="stage" style="width:${width}px">${inner}</div>`;

/** Frames that grow: the first n lines of the script at each cut. */
const grow = (lines, cuts) => cuts.map((n) => lines.slice(0, n));

const out = "docs/media/frames";
rmSync(out, { recursive: true, force: true });
mkdirSync(out, { recursive: true });
const scenes = {};
const scene = (name, frames) => { scenes[name] = frames.length; frames.forEach((html, i) => writeFileSync(`${out}/${name}-${i}.html`, html)); };

// ---- corgi run ------------------------------------------------------------------
{
	const s = [
		"{g}${/} {B}corgi run{/}",
		"Path /Users/me/dev/stack/api does not exist for service api. It should be cloned.",
		"",
		"🚀 🤖 Executing command for api:  {g}git clone https://github.com/acme/api.git api{/}",
		"Cloning into 'api'...",
		"{g}✅{/} Db service db was successfully created",
		"{b} 🤖 Starting database db {/}",
		"",
		"🚀 🤖 Executing command for db:  {g}docker compose -f corgi_services/db/docker-compose.yml up -d{/}",
		"{b} ⛅ GETTING DATABASE DUMP for db {/}",
		"✅ Successfully added database dump to db",
		"{b} 🎉  db  IS SEEDED {/}",
		"{b} 🐶 RUNNING SERVICE api {/}",
		"",
		"Before start commands:",
		"🚀 🤖 Executing command for api:  {g}go mod download{/}",
		"",
		"Start commands:",
		"🚀 🤖 Executing command for api:  {g}go run .{/}",
		"{b} 🐶 RUNNING SERVICE web {/}",
		"⏳ web dependency api satisfied (ready)",
		"🚀 🤖 Executing command for web:  {g}yarn dev{/}",
		"{c}[api]{/} listening on :7012",
		"{c}[web]{/} ➜  Local: http://localhost:5173/",
		"😉 corgi is running — Ctrl+C to stop",
	];
	const cuts = [1, 2, 5, 7, 9, 12, 13, 16, 19, 22, 24, 25];
	scene("run", grow(s, cuts).map((l, i) => page(term("corgi run — stack", l, { rows: s.length, cursor: i < cuts.length - 1 }))));
}

// ---- one feature across repos, then mission control ----------------------------
{
	const s = [
		"{g}${/} {B}corgi run --feature ABC-123{/}",
		"feature: api → ABC-123 @ /Users/me/dev/stack/corgi_services/.worktrees/api-ABC-123",
		"feature: web → ABC-123 @ /Users/me/dev/stack/corgi_services/.worktrees/web-ABC-123",
		"feature: mobile → no ABC-123 branch, staying on current checkout",
		"{b} 🤖 Starting database db {/}",
		"{b} 🐶 RUNNING SERVICE api {/}",
		"{b} 🐶 RUNNING SERVICE web {/}",
		"{b} 🐶 RUNNING SERVICE mobile {/}",
		"😉 corgi is running — Ctrl+C to stop",
	];
	const mc = (t) => [
		"{g}${/} {B}corgi mc --watch{/}",
		"🛰️  corgi mission-control",
		"  {g}✅ db                           running   {/}",
		`  {g}✅ api                          running   {/}  {c}ABC-123{/} *  PR #412 [draft] CI:${t ? "success" : "pending"}`,
		"  {g}✅ web                          running   {/}  {c}ABC-123{/}  PR #98 [open] CI:success",
		"  {g}✅ mobile                       running   {/}  {c}main{/}",
		"",
		`{c}🛰️  4 services every 3s — 4 up, 0 down, 2 open PRs — last update 14:07:${t ? "36" : "33"}{/}`,
	];
	const frames = [...grow(s, [1, 4, 9]).map((l, i) => term("corgi run --feature", l, { rows: 9, cursor: i < 2 })), term("corgi mc", mc(0), { rows: 9 }), term("corgi mc", mc(1), { rows: 9 })];
	scene("feature", frames.map((t) => page(t)));
}

// ---- status -w ----------------------------------------------------------------------
{
	const f = (webUp, t, up) => [
		"{g}${/} {B}corgi status -w{/}",
		"🩺 corgi status",
		"  {g} ✅ db_services.db (postgres)               localhost:5432 listening{/}",
		"  {g} ✅ services.api                            http://localhost:7012/health [HTTP 200]{/}",
		webUp ? "  {g} ✅ services.web                            http://localhost:5173 [HTTP 200]{/}" : "  {r} ❌ services.web                            localhost:5173 not listening{/}",
		"",
		`{c}👀 watching 3 targets every 2s — last update 14:02:${t} (${up} up, ${3 - up} down) — Ctrl+C to stop{/}`,
	];
	scene("status", [f(0, "09", 2), f(0, "11", 2), f(1, "13", 3), f(1, "15", 3)].map((l) => page(term("corgi status -w", l))));
}

// ---- doctor, then doctor --fix -----------------------------------------------------
{
	const doc = (ok) => [
		"{g}${/} {B}corgi doctor{/}",
		"🤖 Required: {g}docker{/}",
		"✅ docker is found",
		"🤖 Required: {g}go{/}",
		"✅ go is found",
		"✅ {g}Docker daemon is running{/}",
		"🔌 Port availability:",
		"  {g}✅ 5432 free — for db_services.db (postgres){/}",
		ok ? "  {g}✅ 7012 free — for services.api{/}" : "  {r}❌ 7012 busy — needed for services.api — held by: node (pid 41221){/}",
		"  {g}✅ 5173 free — for services.web{/}",
		"",
		ok ? "{g}🎉 Doctor: all checks passed{/}" : "{r}❌ Doctor: one or more checks failed{/}",
	];
	const fix = [
		"{g}${/} {B}corgi doctor --fix{/}",
		"Fix port:7012? {g}y{/}",
		"✅ fixed: port:7012",
	];
	scene("doctor", [
		term("corgi doctor", doc(0), { rows: 12 }),
		term("corgi doctor", fix, { rows: 12, cursor: true }),
		term("corgi doctor", doc(1), { rows: 12 }),
	].map((t) => page(t)));
}

// ---- db: snapshot, restore, shell ---------------------------------------------------
{
	const s = [
		"{g}${/} {B}corgi db snapshot nightly{/}",
		'📦 snapshot "nightly" saved (postgres:16-alpine, pg16/arm64, 4823104 bytes)',
		"",
		"{g}${/} {B}corgi db snapshot --list{/}",
		"nightly              pg16/arm64  4823104 bytes  2026-09-08T22:10:03Z",
		"pre-migration        pg16/arm64  4102331 bytes  2026-09-07T09:44:12Z",
		"",
		"{g}${/} {B}corgi db restore nightly{/}",
		'⚠️  This WIPES the current "db" data volume and restores nightly.tar.zst. Continue? [y/N] {g}y{/}',
		'✅ restored "db" from nightly.tar.zst',
		"",
		"{g}${/} {B}corgi db shell{/}",
		"{c}🐚 Opening postgres shell for db...{/}",
		"psql (16.4)",
		"app=# {B}select count(*) from orders;{/}",
		" count",
		"-------",
		"  4812",
		"app=#",
	];
	scene("db", grow(s, [1, 2, 4, 6, 8, 9, 10, 12, 14, 15, 19]).map((l, i) => page(term("corgi db", l, { rows: s.length, cursor: i < 10 }))));
}

// ---- CI: the whole stack on the PR branches, one e2e suite ---------------------------
{
	const s = [
		"{d}▸ Run{/} {B}corgi init --depth 1 --feature PR-812{/}",
		"feature: api → PR-812",
		"feature: web → PR-812",
		"feature: mobile → no PR-812 branch, staying on its default",
		"✅ Service api was successfully created",
		"✅ Service web was successfully created",
		"✅ Service mobile was successfully created",
		"{d}▸ Run{/} {B}corgi run --feature PR-812 --detach --wait{/}",
		"🐶 corgi running detached — 4 service(s), state: /home/runner/work/stack/.corgi/run-state.json",
		"{c}⏳ waiting up to 5m0s for 4 targets to become healthy...{/}",
		"{g}🎉 all 4 targets healthy{/}",
		"{d}▸ Run{/} {B}corgi test --e2e{/}",
		"🚀 🤖 Executing command for e2e:  {g}npx playwright test{/}",
		"  Running 24 tests using 4 workers",
		"  24 passed (1.4m)",
		"📦 collected 2 e2e artifact path(s) into /home/runner/work/stack/corgi_artifacts/e2e",
		"✅ e2e passed",
	];
	scene("ci", grow(s, [1, 4, 7, 8, 9, 11, 12, 15, 17]).map((l, i) => page(term("GitHub Actions · e2e · PR #812", l, { rows: s.length, cursor: i < 8 }))));
}

// ---- an agent takes the ticket ---------------------------------------------------------
{
	const s = [
		"{d}>{/} {B}/corgi:stories ABC-123{/}",
		"",
		"{g}●{/} {B}Read{/} corgi-compose.yml",
		"  ⎿  api (go) · web (vite) · mobile (expo) · db (postgres, seeded)",
		"{g}●{/} {B}Linear{/} ABC-123 · Referral codes at checkout",
		"  ⎿  api: POST /referrals + migration · web: code field · mobile: share sheet",
		"{g}●{/} {B}Edit{/} api/internal/referrals/handler.go",
		"{g}●{/} {B}Edit{/} api/migrations/0042_referrals.sql",
		"{g}●{/} {B}Edit{/} web/src/checkout/Referral.tsx",
		"{g}●{/} {B}Edit{/} mobile/app/checkout.tsx",
		"{g}●{/} {B}Bash{/}(corgi run --feature ABC-123 --detach --wait)",
		"  ⎿  🐶 corgi running detached — 4 service(s)",
		"{g}●{/} {B}Bash{/}(corgi status --ready --timeout 2m)",
		"  ⎿  🎉 all 4 targets healthy",
		"{g}●{/} {B}Bash{/}(corgi test --e2e)",
		"  ⎿  ✅ e2e passed",
		"{g}●{/} {B}Bash{/}(gh pr create --draft) in api, web, mobile",
		"  ⎿  https://github.com/acme/api/pull/412",
		"  ⎿  https://github.com/acme/web/pull/98",
		"  ⎿  https://github.com/acme/mobile/pull/231",
		"{g}●{/} Three draft PRs. The stack ran with all three branches and the e2e suite passed. Nothing is merged.",
	];
	scene("stories", grow(s, [1, 4, 6, 8, 10, 12, 14, 16, 20, 21]).map((l, i) => page(term("Claude Code — stack", l, { rows: s.length, cursor: i < 9 }))));
}

// ---- the phone: agent up, scan, tap a repo -----------------------------------------------
{
	const qr = readFileSync("scripts/showcase-qr.txt", "utf8").trimEnd().split("\n");
	const up = [
		"{g}${/} {B}corgi agent up{/}",
		"",
		"  ✓ workspace acme-stack (registered)",
		"  ✓ agent daemon running (pid 41902)",
		"  ✓ starts at login (launchd) — survives a reboot",
		"  ✓ public endpoint: https://blue-fox-42.trycloudflare.com/mcp",
		"",
		"  📱 scan to pair (single use, 10 minutes):",
		"",
		"{Q}" + qr.map((l) => `   ${l}`).join("\n"),
		"",
		"    or open: https://blue-fox-42.trycloudflare.com/pair#A7K2M9",
		"",
		"  after scanning, the phone opens the launcher — tap a repo to start:",
		"    https://blue-fox-42.trycloudflare.com/app",
	];
	const card = ({ name, path, branch, dot, meta, btn, tap, session }) => `<div class="ws"><div class="head"><span class="dot ${dot}"></span><span class="name">${name}</span></div><div class="path">${path} <span style="color:#8f94a3">${branch}</span></div><div class="meta">${meta}</div><div class="actions"><span class="go${tap ? " tap" : ""}">${btn}</span><span class="chip">sessions</span><span class="chip">open in <b>app</b></span><span class="chip">options</span></div>${session ? `<div class="top"><span>${session}</span><span class="when">13:04</span></div>` : ""}</div>`;
	const launcher = (t) => `<div class="app"><div class="brand"><div class="logo">🐶</div><div><h1>corgi</h1><small>andrii-mbp · 3 workspaces</small></div></div>
	<div class="sum${t < 2 ? " hot" : ""}">${t < 2 ? "1 session needs you" : "2 live · 1 needs you"}</div>
	${card({ name: "acme-stack", path: "~/dev/acme-stack", branch: "main", dot: t === 0 ? "" : t === 1 ? "starting" : "live", meta: t === 0 ? "<span>nothing running</span><span>default</span>" : t === 1 ? '<span class="live">starting</span><span>default</span>' : '<span class="live">1 live</span><span>12s</span><span>default</span>', btn: t === 0 ? "Start" : "Open", tap: t === 1, session: t >= 2 ? "acme-stack · main" : "" })}
	${card({ name: "recipe-app", path: "~/dev/recipe-app", branch: "feat/search*", dot: "live", meta: '<span class="live">1 live</span><span>2h 10m</span><span>default</span>', btn: "Open", session: "recipe-app · search index" })}
	${card({ name: "client-app", path: "~/work/client-app", branch: "main", dot: "attention", meta: '<span class="warn">needs you</span><span class="why">waiting: allow or deny the edit</span><span>work</span>', btn: "Open", session: "client-app · onboarding copy" })}
	</div>`;
	const phone = (screen) => `<div class="phone"><div class="notch"></div><div class="screen">${screen}</div></div>`;
	const lock = `<div class="lock" style="position:absolute;inset:0"><div class="clock">13:04</div><div class="date">Tuesday 9 September</div></div>`;
	const cam = `<div class="cam" style="position:absolute;inset:0"><div class="view"><div class="qr">${esc(qr.join("\n"))}</div></div><div class="frame"></div><div class="pill">Open “blue-fox-42.trycloudflare.com”</div><div class="shutter"></div></div>`;
	const frames = [
		[grow(up, [1])[0], lock, true],
		[up, lock, false],
		[up, cam, false],
		[up, launcher(0), false],
		[up, launcher(1), false],
		[up, launcher(2), false],
	];
	scene("phone", frames.map(([l, screen, cur]) => page(term("corgi agent up", l, { rows: up.length, cursor: cur, extra: phone(screen) }), 1290)));
}


// ---- the launcher on the phone: three tabs, and a ticket moved from its row ----
{
	const qr = readFileSync("scripts/showcase-qr.txt", "utf8").trimEnd().split("\n");
	const up = [
		"{g}${/} {B}corgi agent up{/}",
		"",
		"  ✓ workspace acme-api (registered)",
		"  ✓ agent daemon running (pid 41902)",
		"  ✓ starts at login (launchd) — survives a reboot",
		"  ✓ public endpoint: https://blue-fox-42.trycloudflare.com/mcp",
		"",
		"  📱 scan to pair (single use, 10 minutes):",
		"",
		"{Q}" + qr.map((l) => `   ${l}`).join("\n"),
		"",
		"    or open: https://blue-fox-42.trycloudflare.com/pair#A7K2M9",
		"",
		"  the phone opens on the inbox — what the watch has seen:",
		"    https://blue-fox-42.trycloudflare.com/app",
	];

	// Every string below is one the page prints: cmd/mcp_launcher.go.
	const tabs = (on, n = { inbox: 3, sessions: 3, stacks: 2, laptop: 0, settings: 0 }) => `<div class="tabs">` +
		[["inbox", "Inbox"], ["sessions", "Sessions"], ["stacks", "Stacks"], ["laptop", "Laptop"], ["settings", "Settings"]]
			.map(([k, label]) => `<span class="tab${on === k ? " sel" : ""}${on === "→" + k ? " tap" : ""}">${label}${n[k] ? `<i>${n[k]}</i>` : ""}</span>`)
			.join("") + `</div>`;

	const ev = ({ ref, kind, title, acts, box, aim }) => `<div class="ev">
		<div class="ehead">${box ? `<i class="box"></i>` : ""}<span class="eref">${ref}</span><span class="ekind">${kind}</span></div>
		<div class="etitle">${title}</div>
		<div class="erow">${acts.map((a) => a === "Work on it"
			? `<span class="eb pri">${a}</span>`
			: `<span class="eb${aim && a === "Move…" ? " tapme" : ""}">${a}</span>`).join("")}</div></div>`;

	const inbox = (state, aim) => `
		<p class="sum">From the tracker and your pull requests</p>
		<div class="grp">acme-api</div>
		${ev({ ref: "ABC-123", kind: "NEW ISSUE", box: 1, aim, acts: ["Open issue", "Work on it", "Move…", "Ignore"],
			title: state === "moved" ? "Login redirect loops after SSO <span class=\"col\">In Progress</span>" : "Login redirect loops after SSO" })}
		${ev({ ref: "ABC-128", kind: "NEW ISSUE", box: 1, acts: ["Open issue", "Work on it", "Move…", "Ignore"],
			title: "Cache the workspace registry between polls" })}
		${ev({ ref: "acme/web!41", kind: "PR REVIEW", acts: ["Open PR", "Work on it", "Ignore"],
			title: "Draft: retry the upload on a 502" })}
		<div class="grp">worked on for you</div>
		<div class="ev done"><div class="ehead"><span class="eref">acme/web!38</span><span class="pill">1 PR</span></div>
			<div class="etitle">opened merge_requests/44 · 6m</div></div>`;

	const sheet = (picked) => `<div class="scrim"><div class="sheet"><div class="grab"></div>
		<div class="sh3">Move ABC-123 to</div>
		${["Backlog", "Ready", "In Progress", "In Review", "Staging", "Done", "Assign to me"]
			.map((n) => `<div class="opt${picked === n ? " tap" : ""}">${n}</div>`).join("")}</div></div>`;

	const sess = (dot, label, detail, badge) => `<div class="sess"><span class="sdot ${dot}"></span><span class="slabel">${label}</span><span class="sdetail">${detail}</span>${badge ? `<span class="sbadge">${badge}</span>` : ""}</div>`;
	const sessions = `<p class="sum hot">Claude sessions on this machine · 1 waiting on you · 2 working</p>
		${sess("needs", "acme-api", "permission: Bash go test · ctx 72%", "work")}
		${sess("working", "ABC-123 login redirect", "Edit registry.go · ctx 43%")}
		${sess("working", "web", "Bash npm test · ctx 58%")}
		<div class="acct"><span><b>default</b><i class="bar"><i style="width:55%"></i></i>55% · resets 5:10pm</span><span><b>work</b><i class="bar hot"><i style="width:100%"></i></i>100% · resets 1:10pm</span></div>`;

	const card = ({ name, path, branch, dot, meta, usage, btn }) => `<div class="ws">
		<div class="head"><span class="dot ${dot}"></span><span class="name">${name}</span></div>
		<div class="path">${path} <span style="color:#8a8f98">${branch}</span></div>
		<div class="meta">${meta}</div>${usage ? `<div class="usage">${usage}</div>` : ""}
		<div class="actions"><span class="go">${btn}</span><span class="chip">sessions ⌄</span><span class="chip">open in <b>app</b> ▾</span><span class="chip">Stop</span></div></div>`;
	const stacks = `
		${card({ name: "acme-api", path: "…/dev/acme-api", branch: "· main", dot: "live", meta: '<span class="live">2 live</span><span>up 2h</span><span>default account</span>', usage: "1.2M today · 8.4M this week", btn: "Open" })}
		${card({ name: "web", path: "…/dev/web", branch: "· feat/search*", dot: "ready", meta: '<span class="live">online · no session</span><span>up 2h</span><span>work account</span>', btn: "Start" })}`;

	const app = (tab, extra = "") => `<div class="app">
		<div class="brand"><div class="logo">🐕</div><div><h1>corgi</h1><small>andrii-mbp · corgi 2.9.1 · daemon up</small></div><span class="chip" style="margin-left:auto">↻</span></div>
		${tabs(tab.replace("→", ""))}
		<div class="pane">${tab.endsWith("inbox") ? inbox(extra === "moved" ? "moved" : "", extra === "aim") : tab.endsWith("sessions") ? sessions : stacks}</div>
		</div>`;

	const withTap = (tab, extra) => app(tab, extra);
	const phone = (screen, over = "") => `<div class="phone"><div class="notch"></div><div class="screen">${screen}${over}</div></div>`;

	const extraCss = `<style>
	  .app{background:#08090a}
	  .brand small{color:#8a8f98}
	  .tabs{display:flex;gap:2px;padding:6px 0 8px;border-bottom:1px solid #212327;margin-bottom:8px}
	  .tab{font-size:11.5px;color:#8a8f98;padding:4px 8px;border-radius:6px;display:flex;align-items:center;gap:5px}
	  .tab.sel{color:#eceef1;background:#16181b}
	  .tab.tap{box-shadow:0 0 0 3px rgba(94,106,210,.45)}
	  .tab i{font-style:normal;font-size:9px;color:#5c6169;background:#1e2024;border-radius:9px;padding:1px 5px}
	  .tab.sel i{color:#eceef1}
	  .sum{color:#8a8f98;font-size:10.5px;margin:0 0 6px 2px}.sum.hot{color:#f85149;font-weight:600}
	  .grp{color:#5c6169;font-size:9px;text-transform:uppercase;letter-spacing:.06em;margin:9px 0 4px 2px}
	  .ev{background:#101113;border:1px solid #212327;border-radius:9px;padding:8px 9px;margin:5px 0}
	  .ehead{display:flex;align-items:center;gap:6px}
	  .box{width:11px;height:11px;border:1.5px solid #3a3f47;border-radius:3px;display:inline-block;flex:none}
	  .eref{font-weight:600;font-size:11.5px}
	  .ekind{font-size:8.5px;text-transform:uppercase;letter-spacing:.05em;color:#5c6169}
	  .etitle{font-size:10.5px;color:#8a8f98;margin:3px 0 6px;line-height:1.35}
	  .col{color:#5e6ad2;font-weight:600}
	  .erow{display:flex;flex-wrap:wrap;gap:4px}
	  .eb{font-size:9.5px;font-weight:500;color:#8a8f98;border:1px solid #212327;border-radius:6px;padding:4px 8px}
	  .eb.pri{background:#5e6ad2;border-color:#5e6ad2;color:#fff}
	  .eb.tapme{box-shadow:0 0 0 3px rgba(94,106,210,.4)}
	  .ev.done .etitle{margin-bottom:0}
	  .pill{font-size:8.5px;color:#3fb950;border:1px solid #1c3a24;background:#0f1f14;border-radius:10px;padding:1px 6px}
	  .scrim{position:absolute;inset:0;background:rgba(0,0,0,.62);display:flex;align-items:flex-end}
	  .sheet{width:100%;background:#101113;border-top:1px solid #212327;border-radius:14px 14px 0 0;padding:6px 0 12px}
	  .grab{width:34px;height:4px;border-radius:3px;background:#2a2d33;margin:5px auto 8px}
	  .sh3{font-size:9px;letter-spacing:.07em;text-transform:uppercase;color:#5c6169;margin:4px 14px 4px;font-weight:600}
	  .opt{padding:9px 14px;font-size:11.5px;border-bottom:1px solid #1a1c1f}
	  .opt:last-child{border-bottom:0}
	  .opt.tap{background:#16181b;color:#5e6ad2;font-weight:600}
	  .sess{display:flex;align-items:center;gap:7px;background:#101113;border:1px solid #212327;border-radius:8px;padding:6px 8px;margin:4px 0;font-size:11px}
	  .sdot{width:7px;height:7px;border-radius:50%;background:#3fb950;flex:none;display:inline-block}
	  .sdot.needs{background:#f85149}.sdot.working{background:#d29922}
	  .slabel{font-weight:600;flex:1;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
	  .sdetail{color:#8a8f98;font-size:9.5px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:46%}
	  .sbadge{font-size:8px;font-weight:700;color:#8a8f98;border:1px solid #212327;border-radius:4px;padding:1px 4px;text-transform:uppercase}
	  .acct{display:flex;gap:10px;font-size:9.5px;color:#8a8f98;margin:6px 0 0 2px}.acct b{color:#eceef1}
	  .acct .bar{display:inline-block;width:38px;height:5px;border-radius:3px;background:#1e2024;vertical-align:middle;margin:0 4px;overflow:hidden}
	  .acct .bar i{display:block;height:100%;background:#3fb950}.acct .bar.hot i{background:#f85149}
	  .ws{background:#101113;border:1px solid #212327}
	  .usage{font-size:9.5px;color:#5c6169;margin:3px 0 0 17px}
	  .go{background:#5e6ad2;color:#fff;font-weight:600}
	  .toast{position:absolute;left:14px;right:14px;bottom:18px;background:#16181b;border:1px solid #212327;border-radius:9px;padding:8px 11px;font-size:10.5px;color:#eceef1;z-index:5}
	</style>`;

	// The story: the inbox, a ticket moved from its row, then the other two tabs.
	const shots = [
		["inbox", "", ""],
		["inbox", "", "aim"],                               // the finger is on Move…
		["inbox", sheet(""), ""],
		["inbox", sheet("In Progress"), ""],
		["inbox", "", "moved"],
		["→sessions", "", ""],
		["sessions", "", ""],
		["→stacks", "", ""],
		["stacks", "", ""],
		["inbox", "", "moved"],
	];
	const frames = shots.map(([tab, over, extra], i) => {
		let screen = withTap(tab, extra);
		if (i === 1) screen = screen.replace('class="eb tapme"', 'class="eb tapme"'); // the ring is already on Move…
		const toast = extra === "moved" && !over ? `<div class="toast">ABC-123 → In Progress</div>` : "";
		return page(extraCss + term("corgi agent up", up, { rows: up.length, extra: phone(screen, over + toast) }), 1290);
	});
	scene("dashboard", frames);
}

// ---- Telegram: the same board and answers from a chat -------------------------------
{
	let clock = 0;
	const msg = (who, text, quote) => `<div class="m ${who}">${quote ? `<div class="q">${esc(quote)}</div>` : ""}${esc(text).replace(/\n/g, "<br>")}<span class="t">13:${String(4 + clock++).padStart(2, "0")}</span></div>`;
	const flow = [
		["bot", "corgi agent · acme-api\nBash go test\nhttps://blue-fox-42.trycloudflare.com/app"],
		["me", "/allow acme-api"],
		["bot", "allow sent to acme-api"],
		["me", "/sessions"],
		["bot", "● corgi — WORKING · Edit registry.go · ctx 43%\n● acme-api — WORKING · Bash go test · ctx 72%\n✓ web — DONE · ctx 23%\nreply to a notification, or /send <session> <text> · /allow /deny <session>"],
		["me", "run the tests again and fix what fails", "corgi agent · acme-api"],
		["bot", "typed into acme-api"],
		["me", "/usage"],
		["bot", "default: 5h 55% (resets 5:10pm) · week 10% · 12%/h, lasts until the reset\nwork: 5h 100% (resets 1:10pm) · week 64% · limit reached, lifts 1:10pm\nwaited on you 3× today, longest 9m"],
		["bot", "corgi agent · acme-api\nlimit lifted — back to work"],
	];
	const css2 = `<style>
	  .tg{position:absolute;inset:0;background:#0e1621;color:#fff;font-family:-apple-system,system-ui,sans-serif;display:flex;flex-direction:column}
	  .tg .hdr{height:84px;background:#17212b;padding:46px 14px 0;display:flex;align-items:center;gap:10px;font-size:14px;font-weight:600}
	  .tg .hdr .av{width:30px;height:30px;border-radius:50%;background:linear-gradient(135deg,#f59e0b,#d97706);display:flex;align-items:center;justify-content:center;font-size:16px}
	  .tg .hdr small{display:block;color:#8ba0b8;font-size:10px;font-weight:400}
	  .tg .list{flex:1;padding:10px 10px 0;display:flex;flex-direction:column;gap:6px;justify-content:flex-end;overflow:hidden}
	  .m{max-width:86%;padding:7px 10px 7px;border-radius:12px;font-size:12px;line-height:1.35;position:relative;white-space:pre-wrap;word-break:break-word}
	  .m.bot{background:#182533;align-self:flex-start;border-bottom-left-radius:3px}.m.me{background:#2b5278;align-self:flex-end;border-bottom-right-radius:3px}
	  .m .t{display:block;text-align:right;font-size:9px;color:#8ba0b8;margin-top:2px}
	  .m .q{border-left:2px solid #5eb5f7;padding-left:6px;color:#5eb5f7;font-size:10.5px;margin-bottom:4px}
	  .tg .input{height:48px;background:#17212b;display:flex;align-items:center;padding:0 12px;gap:10px;color:#6c7883;font-size:12px}
	  .tg .input span{flex:1;background:#242f3d;border-radius:16px;padding:7px 12px}
	</style>`;
	const phone = (n) => `<div class="phone"><div class="notch"></div><div class="screen"><div class="tg"><div class="hdr"><div class="av">🐶</div><div>corgi<small>bot</small></div></div><div class="list">${flow.slice(0, n).map((m) => msg(...m)).join("")}</div><div class="input"><span>Message</span><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#6c7883" stroke-width="2" stroke-linecap="round"><rect x="9" y="2" width="6" height="12" rx="3"/><path d="M5 10a7 7 0 0 0 14 0M12 17v5M8 22h8"/></svg></div></div></div></div>`;
	const cuts = [1, 3, 5, 7, 9, 10];
	scene("telegram", cuts.map((n) => page(css2 + phone(n), 380)));
}

// ---- tunnels ----------------------------------------------------------------------------
{
	const s = [
		"{g}${/} {B}corgi tunnel{/}",
		"🌐 Tunnels (cloudflared) — Ctrl+C to stop",
		"",
		"  api                            :7012   cloudflared/quick → starting...",
		"  web                            :5173   cloudflared/quick → starting...",
		"",
		"  {g}✓{/} api                          :7012  → https://muddy-otter-42.trycloudflare.com",
		"  {g}✓{/} web                          :5173  → https://calm-fox-17.trycloudflare.com",
		"",
		"{g}${/} {B}corgi open web{/}",
		"opening http://localhost:5173",
	];
	scene("tunnel", grow(s, [1, 5, 8, 11]).map((l, i) => page(term("corgi tunnel", l, { rows: s.length, cursor: i < 3 }))));
}

// ---- logs -----------------------------------------------------------------------------------
{
	const s = [
		"{g}${/} {B}corgi logs --service api{/}",
		"{c}📄 /Users/me/dev/stack/corgi_services/.logs/api/2026-09-09T12-03-11.ok.log (Ctrl-C to exit){/}",
		"",
		"listening on :7012",
		"GET /health 200 1ms",
		"POST /referrals 201 14ms",
		"",
		"{y}— end of log —{/}",
		"",
		"{g}${/} {B}corgi logs --all{/}",
		"{c}[api]{/} listening on :7012",
		"{c}[web]{/} ➜  Local: http://localhost:5173/",
		"{c}[api]{/} GET /health 200 1ms",
		"{c}[web]{/} hmr update /src/checkout/Referral.tsx",
	];
	scene("logs", grow(s, [1, 2, 6, 8, 10, 14]).map((l, i) => page(term("corgi logs", l, { rows: s.length, cursor: i < 5 }))));
}


// ---- watch: the tracker and your PRs, handled while you are away --------------------
{
	const s = [
		"{g}${/} {B}corgi agent watch enable --prs --ci --action fix --auto-for reviews,comments,ci{/}",
		"watching acme-stack — assigned to me · issue comments · PR reviews and comments → fix",
		"restart the daemon to pick it up: corgi agent restart",
		"",
		"{g}${/} {B}corgi agent watch{/}",
		"Tokens",
		"  machine-wide  linear 3f9a12c0 · jira none · github 8be1d4f7 · gitlab none · webhook secret a7c2e910",
		"",
		"Watched",
		"  acme-stack           fix      every 3m0s linear, github",
		"                       auto for pr.review, issue.comment, pr.comment, ci.failed · caps 3/h 10/day · quiet 23:00-07:00 · fixes today: 0",
		"",
		"Last polls",
		"  acme-stack/github            2026-09-09T13:04:02Z",
		"  acme-stack/linear            2026-09-09T13:04:02Z",
		"",
		"{d}# 13:07 — a bug lands in Linear, assigned to you{/}",
		"{y}🔔 corgi agent · acme-stack{/}  new issue ABC-7 — Login loops after password reset",
		"{d}# 13:19 — a headless claude ran /corgi:stories ABC-7, then reviewed its own diff{/}",
		"{g}🔔 corgi agent · acme-stack{/}  fixed ABC-7 — https://github.com/acme/api/pull/412",
		"{d}# 13:31 — a reviewer comments on your PR{/}",
		"{y}🔔 corgi agent · acme-stack{/}  max commented on acme/api#412: please cover the empty-path case",
		"{g}🔔 corgi agent · acme-stack{/}  fixed acme/api#412 — https://github.com/acme/api/pull/412",
		"{d}# 13:44 — CI goes red; the one kind that brings its own test for done{/}",
		"{y}🔔 corgi agent · acme-stack{/}  red build in acme/api — e2e / checkout failed",
		"{d}# 13:52 — a colleague wants YOUR review: reported, never worked on{/}",
		"{y}🔔 corgi agent · acme-stack{/}  sam wants your review on acme/web!41 — Retry the upload on a 502",
	];
	scene("watch", grow(s, [1, 3, 5, 15, 18, 20, 22, 24, 26, 28]).map((l, i) => page(term("corgi agent watch", l, { rows: s.length, cursor: i < 9 }))));
}

// ---- unattended: every kind that arrives, and every reason one is skipped ----
{
	const s = [
		"{g}${/} {B}corgi agent watch enable --action fix --auto-for reviews,comments,ci \\{/}",
		"    {B}--prs --ci --reviews --pickup \"In Progress\" --lease --quiet 23:00-07:00{/}",
		"watching acme-stack — assigned to me · issue comments · PR reviews and comments → fix",
		"restart the daemon to pick it up: corgi agent restart",
		"",
		"{d}# 09:04 — a reviewer leaves feedback on my PR. Worked on: it is scoped.{/}",
		"{y}🔔{/} max commented on acme/api#412: please cover the empty-path case",
		"{c}   → claimed acme/api#412 · reviewed its own diff · pushed{/}",
		"{g}🔔{/} fixed acme/api#412 — https://github.com/acme/api/pull/412",
		"",
		"{d}# 09:31 — CI goes red. The one kind that brings its own test for done.{/}",
		"{y}🔔{/} red build in acme/api — e2e / checkout failed",
		"{g}🔔{/} fixed acme/api — https://github.com/acme/api/pull/418",
		"",
		"{d}# 10:02 — a ticket assigned to me, in a state the rules allow{/}",
		"{y}🔔{/} new issue ABC-7 — Login loops after password reset",
		"{c}   → claimed · Ready ▸ In Progress · worked · draft PR · ▸ In Review{/}",
		"{g}🔔{/} fixed ABC-7 — https://github.com/acme/api/pull/415",
		"",
		"{d}# 10:09 — a ticket in Backlog. The state filter stops it.{/}",
		"{d}   → no match — state \"Backlog\" is not one of Ready, In Progress{/}",
		"",
		"{d}# 10:15 — a colleague wants MY review. Never worked on unattended.{/}",
		"{y}🔔{/} sam wants your review on acme/web!41 — Retry the upload on a 502",
		"",
		"{d}# 10:40 — a comment on a ticket closed last week{/}",
		"{d}   → skipped: it is done — the comment is not work{/}",
		"{d}# 10:41 — three more comments on acme/api#412 in one poll{/}",
		"{d}   → one notification, not three{/}",
		"{d}# 10:52 — ABC-9, already closed as a duplicate{/}",
		"{d}   → skipped: it is a duplicate — nobody is going to act on it{/}",
		"",
		"{d}# 11:20 — the desktop got to this one first{/}",
		"{d}   → ABC-8 is already claimed by andrii-desktop{/}",
		"{d}# 11:48 — four fixes this hour{/}",
		"{y}🔔{/} new issue ABC-11 — Retry the webhook (3/h cap)",
		"{d}# 12:05 — a run here costs about 12% and 88% of the window is used{/}",
		"{d}   → deferred; corgi agent watch run picks it up{/}",
		"",
		"{g}${/} {B}corgi agent while-away{/}",
		"Since Wed 9 Sep 23:00",
		"",
		"corgi opened",
		"  acme/api#412                 opened 1 PR (acme-stack)",
		"  acme/api                     opened 1 PR (acme-stack)",
		"  ABC-7                        opened 1 PR (acme-stack)",
		"arrived",
		"  acme/web!41                  review.requested (acme-stack)",
		"",
		"waiting for a free slot",
		"  ABC-11",
		"  a cap or quiet hours held these; corgi agent watch run picks them up",
	];
	scene("autofix", grow(s, [2, 4, 9, 13, 18, 21, 24, 31, 38, 44, 51]).map((l, i) =>
		page(term("corgi agent watch — unattended", l, { rows: s.length, cursor: i < 10 }))));
}

// ---- the three things a teammate actually sees it do, in order ----
{
	const s = [
		"{g}${/} {B}corgi agent watch enable --action fix \\{/}",
		"    {B}--auto-for requests,reviews,comments,tickets \\{/}",
		"    {B}--prs --reviews --pickup \"In Progress\" --review-status \"In Review\"{/}",
		"watching acme-stack — assigned to me · issue comments · PR reviews and comments → fix",
		"",
		"{m}━━ 1 ━━  someone asks for my review{/}",
		"{y}🔔{/} sam wants your review on acme/api!318 — Reconcile the stale user rows on connect",
		"{d}   corgi reads the diff. Their branch, so it comments — it never pushes.{/}",
		"{g}🔔{/} reviewed acme/api!318 — 3 comments, 1 blocking",
		"{d}   « the retry loop swallows a 429; it will look like success »{/}",
		"",
		"{m}━━ 2 ━━  someone comments on mine{/}",
		"{y}🔔{/} max commented on acme/api!294: please cover the empty-path case",
		"{d}   my branch, so it fixes it: applies, replies in the thread, pushes.{/}",
		"{g}🔔{/} fixed acme/api!294 — https://gitlab.com/acme/api/-/merge_requests/294",
		"",
		"{m}━━ 3 ━━  a ticket lands in READY TO DEV, assigned to me{/}",
		"{y}🔔{/} new issue ABC-142 — Send the image manifest with the bootstrap",
		"{c}   claimed on the ticket   {/}{d}so the desktop leaves it alone{/}",
		"{c}   READY TO DEV ▸ In Progress{/}",
		"{d}   …works it in the checkout, then reviews its own diff{/}",
		"{c}   draft MR opened      {/}{d}https://gitlab.com/acme/api/-/merge_requests/301{/}",
		"{c}   In Progress ▸ In Review{/}{d}   and the link posted on ABC-142{/}",
		"{g}🔔{/} fixed ABC-142 — https://gitlab.com/acme/api/-/merge_requests/301",
		"",
		"{d}# in the morning{/}",
		"{g}${/} {B}corgi agent while-away{/}",
		"Since Wed 9 Sep 23:00",
		"",
		"corgi opened",
		"  acme/api!294                 opened 1 MR (acme-stack)",
		"  ABC-142                      opened 1 MR (acme-stack)",
		"arrived",
		"  acme/api!318                 review.requested (acme-stack)",
	];
	// Slow, even beats: this one is shown to people who have not seen corgi.
	scene("story", grow(s, [3, 4, 6, 8, 10, 11, 13, 15, 16, 18, 19, 20, 21, 22, 23, 24, 26, 28, 34])
		.map((l, i) => page(term("corgi — while you were in a meeting", l, { rows: s.length, cursor: i < 18 }))));
}

// ---- the two commands that make leaving it on a decision you can reverse ----
{
	const s = [
		"{d}# before turning it on: the week it would have had{/}",
		"{g}${/} {B}corgi agent watch replay --since 168h{/}",
		"Since Thu 3 Sep 09:12, with the rules as they are now",
		"",
		"acme-stack — 4 worked on · 3 reported · 6 ignored",
		"  worked on  pr.review      acme/api#412",
		"  worked on  issue.comment  ABC-7",
		"  worked on  ci.failed      acme/api",
		"  worked on  pr.comment     acme/web!41",
		"  reported   issue.new      ABC-9",
		"  reported   issue.new      ABC-11",
		"  reported   review.requested acme/web!41",
		"  ignored    6 — run with --json to see each reason",
		"",
		"{d}# in the morning, one card instead of nine notifications{/}",
		"{g}${/} {B}corgi agent while-away{/}",
		"Since Wed 9 Sep 18:22",
		"",
		"corgi opened",
		"  ABC-7                        opened 1 PR (acme-stack)",
		"                               https://github.com/acme/api/pull/412",
		"corgi could not",
		"  acme/api                     the last runs failed on no-credential (acme-stack)",
		"arrived",
		"  acme/web!41                  review.requested (acme-stack)",
		"",
		"{d}# and put a run back the way it was{/}",
		"{g}${/} {B}corgi agent watch undo ABC-7 --dry-run{/}",
		"ABC-7 (acme-stack)",
		"  would close https://github.com/acme/api/pull/412",
		"  would move back to Ready",
		"  the branch is left alone — the work is on it",
		"",
		"{g}${/} {B}corgi agent watch undo ABC-7{/}",
		"ABC-7 (acme-stack)",
		"  closed   https://github.com/acme/api/pull/412",
		"  moved    back to Ready",
		"  the branch is left alone — the work is on it",
	];
	scene("replay", grow(s, [2, 5, 9, 13, 15, 20, 24, 29, 38]).map((l, i) => page(term("corgi agent watch replay · while-away · undo", l, { rows: s.length, cursor: i < 8 }))));
}


writeFileSync(`${out}/scenes.json`, JSON.stringify(scenes));
console.log("wrote", Object.entries(scenes).map(([k, v]) => `${k}:${v}`).join(" "));
