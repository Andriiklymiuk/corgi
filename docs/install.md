# Installing corgi

Once installed, `corgi` works from any folder. corgi is a single binary on a steady semver release train (1.x).

## macOS / Linux — [Homebrew](https://brew.sh)

```bash
brew install andriiklymiuk/homebrew-tools/corgi
```

## macOS / Linux — install script

No Homebrew? This one-liner grabs the right binary for your OS/arch from GitHub releases:

```bash
curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh
```

It verifies the release's sha256 checksum before installing, to `/usr/local/bin` if it can, otherwise `~/.local/bin` (and adds it to your PATH for zsh/bash/fish).

Optional overrides:

- `CORGI_VERSION=1.10.0` — pin a version
- `CORGI_INSTALL_DIR=$HOME/bin` — force a directory
- `CORGI_NO_MODIFY_PATH=1` — don't touch shell rc files

## Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.ps1 | iex
```

Installs to `%LOCALAPPDATA%\corgi\bin` and adds it to your user PATH.

## Windows — [Scoop](https://scoop.sh)

```powershell
scoop bucket add corgi https://github.com/Andriiklymiuk/scoop-bucket
scoop install corgi
```

## [mise](https://mise.jdx.dev) (tool/version manager)

```bash
mise use -g github:Andriiklymiuk/corgi
```

Reads corgi's GitHub releases directly — no registry config needed.

## [pkgx](https://pkgx.sh)

```bash
pkgx corgi run        # one-off, no install
pkgx install corgi    # to PATH
```

## Verify & update

```bash
corgi -h
```

`corgi upgrade` (alias `corgi update`) notices how you installed corgi and upgrades the same way. Pin the whole team to one version with `CORGI_VERSION` (install script) or mise, so everyone runs the same corgi.

Want to try it cold? Run the expo + hono example straight from a URL:

```bash
corgi run -t https://github.com/Andriiklymiuk/corgi_examples/blob/main/honoExpoTodo/hono-bun-expo.corgi-compose.yml
```

## VSCode extension

Install the [corgi extension](https://marketplace.visualstudio.com/items?itemName=corgi.corgi) for syntax highlighting, autocompletion, and one-click commands.

## Shell tab-completion

Brew installs `_corgi` (zsh), `corgi.bash`, `corgi.fish` automatically. After that:

- `corgi run --services <TAB>` → service names from `corgi-compose.yml`
- `corgi run --dbServices <TAB>` → db_services
- `corgi script -n <TAB>` → script names per service (filters by `--services` if set)
- `corgi tunnel <TAB>` → tunnelable services
- `corgi clean -i <TAB>` → clean targets — and completions are wired for `corgi tunnel --provider`, `corgi run --omit`, and the global `--dockerContext` / `--fromTemplateName` too

### Completion showing filenames instead of names? (zsh fpath / Linux setup)

**zsh users — if `<TAB>` shows files instead of names**, your shell isn't loading brew's site-functions dir. One-time fix in `~/.zshrc` (works for every brew CLI, not just corgi):

```sh
# macOS Apple Silicon
FPATH="/opt/homebrew/share/zsh/site-functions:$FPATH"
# macOS Intel
FPATH="/usr/local/share/zsh/site-functions:$FPATH"
# Linux (linuxbrew)
FPATH="/home/linuxbrew/.linuxbrew/share/zsh/site-functions:$FPATH"

autoload -Uz compinit && compinit
```

Add it BEFORE any existing `compinit` call. Then `rm -f ~/.zcompdump* && exec zsh`.

Why: brew drops completions in `<brew-prefix>/share/zsh/site-functions/`, but plain zsh doesn't include that path in `$fpath` by default — so the file is installed but never loaded. Same gap affects `gh`, `kubectl`, `helm`, etc.

**Linux native package managers** (apt/dnf/pacman) — corgi isn't packaged there yet. Use the install script, then generate the completion script manually:

```sh
# zsh
mkdir -p ~/.zsh/completions
corgi completion zsh > ~/.zsh/completions/_corgi
# add once to ~/.zshrc:
fpath=(~/.zsh/completions $fpath); autoload -Uz compinit && compinit

# bash (needs bash-completion package)
corgi completion bash | sudo tee /etc/bash_completion.d/corgi >/dev/null

# fish
corgi completion fish > ~/.config/fish/completions/corgi.fish
```
