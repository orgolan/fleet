#!/bin/sh
# fleet build/install helper. Usage:
#   scripts/install.sh [setup|build|test|install|install-skill|uninstall|check-go|clean]
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

# go_ok BIN: success if BIN is Go >= MIN_MAJOR.MIN_MINOR.
go_ok() {
	v=$("$1" env GOVERSION 2>/dev/null) || return 1
	v=${v#go}; major=${v%%.*}; rest=${v#*.}; minor=${rest%%[!0-9]*}
	case $major in ''|*[!0-9]*) return 1 ;; esac
	[ -n "$minor" ] && [ "$rest" != "$v" ] || return 1
	[ "$major" -gt "$MIN_MAJOR" ] || { [ "$major" -eq "$MIN_MAJOR" ] && [ "$minor" -ge "$MIN_MINOR" ]; }
}

# Prints $GO, else the first Go that is new enough among: go on PATH,
# $HOME/.local/go/bin/go, /usr/local/go/bin/go. If none is new enough, prints the
# first Go found so check_go can say which one is too old.
find_go() {
	if [ -n "${GO:-}" ]; then
		printf '%s\n' "$GO"
		return
	fi
	first=
	for c in "$(command -v go 2>/dev/null || true)" "$HOME/.local/go/bin/go" /usr/local/go/bin/go; do
		[ -n "$c" ] && [ -x "$c" ] || continue
		[ -n "$first" ] || first=$c
		if go_ok "$c"; then
			printf '%s\n' "$c"
			return
		fi
	done
	[ -z "$first" ] || printf '%s\n' "$first"
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

# install links (not copies) bin/fleet into PREFIX/bin, so the binary can always
# find this checkout and its projects/ folder.
cmd_install() {
	cmd_build
	mkdir -p "$PREFIX/bin"
	if [ -e "$BIN_DST" ] && [ ! -L "$BIN_DST" ]; then
		echo "replacing existing $BIN_DST with a link to $BIN_SRC"
	fi
	ln -sfn "$BIN_SRC" "$BIN_DST"
	echo "linked $BIN_DST -> $BIN_SRC"
	cmd_install_skill
	case ":$PATH:" in
	*":$PREFIX/bin:"*) ;;
	*) echo "warning: $PREFIX/bin is not on PATH; add it to use 'fleet'" >&2 ;;
	esac
}

# setup is the first-run onboarding: check what fleet needs, tell the captain
# exactly what is missing and how to get it, then build and install.
# It never installs Go, herdr or Claude Code itself.
cmd_setup() {
	missing=0
	need() { # name, found?, hint
		if [ "$2" = ok ]; then
			echo "ok    $1"
		else
			echo "MISS  $1: $3"
			missing=1
		fi
	}
	if command -v git >/dev/null 2>&1; then need git ok; else need git no "install git from your package manager"; fi
	if command -v herdr >/dev/null 2>&1; then
		need "herdr ($(herdr --version 2>/dev/null | head -n1))" ok
	else
		need herdr no "install herdr >= 0.9 from https://herdr.dev, then run this from a herdr pane"
	fi
	if command -v claude >/dev/null 2>&1; then need "claude (Claude Code)" ok; else need claude no "install Claude Code: https://claude.com/claude-code"; fi
	gobin=$(find_go)
	if [ -n "$gobin" ] && ( check_go ) >/dev/null 2>&1; then
		need "go ($("$gobin" env GOVERSION))" ok
	else
		need "go >= $MIN_MAJOR.$MIN_MINOR" no "install from https://go.dev/dl (or unpack it at \$HOME/.local/go)"
	fi
	if [ "${HERDR_ENV:-}" = 1 ]; then echo "ok    running inside herdr"; else echo "warn  not inside herdr: run 'herdr', then start Claude Code in a pane before using the crew"; fi
	if [ "$missing" -ne 0 ]; then
		err "install the missing tools above, then run: scripts/install.sh setup"
		exit 1
	fi
	cmd_install
	mkdir -p "$ROOT/projects"
	echo
	"$BIN_DST" doctor || true
	echo
	echo "fleet is ready. Add your first project:"
	echo "  fleet project new <name>          # empty repo under projects/"
	echo "  fleet project clone <name> <url>  # clone a repo under projects/"
	echo "  fleet project add <name> <path>   # use a repo you already have"
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
setup) cmd_setup ;;
build) cmd_build ;;
test) cmd_test ;;
install) cmd_install ;;
install-skill) cmd_install_skill ;;
uninstall) cmd_uninstall ;;
check-go) check_go; echo "ok: $("$GOBIN_PATH" env GOVERSION) ($GOBIN_PATH)" ;;
clean) cmd_clean ;;
*)
	err "unknown command '$1'; usage: $0 [setup|build|test|install|install-skill|uninstall|check-go|clean]"
	exit 2
	;;
esac
