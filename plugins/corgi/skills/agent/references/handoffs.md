# Handoffs, the workpad, blocked, the kanban

A run that stops part-way leaves a **handoff**: a typed packet, never a
transcript, at `.corgi/corgi_services/handoffs/<ref>.json` with a Markdown
twin. Write one yourself when you stop with work remaining, before a carry,
or when you are blocked:

```bash
corgi agent handoff --ref ABC-123 --done "api returns 429" --remaining "web banner" \
  --decision "5h sliding window" --uncertain "retry on the phone?" --next "web banner" \
  --verify "corgi test --changed"        # runs it now, records exit + head
corgi agent handoff show ABC-123         # the packet; says how many commits since
corgi agent handoff verify ABC-123       # re-runs its check (must be a doneWhen line)
corgi agent handoff --ref ABC-123 --blocked "no GITLAB_TOKEN for the web repo"
```

The ref defaults to the ticket key in the branch name. A packet with a
secret or a TODO is refused. The next run - unattended, or a new session on
the branch (the SessionStart context says "handoff for ABC-123: read … first") -
re-runs the packet's check at the current head before trusting its done
list, but only when that check is one of the workspace's `doneWhen` lines
(the packet was written by a run, so its command is not the person's); a
check that fails, is not listed, or a branch that moved, means start from
the ticket and the diff. `corgi agent carry` writes a draft packet before it
moves a session, and past 85 % context (or `--fresh`) starts the new session
clean with the packet as its first prompt.

The **workpad** is the one corgi comment on a ticket, sections rewritten in
place: `Spec`, `Pull requests`, `Handoff`, `Blocked`. Post the spec there
(`corgi agent watch workpad ABC-123 Spec - < docs/stories/ABC-123.md`); corgi
adds the PRs a run opened, the handoff it left, the reason it is blocked.
The first line of the comment is `corgi · workpad`; never write a second.

**Blocked** is a column: two failed runs in a row on one ticket trip the
breaker; a handoff in state `blocked` or `auth-required` blocks it; a person
can too (`corgi agent watch block ABC-123 "needs the sandbox key"`). A
blocked ticket takes no budget and no run until `corgi agent watch unblock
ABC-123` (also on the phone). A quota hit or a missing credential never
counts as a failure.

`corgi agent watch enable --isolate` gives every unattended run its own
worktrees on `corgi/<ref>`, one per repository, so a run never touches the
checkout; `watch undo` releases them (keeping any with uncommitted work).
