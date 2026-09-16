---
type: llm
---
Score 1 if the assistant proposes exactly one idea with evidence it can point at (a file and line, a README promise, a missing test), puts it on the board with `corgi agent task add`, records it with `corgi suggest-history record`, asks nothing, and starts its answer with a one-line headline. Score 0 if it writes a file under docs/, changes code, files a tracker ticket without an opt-in, proposes several ideas, or ends on a question.
