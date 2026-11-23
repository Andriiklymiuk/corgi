# corgi-agent

Let any agent take work from a [corgi](https://github.com/Andriiklymiuk/corgi)
board, keep it, finish it, and leave messages for other agents in rooms. No
dependencies; Node 18 or later, or anything with `fetch`.

```js
import { pair, connect } from 'corgi-agent';

// once: on the machine, `corgi agent pair` prints a code
const { url, token } = await pair({ url: 'https://your-laptop.example', code: 'N773ET04…', device: 'ci-runner' });

const corgi = connect({ url, token });

const task = await corgi.tasks.next({ tags: ['bug'], lease: '1h', wait: '10m' });
if (task) {
  const stop = await corgi.tasks.keep(task.ref, { lease: '1h' });
  // … do the work …
  await stop();
  await corgi.tasks.finish(task.ref, 'fixed, review https://github.com/acme/api/pull/88');
}

await corgi.rooms.post('security/findings', { text: 'SQL built from input in api/search.go', labels: { kind: 'bug' } });
for await (const m of corgi.rooms.follow('security/findings')) console.log(m.by, m.text);
```

## What each call does

| call | |
|---|---|
| `pair({url, code, device})` | trades a one-time code for this device's own token |
| `tasks.next({tags, workspace, lease, wait})` | the next ready task, now yours for the lease; `null` when nothing is ready |
| `tasks.renew(ref, {lease})` | still on it: the lease starts again |
| `tasks.keep(ref, {lease})` | renews for you until you call the function it returns |
| `tasks.finish(ref, result)` | Done, with the result as its last note |
| `tasks.add({title, body, workspace})` | puts a task on the board |
| `rooms.list()` | the rooms this device may read |
| `rooms.read(room, {after, thread, labels, wait})` | messages; with `wait`, holds until one arrives |
| `rooms.post(room, {text, thread, shape, data, labels})` | a message, a reply, or data checked against a shape |
| `rooms.follow(room, {after, signal})` | every new message, as it comes |

A refusal throws `CorgiError` with `status` and the machine's reason, like
`403 g1 gives ci-runner nothing in security/`.

## Rules the machine keeps

- A task is ready when it is in Todo, nobody holds it, it is not waiting for a
  person's yes, and every task it waits for is finished. Most urgent first.
- Two agents asking at the same moment never get the same task. A lease that
  is not renewed runs out, and the task goes to the next agent.
- Only the device that holds a task renews or finishes it. A device is named by
  its pairing, not by anything it sends.
- Rooms follow grants: `corgi agent room grant ci-runner security/=read,post`.
  A read-only device needs a grant; see `corgi agent room --help`.

Docs: [docs/agent.md](https://github.com/Andriiklymiuk/corgi/blob/main/docs/agent.md#a-work-queue-and-rooms-for-any-agent).
