---
description: Verify a web change in a real browser on the running stack - drive the page, read the screenshot, the console and the network.
---

Run the corgi **web** flow for `$ARGUMENTS`.

- `$ARGUMENTS` = a route, a URL, a service or the flow to click through. Empty → the page the current branch changed.

Follow the `web` skill (load it with the Skill tool: `corgi:web`): the stack is healthy first,
then the best driver the session has, the ticket's path plus one wrong input, a reload after
every write, and two widths when the layout changed.

Report the page, the flow, the console, the network, and what was not run.
