import { spawn } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

const args = process.argv.slice(2);
const flag = (name, fallback) => { const i = args.indexOf(`--${name}`); return i >= 0 ? args[i + 1] : fallback; };
const fps = Number(flag("fps", 30));
const stills = flag("stills", "");
const output = resolve(flag("out", "docs/media/corgi-film.mp4"));
const poster = resolve(flag("poster", "docs/media/corgi-film.png"));
const posterAt = Number(flag("poster-at", 153));
const audio = resolve(flag("audio", "docs/film/music.mp3"));
const chrome = process.env.CHROME ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
const port = 9400 + Math.floor(Math.random() * 400);
const profile = mkdtempSync(join(tmpdir(), "corgi-film-"));

const browser = spawn(chrome, ["--headless=new", `--remote-debugging-port=${port}`, `--user-data-dir=${profile}`, "--hide-scrollbars", "--no-first-run", "--window-size=1280,720", "about:blank"], { stdio: "ignore" });
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function pageSocket() {
	for (let attempt = 0; attempt < 60; attempt++) {
		try {
			const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
			const page = targets.find((t) => t.type === "page");
			if (page) return new WebSocket(page.webSocketDebuggerUrl);
		} catch {}
		await sleep(250);
	}
	throw new Error("Chrome did not open a debugging port");
}

const socket = await pageSocket();
await new Promise((r) => socket.addEventListener("open", r, { once: true }));
let nextId = 0;
const pending = new Map();
socket.addEventListener("message", (event) => {
	const message = JSON.parse(event.data);
	if (message.id && pending.has(message.id)) { pending.get(message.id)(message.result ?? message); pending.delete(message.id); }
});
const send = (method, params = {}) => new Promise((done) => { const id = ++nextId; pending.set(id, done); socket.send(JSON.stringify({ id, method, params })); });
const evaluate = async (expression) => (await send("Runtime.evaluate", { expression, returnByValue: true, awaitPromise: true })).result?.value;
const shot = async (format) => Buffer.from((await send("Page.captureScreenshot", { format, quality: 93 })).data, "base64");

await send("Emulation.setDeviceMetricsOverride", { width: 1280, height: 720, deviceScaleFactor: 1.5, mobile: false });
await send("Page.navigate", { url: `${pathToFileURL(resolve("docs/film/index.html"))}?capture=1` });
for (let attempt = 0; attempt < 80 && !(await evaluate("window.filmReady === true")); attempt++) await sleep(250);
const duration = await evaluate("window.filmDuration");

try {
	if (stills) {
		mkdirSync(stills, { recursive: true });
		for (const t of flag("at", "").split(",").map(Number)) {
			await evaluate(`seek(${t})`);
			writeFileSync(join(stills, `t${String(t).replace(".", "_")}.jpg`), await shot("jpeg"));
		}
	} else {
		const inputs = ["-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", String(fps), "-c:v", "mjpeg", "-i", "-"];
		if (existsSync(audio)) inputs.push("-i", audio, "-c:a", "aac", "-b:a", "160k", "-shortest");
		const ffmpeg = spawn("ffmpeg", [...inputs, "-c:v", "libx264", "-preset", "slow", "-crf", "28", "-pix_fmt", "yuv420p", "-movflags", "+faststart", output], { stdio: ["pipe", "inherit", "inherit"] });
		const total = Math.floor(duration * fps);
		for (let frame = 0; frame < total; frame++) {
			await evaluate(`seek(${frame / fps})`);
			const jpeg = await shot("jpeg");
			if (!ffmpeg.stdin.write(jpeg)) await new Promise((r) => ffmpeg.stdin.once("drain", r));
			if (frame % 300 === 0) console.log(`frame ${frame}/${total}`);
		}
		ffmpeg.stdin.end();
		await new Promise((r) => ffmpeg.on("close", r));
		await evaluate(`seek(${posterAt})`);
		writeFileSync(poster, await shot("png"));
		console.log(`wrote ${output}`);
	}
} finally {
	socket.close();
	browser.kill();
	await sleep(300);
	rmSync(profile, { recursive: true, force: true });
}
