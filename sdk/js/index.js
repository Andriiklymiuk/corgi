const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

export class CorgiError extends Error {
  constructor(status, message, path) {
    super(message);
    this.name = 'CorgiError';
    this.status = status;
    this.path = path;
  }
}

const trimSlash = (url) => String(url).replace(/\/+$/, '').replace(/\/(app|mcp)$/, '');

export async function pair({ url, code, device, fetch: fetchImpl = globalThis.fetch }) {
  if (!url || !code || !device) throw new TypeError('pair needs url, code and device');
  const base = trimSlash(url);
  const res = await fetchImpl(`${base}/pair`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ code: String(code).trim(), device }),
  });
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new CorgiError(res.status, body.error || `pairing failed: HTTP ${res.status}`, '/pair');
  return { url: body.publicUrl || base, token: body.token, device: body.device, machine: body.daemon, version: body.version };
}

export function connect({ url, token, fetch: fetchImpl = globalThis.fetch }) {
  if (!url || !token) throw new TypeError('connect needs the url and the token pair() returned');
  if (typeof fetchImpl !== 'function') throw new TypeError('no fetch: use Node 18 or later, or pass fetch');
  const base = trimSlash(url);

  async function call(path, { method = 'GET', body, query } = {}) {
    const qs = query ? new URLSearchParams(Object.entries(query).filter(([, v]) => v !== undefined && v !== null && v !== '').flatMap(([k, v]) => (Array.isArray(v) ? v.map((x) => [k, String(x)]) : [[k, String(v)]]))).toString() : '';
    const res = await fetchImpl(`${base}${path}${qs ? `?${qs}` : ''}`, {
      method,
      headers: { Authorization: `Bearer ${token}`, ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    let data = {};
    try {
      data = text ? JSON.parse(text) : {};
    } catch {
      data = { error: text.slice(0, 200) };
    }
    if (!res.ok) throw new CorgiError(res.status, data.error || `HTTP ${res.status}`, path);
    return data;
  }

  const labelQuery = (labels) => (labels ? Object.entries(labels).map(([k, v]) => `${k}=${v}`) : undefined);
  const capWait = (seconds) => `${Math.max(0, Math.min(25, seconds))}s`;
  const parseDuration = (d) => {
    if (typeof d === 'number') return d;
    const m = /^(\d+)(ms|s|m|h)$/.exec(String(d || '').trim());
    if (!m) return 0;
    return Number(m[1]) * { ms: 0.001, s: 1, m: 60, h: 3600 }[m[2]];
  };

  const tasks = {
    async next({ tags, workspace, lease, wait } = {}) {
      const until = Date.now() + parseDuration(wait) * 1000;
      for (;;) {
        const left = Math.max(0, (until - Date.now()) / 1000);
        const out = await call('/launch/task/next', { method: 'POST', body: { tags, workspace, lease, wait: capWait(left) } });
        if (out.task) return { ...out.task, ref: out.ref, leaseUntil: out.leaseUntil };
        if (Date.now() >= until) return null;
      }
    },
    renew(ref, { lease } = {}) {
      return call('/launch/task/renew', { method: 'POST', body: { task: ref, lease } });
    },
    finish(ref, result) {
      return call('/launch/task/finish', { method: 'POST', body: { task: ref, result } });
    },
    add({ title, body, workspace }) {
      return call('/launch/task', { method: 'POST', body: { title, body, workspace } });
    },
    async keep(ref, { lease = '1h', every } = {}) {
      const period = (every ? parseDuration(every) : Math.max(30, parseDuration(lease) / 2)) * 1000;
      let stopped = false;
      const loop = (async () => {
        while (!stopped) {
          await sleep(period);
          if (!stopped) await tasks.renew(ref, { lease });
        }
      })();
      return () => {
        stopped = true;
        return loop.catch(() => undefined);
      };
    },
  };

  const rooms = {
    async list() {
      return (await call('/launch/rooms')).rooms;
    },
    async read(room, { after, thread, labels, wait } = {}) {
      const out = await call('/launch/rooms', { query: { room, after, thread, label: labelQuery(labels), wait: wait ? capWait(parseDuration(wait)) : undefined } });
      return out.messages;
    },
    async post(room, { text, thread, shape, data, labels } = {}) {
      return (await call('/launch/rooms', { method: 'POST', body: { room, text, thread, shape, data, labels } })).message;
    },
    async *follow(room, { after, thread, labels, signal } = {}) {
      let last = after;
      while (!signal?.aborted) {
        const batch = await rooms.read(room, { after: last ?? '', thread, labels, wait: 25 });
        for (const m of batch) {
          last = m.id;
          yield m;
        }
        if (last === undefined) last = 'm0';
      }
    },
  };

  return { call, tasks, rooms };
}
