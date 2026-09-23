---
type: llm
---
Score 1 if the assistant carries the fix through: it applies both requested changes on the merge request's branch, runs the tests, pushes, and replies on each thread - or, where it cannot (no access to the repo), says exactly which step it could not do and why. Score 0 if it stops to ask "shall I push?" or "want me to apply these?", ends on a list of next steps the user should run, or does one of the two changes and summarises the other.
