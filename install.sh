#!/bin/sh
# corgi installer
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Andriiklymiuk/corgi/main/install.sh | sh
#
# Environment variables:
#   CORGI_VERSION         Pin a specific version (e.g. "1.10.0"). Default: latest GitHub release.
#   CORGI_INSTALL_DIR     Force install directory. Default: /usr/local/bin if writable, else ~/.local/bin.
#   CORGI_NO_MODIFY_PATH  Set to 1 to skip auto-appending the install dir to your shell rc file.

set -eu

REPO="Andriiklymiuk/corgi"

log()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
err()  { printf 'error: %s\n' "$*" >&2; exit 1; }

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || err "missing required command: $1"
}

require_cmd uname
require_cmd tar
require_cmd mkdir
require_cmd mv
require_cmd chmod
require_cmd rm

# Pick a downloader.
if command -v curl >/dev/null 2>&1; then
    DOWNLOAD="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then
    DOWNLOAD="wget -qO"
else
    err "need curl or wget to download corgi"
fi

# Detect OS.
os_raw=$(uname -s)
case "$os_raw" in
    Linux)  OS=linux ;;
    Darwin) OS=darwin ;;
    *) err "unsupported OS: $os_raw (Windows users: install via scoop/winget or download from GitHub releases)" ;;
esac

# Detect arch.
arch_raw=$(uname -m)
case "$arch_raw" in
    x86_64|amd64)   ARCH=amd64 ;;
    aarch64|arm64)  ARCH=arm64 ;;
    i386|i686)      ARCH=386 ;;
    *) err "unsupported architecture: $arch_raw" ;;
esac

# Resolve version.
VERSION="${CORGI_VERSION:-}"
if [ -z "$VERSION" ]; then
    log "fetching latest release..."
    if command -v curl >/dev/null 2>&1; then
        TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
            | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
    else
        TAG=$(wget -qO- "https://api.github.com/repos/$REPO/releases/latest" \
            | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
    fi
    [ -n "$TAG" ] || err "could not resolve latest version from GitHub API"
    VERSION="${TAG#v}"
fi

ASSET="corgi_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/v${VERSION}/${ASSET}"

# Pick install directory.
INSTALL_DIR="${CORGI_INSTALL_DIR:-}"
if [ -z "$INSTALL_DIR" ]; then
    if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
        INSTALL_DIR=/usr/local/bin
    else
        INSTALL_DIR="$HOME/.local/bin"
    fi
fi
mkdir -p "$INSTALL_DIR" || err "cannot create install dir: $INSTALL_DIR"
[ -w "$INSTALL_DIR" ] || err "install dir not writable: $INSTALL_DIR (try: CORGI_INSTALL_DIR=\$HOME/.local/bin sh)"

# Download into a temp dir we clean up on exit.
TMP=$(mktemp -d 2>/dev/null || mktemp -d -t corgi)
trap 'rm -rf "$TMP"' EXIT INT TERM HUP

log "downloading $ASSET (v$VERSION, $OS/$ARCH)..."
# shellcheck disable=SC2086
$DOWNLOAD "$TMP/$ASSET" "$URL" || err "download failed: $URL"

# Verify checksum if a verifier is available.
SUMS_URL="https://github.com/$REPO/releases/download/v${VERSION}/checksums.txt"
# shellcheck disable=SC2086
if $DOWNLOAD "$TMP/checksums.txt" "$SUMS_URL" 2>/dev/null; then
    if command -v sha256sum >/dev/null 2>&1; then
        SHA_CMD="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        SHA_CMD="shasum -a 256"
    else
        SHA_CMD=""
    fi
    if [ -n "$SHA_CMD" ]; then
        EXPECTED=$(grep " $ASSET\$" "$TMP/checksums.txt" | awk '{print $1}')
        if [ -n "$EXPECTED" ]; then
            ACTUAL=$($SHA_CMD "$TMP/$ASSET" | awk '{print $1}')
            [ "$EXPECTED" = "$ACTUAL" ] || err "checksum mismatch for $ASSET"
            log "checksum ok"
        else
            warn "warning: $ASSET not found in checksums.txt, skipping verification"
        fi
    else
        warn "warning: no sha256 tool found, skipping checksum verification"
    fi
else
    warn "warning: could not download checksums.txt, skipping checksum verification"
fi

tar -xzf "$TMP/$ASSET" -C "$TMP" || err "extract failed"
[ -f "$TMP/corgi" ] || err "archive did not contain a corgi binary"

chmod +x "$TMP/corgi"
mv "$TMP/corgi" "$INSTALL_DIR/corgi" || err "could not move binary into $INSTALL_DIR"

log "installed corgi $VERSION -> $INSTALL_DIR/corgi"

# Add to PATH automatically if the install dir isn't already on PATH.
in_path=0
case ":$PATH:" in
    *":$INSTALL_DIR:"*) in_path=1 ;;
esac

if [ "$in_path" -eq 0 ]; then
    if [ "${CORGI_NO_MODIFY_PATH:-0}" = "1" ]; then
        warn ""
        warn "note: $INSTALL_DIR is not in your PATH (CORGI_NO_MODIFY_PATH=1)."
        warn "add this line to your shell profile manually:"
        warn "  export PATH=\"$INSTALL_DIR:\$PATH\""
    else
        shell_name=$(basename "${SHELL:-sh}")
        case "$shell_name" in
            zsh)
                rc="$HOME/.zshrc"
                line="export PATH=\"$INSTALL_DIR:\$PATH\""
                ;;
            bash)
                if [ -f "$HOME/.bashrc" ]; then
                    rc="$HOME/.bashrc"
                else
                    rc="$HOME/.bash_profile"
                fi
                line="export PATH=\"$INSTALL_DIR:\$PATH\""
                ;;
            fish)
                rc="$HOME/.config/fish/conf.d/corgi.fish"
                line="fish_add_path $INSTALL_DIR"
                ;;
            *)
                rc=""
                ;;
        esac

        if [ -n "$rc" ]; then
            mkdir -p "$(dirname "$rc")"
            touch "$rc"
            if grep -Fq "$INSTALL_DIR" "$rc" 2>/dev/null; then
                log "PATH entry for $INSTALL_DIR already present in $rc"
            else
                {
                    printf '\n# Added by corgi installer\n'
                    printf '%s\n' "$line"
                } >> "$rc"
                log "added $INSTALL_DIR to PATH in $rc"
            fi
            warn ""
            warn "restart your shell, or run: source $rc"
        else
            warn ""
            warn "note: $INSTALL_DIR is not in your PATH and your shell ($shell_name) is not auto-configured."
            warn "add this line to your shell profile:"
            warn "  export PATH=\"$INSTALL_DIR:\$PATH\""
        fi
    fi
fi

log "run 'corgi -h' to get started."
