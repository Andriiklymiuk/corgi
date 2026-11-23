import { test } from 'node:test';
import assert from 'node:assert/strict';

import { connect, CorgiError, pair } from '../index.js';

function fakeFetch(answers) {
  const calls = [];
  const fn = async (url, init = {}) => {
    const u = new URL(url);
    const body = init.body ? JSON.parse(init.body) : undefined;
    calls.push({ path: u.pathname, query: u.searchParams, method: init.method ?? 'GET', body, auth: init.headers?.Authorization });
    const next = answers.shift() ?? { status: 200, json: {} };
    return new Response(JSON.stringify(next.json), { status: next.status ?? 200 });
  };
  fn.calls = calls;
  return fn;
}

test('pair sends the code and the device name, and keeps the token', async () => {
  const fetch = fakeFetch([{ json: { token: 't1', device: 'ci', daemon: 'mac', publicUrl: 'https://x.example' } }]);
  const p = await pair({ url: 'https://x.example/app', code: ' 123456 ', device: 'ci', fetch });
  assert.equal(p.token, 't1');
  assert.equal(p.url, 'https://x.example');
  assert.equal(fetch.calls[0].path, '/pair');
  assert.deepEqual(fetch.calls[0].body, { code: '123456', device: 'ci' });
  await assert.rejects(pair({ url: 'https://x', code: 'bad', device: 'ci', fetch: fakeFetch([{ status: 403, json: { error: 'wrong code' } }]) }), /wrong code/);
  await assert.rejects(pair({ url: 'https://x' }), TypeError);
});

test('next asks for work and returns null when nothing is ready', async () => {
  const fetch = fakeFetch([{ json: { task: { id: 3, title: 'fix' }, ref: 'TASK-3', leaseUntil: '2026-11-23T13:00:00Z' } }, { json: { task: null } }]);
  const corgi = connect({ url: 'https://x.example/mcp', token: 'tok', fetch });
  const task = await corgi.tasks.next({ tags: ['bug'], lease: '30m' });
  assert.equal(task.ref, 'TASK-3');
  assert.equal(task.leaseUntil, '2026-11-23T13:00:00Z');
  assert.equal(fetch.calls[0].path, '/launch/task/next');
  assert.equal(fetch.calls[0].auth, 'Bearer tok');
  assert.deepEqual(fetch.calls[0].body, { tags: ['bug'], lease: '30m', wait: '0s' });
  assert.equal(await corgi.tasks.next(), null);
});

test('a long wait is asked for in pieces the server holds', async () => {
  const fetch = fakeFetch([{ json: { task: null } }, { json: { task: { id: 1 }, ref: 'TASK-1' } }]);
  const corgi = connect({ url: 'https://x', token: 't', fetch });
  const task = await corgi.tasks.next({ wait: '10m' });
  assert.equal(task.ref, 'TASK-1');
  assert.equal(fetch.calls[0].body.wait, '25s');
});

test('renew, finish and add send what the routes expect', async () => {
  const fetch = fakeFetch([{ json: { ref: 'TASK-3' } }, { json: { ref: 'TASK-3', state: 'Done' } }, { json: { ref: 'TASK-4' } }]);
  const corgi = connect({ url: 'https://x', token: 't', fetch });
  await corgi.tasks.renew('TASK-3', { lease: '2h' });
  const done = await corgi.tasks.finish('TASK-3', 'fixed in #8');
  await corgi.tasks.add({ title: 'next one', workspace: 'api' });
  assert.equal(done.state, 'Done');
  assert.deepEqual(fetch.calls.map((c) => [c.path, c.body]), [
    ['/launch/task/renew', { task: 'TASK-3', lease: '2h' }],
    ['/launch/task/finish', { task: 'TASK-3', result: 'fixed in #8' }],
    ['/launch/task', { title: 'next one', workspace: 'api' }],
  ]);
});

test('keep renews until it is stopped', async () => {
  const fetch = fakeFetch([{ json: {} }, { json: {} }, { json: {} }]);
  const corgi = connect({ url: 'https://x', token: 't', fetch });
  const stop = await corgi.tasks.keep('TASK-3', { lease: '1h', every: 0.02 });
  await new Promise((r) => setTimeout(r, 70));
  await stop();
  const renewed = fetch.calls.length;
  assert.ok(renewed >= 2, `renewed ${renewed} times`);
  await new Promise((r) => setTimeout(r, 50));
  assert.equal(fetch.calls.length, renewed);
});

test('rooms post, read with labels, and list', async () => {
  const fetch = fakeFetch([{ json: { message: { id: 'm1' } } }, { json: { messages: [{ id: 'm2' }] } }, { json: { rooms: [{ name: 'security' }] } }]);
  const corgi = connect({ url: 'https://x', token: 't', fetch });
  const m = await corgi.rooms.post('security', { text: 'found one', shape: 'finding@1', data: { repo: 'acme/api' }, labels: { kind: 'bug' } });
  const read = await corgi.rooms.read('security', { after: 'm1', labels: { kind: 'bug' }, wait: '2m' });
  const list = await corgi.rooms.list();
  assert.equal(m.id, 'm1');
  assert.equal(read[0].id, 'm2');
  assert.equal(list[0].name, 'security');
  assert.deepEqual(fetch.calls[0].body, { room: 'security', text: 'found one', shape: 'finding@1', data: { repo: 'acme/api' }, labels: { kind: 'bug' } });
  assert.equal(fetch.calls[1].query.get('after'), 'm1');
  assert.deepEqual(fetch.calls[1].query.getAll('label'), ['kind=bug']);
  assert.equal(fetch.calls[1].query.get('wait'), '25s');
});

test('follow yields each message once and goes on from the last', async () => {
  const fetch = fakeFetch([{ json: { messages: [{ id: 'm1' }, { id: 'm2' }] } }, { json: { messages: [] } }, { json: { messages: [{ id: 'm3' }] } }]);
  const corgi = connect({ url: 'https://x', token: 't', fetch });
  const ctrl = new AbortController();
  const seen = [];
  for await (const m of corgi.rooms.follow('security', { signal: ctrl.signal })) {
    seen.push(m.id);
    if (seen.length === 3) ctrl.abort();
  }
  assert.deepEqual(seen, ['m1', 'm2', 'm3']);
  assert.equal(fetch.calls[1].query.get('after'), 'm2');
  assert.equal(fetch.calls[2].query.get('after'), 'm2');
});

test('a refusal comes back as a CorgiError with the status and the reason', async () => {
  const corgi = connect({ url: 'https://x', token: 't', fetch: fakeFetch([{ status: 403, json: { error: 'g2 gives ci nothing in security/' } }]) });
  await assert.rejects(corgi.rooms.read('security'), (err) => err instanceof CorgiError && err.status === 403 && /g2/.test(err.message) && err.path === '/launch/rooms');
  assert.throws(() => connect({ url: 'https://x' }), TypeError);
});
