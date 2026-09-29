#!/bin/sh
# fleet build/install helper. Usage:
#   scripts/install.sh [build|test|install|install-skill|uninstall|check-go|clean]
# Env: GO (go binary), PREFIX (default $HOME/.local).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PREFIX=${PREFIX:-$HOME/.local}
BIN_SRC=$ROOT/bin/fleet
BIN_DST=$PREFIX/bin/fleet
SKILL_SRC=$ROOT/skills/fleet
SKILL_DST=$HOME/.claude/skills/fleet
MIN_MAJOR=1
MIN_MINOR=27

err() { echo "error: $*" >&2; }

find_go() {
	if [ -n "${GO:-}" ]; then
		printf '%s\n' "$GO"
	elif command -v go >/dev/null 2>&1; then
		command -v go
	elif [ -x "$HOME/.local/go/bin/go" ]; then
		printf '%s\n' "$HOME/.local/go/bin/go"
	fi
}

# Sets GOBIN_PATH; exits 1 if Go is missing or older than 1.27.
check_go() {
	GOBIN_PATH=$(find_go)
	if [ -z "$GOBIN_PATH" ]; then
		err "Go not found; install Go >= $MIN_MAJOR.$MIN_MINOR, set GO=/path/to/go, or put it in \$HOME/.local/go/bin"
		exit 1
	fi
	v=$("$GOBIN_PATH" env GOVERSION 2>/dev/null || true)
	v=${v#go}
	major=${v%%.*}
	rest=${v#*.}
	minor=${rest%%[!0-9]*}
	case $major in ''|*[!0-9]*) major= ;; esac
	if [ -z "$major" ] || [ -z "$minor" ] || [ "$rest" = "$v" ]; then
		err "cannot parse Go version '$v' from $GOBIN_PATH (try PATH=\$HOME/.local/go/bin:\$PATH)"
		exit 1
	fi
	if [ "$major" -lt "$MIN_MAJOR" ] || { [ "$major" -eq "$MIN_MAJOR" ] && [ "$minor" -lt "$MIN_MINOR" ]; }; then
		err "Go >= $MIN_MAJOR.$MIN_MINOR required, found $v at $GOBIN_PATH (try PATH=\$HOME/.local/go/bin:\$PATH or GO=/path/to/go)"
		exit 1
	fi
}

cmd_build() {
	check_go
	cd "$ROOT"
	"$GOBIN_PATH" build -o bin/fleet ./cmd/fleet
	echo "built $BIN_SRC"
}

cmd_test() {
	check_go
	cd "$ROOT"
	"$GOBIN_PATH" vet ./...
	"$GOBIN_PATH" test -race ./...
}

cmd_install_skill() {
	if [ ! -d "$SKILL_SRC" ]; then
		err "skill source $SKILL_SRC not found"
		exit 1
	fi
	mkdir -p "$(dirname "$SKILL_DST")"
	if [ -L "$SKILL_DST" ]; then
		rm "$SKILL_DST"
	elif [ -e "$SKILL_DST" ]; then
		err "$SKILL_DST exists and is not a symlink; refusing to overwrite"
		exit 1
	fi
	ln -s "$SKILL_SRC" "$SKILL_DST"
	echo "linked $SKILL_DST -> $SKILL_SRC"
}

cmd_install() {
	cmd_build
	mkdir -p "$PREFIX/bin"
	tmp=$(mktemp "$PREFIX/bin/.fleet.XXXXXX")
	if ! { cp "$BIN_SRC" "$tmp" && chmod 0755 "$tmp" && mv -f "$tmp" "$BIN_DST"; }; then
		rm -f "$tmp"
		err "failed to install $BIN_DST"
		exit 1
	fi
	echo "installed $BIN_DST"
	cmd_install_skill
	case ":$PATH:" in
	*":$PREFIX/bin:"*) ;;
	*) echo "warning: $PREFIX/bin is not on PATH; add it to use 'fleet'" >&2 ;;
	esac
}

cmd_uninstall() {
	if [ -e "$BIN_DST" ] || [ -L "$BIN_DST" ]; then
		rm -f "$BIN_DST"
		echo "removed $BIN_DST"
	fi
	if [ -L "$SKILL_DST" ]; then
		rm "$SKILL_DST"
		echo "removed $SKILL_DST"
	elif [ -e "$SKILL_DST" ]; then
		echo "leaving $SKILL_DST: not a symlink" >&2
	fi
}

cmd_clean() {
	rm -rf "$ROOT/bin"
	echo "removed $ROOT/bin"
}

case "${1:-install}" in
build) cmd_build ;;
test) cmd_test ;;
install) cmd_install ;;
install-skill) cmd_install_skill ;;
uninstall) cmd_uninstall ;;
check-go) check_go; echo "ok: $("$GOBIN_PATH" env GOVERSION) ($GOBIN_PATH)" ;;
clean) cmd_clean ;;
*)
	err "unknown command '$1'; usage: $0 [build|test|install|install-skill|uninstall|check-go|clean]"
	exit 2
	;;
esac
