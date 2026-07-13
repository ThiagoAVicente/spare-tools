#!/usr/bin/env sh
# install.sh — build and install a spare-tool (re-running updates/overwrites).
#
# Usage:
#   ./install.sh <tool>       install one tool (alone, notify, waitfor, countdown, freshname, recent)
#   ./install.sh all          install every tool
#   BINDIR=~/bin ./install.sh <tool>   custom install dir (default: ~/.local/bin)
set -eu

TOOLS="alone notify waitfor countdown freshname recent spare"
BINDIR="${BINDIR:-$HOME/.local/bin}"
REPO_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"

usage() {
    echo "usage: $0 <tool>|all"
    echo "tools: $TOOLS"
    exit 2
}

[ $# -eq 1 ] || usage

if ! command -v go >/dev/null 2>&1; then
    echo "error: go toolchain not found (https://go.dev/dl/)" >&2
    exit 1
fi

install_one() {
    tool="$1"
    case " $TOOLS " in
        *" $tool "*) ;;
        *) echo "error: unknown tool '$tool'" >&2; usage ;;
    esac
    printf 'installing %s -> %s/%s ... ' "$tool" "$BINDIR" "$tool"
    (cd "$REPO_DIR" && go build -trimpath -ldflags='-s -w' -o "$BINDIR/$tool" "./cmd/$tool")
    echo "ok"
}

mkdir -p "$BINDIR"

if [ "$1" = "all" ]; then
    for t in $TOOLS; do install_one "$t"; done
else
    install_one "$1"
fi

case ":$PATH:" in
    *":$BINDIR:"*) ;;
    *) echo "note: $BINDIR is not in your PATH" >&2 ;;
esac
