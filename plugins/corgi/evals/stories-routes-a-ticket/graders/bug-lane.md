---
type: llm
---
Score 1 if the assistant treats this as a bug: it reproduces or reads logs first, writes a regression test that fails before the fix, and does not write a plan file or fan out to subagents. Score 0 if it plans it like a feature or starts editing without reproducing.
