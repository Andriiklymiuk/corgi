# The smallest change that works

Generated code tends to build too much: a component where a native element exists, a
helper the repo already has, a dependency for one call, an abstraction with one
caller. Every line of that is a line someone reads, tests, and maintains. Before
writing anything, walk down this ladder and **stop at the first rung that holds**.

## The ladder
1. **Not needed.** The spec does not ask for it and the user would not notice its
   absence. Leave it out and say so in one line.
2. **Already in the repo.** A helper, component, config path or query exists — grep
   before you write (`rg -n '<what you are about to name>'`). Call it.
3. **The language ships it.** The standard library or runtime API covers it (dates,
   URLs, sorting, parsing, hashing, `Intl`, `fetch`). Use it.
4. **The platform ships it.** A native browser, OS, framework or database feature does
   it — `<input type=date>`, `<dialog>`, form validation, CSS, a unique constraint, a
   foreign key, a framework middleware. Use it.
5. **An installed dependency does it.** Something already in the lockfile covers it.
   Use it; never add a dependency for what an installed one already does.
6. **It is one clear line.** Write that line.
7. **Otherwise** write the least code that makes the spec's check pass.

Climb it only after the problem is understood end to end — the ladder is a reflex for
*how much* to write, never a substitute for reading the code you are changing.

## Rules that go with it
- **Delete before you add.** A change that removes lines and still passes the check
  beats one that adds them.
- **Boring beats clever.** Two built-ins fit → the one that is right on the edge
  cases, not the shorter one.
- **Fix the cause once**, in the function the callers share — not a guard in every
  caller.
- **No abstraction nobody asked for.** An interface with one implementation, a layer
  that only forwards, a config knob the ticket never mentioned, "in case we need it
  later" — out.
- **A deliberate shortcut is recorded, not hidden.** When rung 6 or 7 leaves a known
  ceiling — a limit, an unhandled scale, a case deferred — it goes in the PR/MR body
  under `Deferred`, one line each: `<what> · ceiling: <the limit> · revisit when:
  <the trigger>`. Never as a code comment (the repo rule stands) and never silently:
  a deferral with no trigger is the one that rots into "later means never".

## Never trimmed
The ladder decides how much to write, not what to skip. These stay whatever rung you
stop on:
- input validation at a trust boundary (request, file, message, env);
- error handling that prevents data loss or a silent wrong result;
- security and accessibility requirements;
- a feature the ticket explicitly asks for;
- the test the story's tier requires (a one-line change still gets its check;
  non-trivial logic gets one runnable check, whatever the framework).

## Saying what was left out
After the code, one line per thing skipped and the rung that covered it — *"no custom
date picker: `<input type=date>` (rung 4)"*. No essay: if the explanation is longer
than the change, cut the explanation.
