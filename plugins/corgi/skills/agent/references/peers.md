# Two laptops on the same trackers

Two laptops that watch the same Linear project or GitHub repo would each start
a fix and each ring the phone. Paired as **peers** they agree: for every
tracker both watch, one leads - it fixes, runs bots and routines, and rings;
the other records the events and stays quiet. A leader that stops pulsing for
three minutes (asleep, shut) is replaced by the next one, and it takes the
lead back when it returns. Sessions and their permission prompts are still
per laptop - those are not shared work.

- The phone pairs with both → it introduces them (`POST /launch/peers/invite`
  on the one it knew, `POST /launch/peers/join` on the new one). Nothing to run.
- By hand: `corgi agent peers invite` on laptop A prints a URL and a
  ten-minute code; `corgi agent peers join <url> <code>` on B pairs both ways.
- A machine that has nothing yet: `corgi agent peers bootstrap user@host` from
  A installs corgi and claude there over ssh, clones A's workspaces under the
  same names with their watch, starts the daemon and pairs; `--lead` makes it
  the one that works unattended, `--with-tokens` carries the tracker tokens
  over the ssh session, `--dry-run` only prints the plan (the setup skill).
- No ssh: `corgi agent peers invite --setup [--with-secrets]` on A, then the
  printed `corgi agent peers join <url> <code> --import` on B; or a sealed file:
  `peers export --out me.corgi --with-tokens --with-env --with-claude` on A,
  `peers import --from me.corgi` on B (the setup skill).
- `corgi agent peers` - who is paired, awake, leading, and what each watches.
- `corgi agent peers lead` - this laptop leads every tracker it shares (the
  home machine that should do the unattended work); `lead off` returns to
  first-by-name.
- `corgi agent peers rm <name>` - forget one (run it on both).
- `corgi agent task send TASK-3 --to <name>` - move a task and its thread. A task
  waiting for a run also moves by itself once its machine is silent for 10 minutes.

What the pulse carries (2.29.1): each laptop's board (sessions, tickets,
branches, what waits), its unattended runs of the day (running, failed with
why, blocked), the tickets it ignored, how much of its five-hour window is
free, and whether it can work at all. So:

- `corgi agent sessions` and `/launch/board` (`peers[]`) show the other
  laptop's sessions under "on <name>" and what failed there - never its token.
- **Who leads, in order:** a laptop that can work (login fine, window not
  spent) before one that cannot; then the one marked `peers lead`; then the
  one with clearly more budget (a ten-point gap); then the first by name. A
  laptop left alone for weeks whose Claude login lapses stops leading on its
  next pulse, the other rings once "home cannot run fixes (its agent wants a
  login)", and takes over. When it can again, it leads again.
- `corgi agent watch ignore ENG-5` on one laptop ignores it on both;
  `corgi agent mute 60` on one mutes both for the hour.
- The lead moving (a laptop went silent, or came back) rings once - unless
  it flapped within fifteen minutes, then it is only logged.
- `corgi agent doctor --away` has a `peers` line; Telegram `/peers` prints
  the same summary. `corgi agent peers --json` never prints tokens.
- Same ticket, or same branch of the same repo, open on both laptops →
  "crossing laptops" rings once.

**A laptop alone for weeks** (see also `corgi agent doctor --away`): keep it
plugged in with the lid open or `corgi agent awake`; turn off automatic
macOS restarts (a reboot lands on the FileVault password screen and nothing
runs until someone types it); `corgi agent install` so the daemon returns
after a crash; tunnel credentials (cloudflared, ngrok) and peer tokens do not
expire; a Claude login that does lapse takes only that laptop out of the
lead - the other keeps working, and `corgi agent profile add` there gives it
a second account to fall back on.

How it holds together: a peer is a device in the other laptop's
`devices.json` with role `peer` - a bearer token plus the same end-to-end key
a phone gets - and that token opens only `/launch/peers/pulse` and
`/launch/peers/join` (never the board, a transcript or a send). Peers pulse
each other once a minute with the trackers they watch (`linear/<project>`,
`github/<owner/repo>`), stored in `<agentDir>/peers.json` (0600). Both need a
URL the other can reach: a tunnel each, or the same Wi-Fi.
