# corgi-guard

A Claude Code mod that stops the Bash shapes corgi runs lose turns to, and hands the model the corgi fix:

- `set -- $pair` - zsh does not split it, so the second word arrives empty. Refused, with `read -r a b <<< "$pair"`.
- `$PIPESTATUS` - blank in zsh, so the check it guards passes on nothing. Refused, with `$pipestatus[1]`.
- `echo ====` - zsh reads a word starting with `=` as a command and stops. The word is quoted before it runs.
- `grep --include=*.ts` - zsh stops on "no matches found". The glob is quoted before it runs (`--include='*.ts'`), which bash reads the same way.
- "Correction, ignore my earlier comment" posted on a PR, MR or ticket, from Bash or a tracker's own tool - refused, with how to edit or delete the wrong one.
- `rm -rf /tmp/corgi-wt/...` or `/tmp/corgi-review/...` - refused in a hardened workspace anyway. Refused, with the fresh-path recipe.
- A GraphQL call with an empty variable - the result carries a pointer to `corgi agent watch pr threads`.
- `gh pr checks` right after a push ("no checks reported") - the result carries the wait-for-checks loop.
- A corgi chat post with a dead Slack token - the result says to tell you and how to renew it.

## Install

```
/plugin install corgi-guard --marketplace Andriiklymiuk/corgi
```
