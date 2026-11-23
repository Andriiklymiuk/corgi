import { readFileSync } from "node:fs";

const iso = (minutes) => new Date(Date.now() - minutes * 60000).toISOString();
const ms = (minutes) => Date.now() - minutes * 60000;
const linear = ["Backlog", "Ready", "In Progress", "In Review", "Staging", "Done"];
const taskColumns = ["Todo", "Doing", "Review", "Done", "Canceled"];
const body = "The export writes rows in database order, so a person who sorted by date gets a file in another order. Read the table's sort from the URL and pass it to the export job. It is done when the first three rows of the file match the first three on the screen.";

const data = (moved) => {
	const login = { ref: "ABC-123", key: "l1", title: "Login redirect loops after SSO", url: "https://linear.app/acme/issue/ABC-123", workspace: "acme-api", kind: "issue.new" };
	const task = { ref: "TASK-7", key: "task:7", title: "Export keeps the sort order the table shows", workspace: "web", kind: "task", body, columns: taskColumns, state: "Todo" };
	const review = { ref: "acme/web#41", key: "h41", title: "Retry the upload on a 502", url: "https://github.com/acme/web/pull/41", workspace: "web" };
	const cards = [
		{ ...task, column: "Inbox", why: "waiting in the inbox" },
		moved
			? { ...login, column: "Running", why: "picked up, In Progress on the tracker", state: "In Progress", now: { doing: "reading the redirect", model: "opus" }, branch: "feature/ABC-123" }
			: { ...login, column: "Inbox", why: "waiting in the inbox", state: "Ready" },
		{ ref: "ABC-128", key: "l2", title: "Cache the workspace registry between polls", url: "https://linear.app/acme/issue/ABC-128", workspace: "acme-api", kind: "issue.new", column: "Ready", why: "handoff: input-required · 2 done · 1 remaining", state: "Ready" },
		{ ref: "ABC-131", key: "l3", title: "Cap the retries of the nightly import", url: "https://linear.app/acme/issue/ABC-131", workspace: "acme-api", kind: "issue.new", column: "Running", why: "a run started 6m ago", now: { doing: "go test ./...", model: "sonnet" }, branch: "corgi/abc-131", cost: { tokens: 310000, usd: 0.41, runs: 1, sessions: 0, models: ["sonnet"], agents: ["claude"] } },
		{ ref: "ABC-119", key: "l4", title: "Prices show the wrong currency sign", url: "https://linear.app/acme/issue/ABC-119", workspace: "web", kind: "issue.new", column: "Blocked", why: "no token for the pricing service", state: "Ready" },
		{ ...review, kind: "pr.review", column: "Review", why: "checks passing · review pending", fix: { prs: [review.url] }, cost: { tokens: 840000, usd: 0.84, runs: 2, sessions: 0, models: ["sonnet"], agents: ["claude"] } },
		{ ref: "ABC-101", key: "l9", title: "Empty cart", url: "https://linear.app/acme/issue/ABC-101", workspace: "web", kind: "issue.new", column: "Done", why: "merged" },
	];
	const boards = { "acme-api": { columns: linear }, web: { columns: linear } };
	const session = (id, label, status, more) => ({ id, label, display: label, status, ...more });
	const repo = (id, more) => ({ id, path: "/Users/sam/dev/" + id, running: true, state: "ready", branch: "main", live: 0, startedAt: ms(130), profiles: ["work"], ...more });
	return {
		"/launch/info": { host: "sams-mbp", version: "", daemon: true },
		"/launch/kanban": { columns: ["Inbox", "Ready", "Running", "Blocked", "Review", "Done"], cards, boards, features: [] },
		"/launch/events": {
			boards, events: [
				{ ...login, state: moved ? "In Progress" : "Ready", actionable: true },
				{ ...task, actionable: true },
				{ ...review, kind: "pr.review", pr: review.url },
			],
			fixes: [{ key: "f1", ref: "acme/web#38", startedAt: iso(50), prs: ["https://github.com/acme/web/pull/38"] }],
		},
		"/launch/board": {
			windows: [{ id: "w", folders: ["/dev/acme-api"] }],
			accounts: [{ profile: "default", limits: { fiveHour: { percent: 34 } } }, { profile: "work", limits: { fiveHour: { percent: 91 } } }],
			sessions: [
				session("1", "acme-api", "needs_input", { detail: "Bash go test", pending: { tool: "Bash" }, branch: "feature/ABC-123", summary: "Login redirect loops after SSO", profile: "work" }),
				session("2", "web", "working", { detail: "Edit table.tsx", branch: "feature/TASK-7", summary: "Export keeps the sort order", turnStartedAt: iso(7), url: "https://claude.ai/code/session_1" }),
				session("3", "mobile", "done", { branch: "main", summary: "Reviewed the upload retry" }),
			],
		},
		"/launch/workspaces": {
			workspaces: [
				repo("acme-api", { state: "live", live: 2, sessionUrl: "https://claude.ai/code/session_2", topSession: { name: "acme-api · feature/ABC-123", url: "https://claude.ai/code/session_2", startedAt: ms(40), where: "remote" }, usage: { today: { input: 1200000 }, week: { input: 8400000 } }, dirty: true, branch: "feature/ABC-123" }),
				repo("web", {}),
				repo("mobile", { running: false, state: "stopped" }),
			],
		},
		"/launch/ideas": { ideas: [] },
		"/launch/devices": { devices: [] },
		"/launch/history": { entries: [] },
	};
};

const launcherPage = () => {
	const source = readFileSync("cmd/mcp_launcher.go", "utf8");
	const open = "const launcherPageHTML = `";
	const from = source.indexOf(open) + open.length;
	return source.slice(from, source.indexOf("\n`\n", from));
};

const script = (value) => JSON.stringify(value).replace(/</g, "\\u003c");

export const launcherFrame = ({ tab, moved = false, press = "", css = "" }) => {
	const stub = `<style>${css}</style><script>
		const kept = new Map([["corgi_token", "x"], ["corgi.tab", ${script(tab)}]]);
		Object.defineProperty(window, "localStorage", { value: { getItem: (k) => kept.get(k) ?? null, setItem: (k, v) => kept.set(k, v), removeItem: (k) => kept.delete(k) } });
		const answers = ${script(data(moved))};
		window.fetch = async (path) => new Response(JSON.stringify(answers[String(path).split("?")[0]] ?? {}), { headers: { "Content-Type": "application/json" } });
		window.ResizeObserver ??= class { observe() {} };
	</script>`;
	return launcherPage()
		.replace("@media (prefers-color-scheme:dark)", "@media all")
		.replace("</style>", `</style>${stub}`) + (press ? `<script>setTimeout(() => { ${press} }, 500)</script>` : "");
};

export const pressButton = (pane, text) => `[...document.querySelectorAll('[data-pane=${pane}] .erow button')].find((b) => b.textContent === '${text}').click()`;
export const openText = (pane) => `document.querySelector('[data-pane=${pane}] .words').nextSibling.click()`;
export const say = (text) => `toast('${text}')`;
