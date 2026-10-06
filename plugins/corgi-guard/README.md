# corgi-guard

A Claude Code mod that stops the Bash shapes corgi runs lose turns to, and hands the model the corgi fix:

- `set -- $pair` - zsh does not split it, so the second word arrives empty. Refused, with `read -r a b <<< "$pair"`.
- `rm -rf /tmp/corgi-wt/...` or `/tmp/corgi-review/...` - refused in a hardened workspace anyway. Refused, with the fresh-path recipe.
- A GraphQL call with an empty variable - the result carries a pointer to `corgi agent watch pr threads`.
- `gh pr checks` right after a push ("no checks reported") - the result carries the wait-for-checks loop.
- A corgi chat post with a dead Slack token - the result says to tell you and how to renew it.

## Install

```
/plugin install corgi-guard --marketplace Andriiklymiuk/corgi
```
