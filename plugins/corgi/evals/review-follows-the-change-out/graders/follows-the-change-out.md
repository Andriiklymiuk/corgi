---
type: llm
---
Score 1 if the assistant leaves the diff for evidence on all three counts: (a) it asks where each deployed environment sets RATE_LIMIT_PER_MINUTE (a deploy workflow, task definition or secrets list) instead of accepting `.env.example` as delivery, and names what the fallback default does in production; (b) it greps for the other handlers that build the same guard and asks whether they received checkRateLimit() too; (c) it flags the metadata-only test as not exercising the rate limit — a test that stays green with the guard removed. Score 0 if it reviews only the diffed lines, or accepts the PR body's claims about delivery and tests.
