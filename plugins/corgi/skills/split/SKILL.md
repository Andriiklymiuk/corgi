---
name: split
description: "Use to cut a spec, HLD, RFC, PRD, epic or idea page into tickets: \"split this into tickets\", \"break this spec down\", \"ticket this HLD\", \"the tickets are one per repo, redo them\". One ticket per whole feature across backend, frontend and every repo it touches, never one per layer. NOT writing the page (idea), building the tickets (stories) or tracker status (tracker)."
---

# Corgi split: one ticket per feature, never per layer

**Done when:** every ticket is one thing a person can see or do, it names what it touches
and how a tester sees it is done, the order is said, and the split sits where the work
starts from (the idea page, the board or the tracker).

Read `../_shared/conventions.md` first.

## The rule

A ticket is a **feature slice**: one thing a person gets, built end to end. Its migration,
its API, its screen, its background job and its tests are all in that one ticket, in every
repository it reaches. One session carries it on one branch across those repositories and
opens one pull request per repository, each linking the others.

```
wrong: by layer                      right: by feature
1 Add the limits migration           1 Show the owner a banner at the limit
2 Build the limits endpoint            (migration + endpoint + banner + tests; api, web)
3 Build the limits banner            2 Mail the owner at 80 percent
4 Write the tests                      (job + template + tests; worker)   after 1
```

Why: a layer ticket ships nothing a person can check. The migration merges with no
reader, the endpoint with no caller, the screen waits for both, and the contract between
them is guessed three times by three sessions. A slice is checked by using it.

## How to cut

1. **Read the source whole**: the page (`corgi idea show <slug>`), the epic and its
   children, or the file. Read the stack too: `corgi-compose.yml` says which services
   live in which repository.
2. **List what a person gets**, in their words: each user story, each flow, each screen
   state that can ship alone. That list is the tickets. Not the list of files, tables or
   services.
3. **Thinnest first.** Ticket 1 is the smallest slice that works end to end: one path,
   one screen, real data. Later tickets widen it (the error states, the second role, the
   settings, the mail).
4. **Groundwork only when shared.** Cut a groundwork ticket only for something two or
   more slices need and none can own: a new service, a shared sign-in change, a design
   token set. One slice needs it → it belongs in that slice.
5. **Say the order and what may run side by side.** `after` names the tickets a ticket
   waits for. Two tickets that would edit the same files wait for one another. Tickets
   that do not may run at once; keep those apart on purpose (different screens,
   different tables), and say in each what not to touch.
6. **Size:** one session, one sitting. Too big → cut along what a person gets (happy
   path, then the edge), never along the layer. Two to eight tickets; more means the
   page is two pages.

## Each ticket

| Field | What goes in |
|---|---|
| title | What a teammate says picking it up: a verb, the thing, the person it serves. Under 80 characters. "Show the owner a banner at the limit", not "Limits banner component" |
| body | Two or three short paragraphs: what to build and why it matters to the person using it; then where to start in each repository, the trap, what not to touch |
| touches | The services or repositories it changes, by the names the compose file uses |
| checks | Two to five things a tester does to see it is done, one sentence each: "the owner sees the banner at 100 requests" |
| after | The numbers of the tickets it waits for |

## Check the cut before you hand it over

- Read only the titles. Does each say what a person gets? A title that is a noun, a
  layer ("Backend:", "Add migration", "Write tests") or a repository name is a layer cut.
- In a workspace with two or more repositories: does at least one ticket cross them? If
  every ticket stays inside one repository, the features were cut in half. Merge them.
- Could ticket 1 be demoed alone? If it needs ticket 2 to be seen, merge or reorder.
- Is every line of the source covered by a ticket, and no ticket invented beyond it?
- "Write tests", "Update docs", "Refactor" as a ticket of its own → fold it into the
  slice it belongs to.

## Where the split goes

- **An idea page** (the usual place): `corgi_idea_edit` with `tickets`
  (`[{title, body, after, touches, checks}]`), or by hand `corgi idea tickets <slug>` to
  read it. The tool answers with `splitWarning` when the cut is by layer; fix it and send
  again. `corgi idea tickets <slug> --recut` lets corgi cut an old split again by feature.
  Then `corgi idea build <slug>` puts it on the board, and `corgi idea send <slug> --to
  linear|jira` makes the epic and its tickets. Build cuts a layered split again on its
  own, and starts a ticket that waits for one still in review from that one's branch.
- **A goal with no page:** `corgi agent plan "<goal>" --workspace <id>`; the planner
  follows the same rule. `--stack` makes a waiting task build on the branch it waits for.
- **A tracker epic:** preview the set, one confirm, then create the tickets under the
  epic with blocks-links for `after` (the `tracker` skill owns the writes). Put `touches`
  and `checks` in each description.

## Stop and ask when

- The source is under three lines or two readings fit: write the page first (`idea`
  skill, `--type spec`), split after.
- A slice cannot be made thin: it needs a migration of live data, a new vendor, or a
  change to sign-in. Say so, cut that one as groundwork, and say who should review it.
