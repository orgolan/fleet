#!/usr/bin/env bash
# Read-only health check for a fleet install: is it installed correctly, and is
# it in good shape? Prints ok / warn / FAIL lines with a fix for each problem.
# Exit status is 1 if anything FAILed, 0 otherwise. Warnings never fail.
#   scripts/doctor.sh          fast checks
#   scripts/doctor.sh --full   also run the test suite
set -u

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PREFIX=${PREFIX:-$HOME/.local}
FLEET_HOME_DIR=${FLEET_HOME:-$HOME/.local/state/fleet}
FULL=0
[ "${1:-}" = "--full" ] && FULL=1

fails=0 warns=0
ok()   { printf 'ok    %s\n' "$*"; }
warn() { printf 'warn  %s\n' "$1"; [ -n "${2:-}" ] && printf '      fix: %s\n' "$2"; warns=$((warns + 1)); }
fail() { printf 'FAIL  %s\n' "$1"; [ -n "${2:-}" ] && printf '      fix: %s\n' "$2"; fails=$((fails + 1)); }
section() { printf '\n== %s ==\n' "$*"; }

# version_ge "0.9.1" 0 9  -> success if major.minor >= 0.9
version_ge() {
	v=${1#v}; major=${v%%.*}; rest=${v#*.}; minor=${rest%%[!0-9]*}
	case $major in ''|*[!0-9]*) return 1 ;; esac
	[ -n "$minor" ] || return 1
	[ "$major" -gt "$2" ] || { [ "$major" -eq "$2" ] && [ "$minor" -ge "$3" ]; }
}

section "Tools"
command -v git >/dev/null 2>&1 && ok "git" || fail "git not found" "install git"
if command -v herdr >/dev/null 2>&1; then
	hv=$(herdr --version 2>/dev/null | grep -Eo '[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n1)
	if version_ge "${hv:-x}" 0 9; then ok "herdr $hv"; else fail "herdr ${hv:-?} is older than 0.9" "update herdr: https://herdr.dev"; fi
else
	fail "herdr not found" "install herdr >= 0.9: https://herdr.dev"
fi
command -v claude >/dev/null 2>&1 && ok "claude (Claude Code)" || fail "claude not found" "install Claude Code: https://claude.com/claude-code"
# Same search order as scripts/install.sh: $GO, then the first new-enough Go among
# PATH, ~/.local/go, /usr/local/go; else the first one found.
GOBIN=${GO:-}
if [ -z "$GOBIN" ]; then
	for c in "$(command -v go 2>/dev/null || true)" "$HOME/.local/go/bin/go" /usr/local/go/bin/go; do
		[ -n "$c" ] && [ -x "$c" ] || continue
		[ -n "$GOBIN" ] || GOBIN=$c
		gv=$("$c" env GOVERSION 2>/dev/null); gv=${gv#go}
		if version_ge "${gv:-x}" 1 27; then GOBIN=$c; break; fi
	done
fi
if [ -n "$GOBIN" ]; then
	gv=$("$GOBIN" env GOVERSION 2>/dev/null); gv=${gv#go}
	if version_ge "${gv:-x}" 1 27; then ok "go $gv"; else warn "go ${gv:-?} is older than 1.27 (only needed to rebuild)" "install Go >= 1.27: https://go.dev/dl"; fi
else
	warn "go not found (only needed to rebuild fleet)" "install Go >= 1.27: https://go.dev/dl"
fi

section "Binary"
BIN=$ROOT/bin/fleet
if [ -x "$BIN" ]; then ok "built: $BIN"; else fail "bin/fleet is not built" "scripts/install.sh build"; fi
if [ -x "$BIN" ]; then
	newer=$(find "$ROOT/cmd" "$ROOT/internal" "$ROOT/go.mod" -newer "$BIN" \( -name '*.go' -o -name go.mod \) 2>/dev/null | head -n1)
	[ -z "$newer" ] && ok "bin/fleet is up to date with the source" || warn "source is newer than bin/fleet ($newer)" "scripts/install.sh build"
fi
if onpath=$(command -v fleet 2>/dev/null); then
	if [ "$(readlink -f "$onpath")" = "$(readlink -f "$BIN" 2>/dev/null)" ]; then
		ok "fleet on PATH is this checkout ($onpath)"
	else
		warn "fleet on PATH ($onpath) is not this checkout's bin/fleet" "scripts/install.sh install (links it; a copy can't find projects/)"
	fi
else
	fail "fleet is not on PATH" "scripts/install.sh install, and put $PREFIX/bin on PATH"
fi
case ":$PATH:" in *":$PREFIX/bin:"*) ok "$PREFIX/bin is on PATH" ;; *) warn "$PREFIX/bin is not on PATH" "add it to your shell profile" ;; esac

section "Skills"
for s in fleet fleet-doctor; do
	[ -d "$ROOT/skills/$s" ] || continue
	if [ -e "$ROOT/.claude/skills/$s/SKILL.md" ]; then ok "project skill $s (works when Claude Code is opened in this repo)"; else warn "project skill $s missing" "ln -s ../../skills/$s .claude/skills/$s"; fi
done
u=$HOME/.claude/skills/fleet
if [ -L "$u" ] && [ "$(readlink -f "$u")" = "$(readlink -f "$ROOT/skills/fleet")" ]; then
	ok "user skill fleet -> this checkout (works in any repo)"
elif [ -e "$u" ]; then
	warn "$u exists but is not a link to this checkout" "scripts/install.sh install-skill (refuses to replace a non-link; move it first)"
else
	warn "user skill fleet is not installed (fleet only works inside this repo)" "scripts/install.sh install-skill"
fi

section "fleet doctor"
if command -v fleet >/dev/null 2>&1 || [ -x "$BIN" ]; then
	f=$(command -v fleet || echo "$BIN")
	out=$("$f" doctor 2>&1); rc=$?
	printf '%s\n' "$out" | sed 's/^/  /'
	[ $rc -eq 0 ] && ok "fleet doctor passed" || fail "fleet doctor reported failures (see above)" "fix the FAIL lines, then re-run"
else
	warn "skipped: no fleet binary"
fi

section "Projects"
PROJ=${FLEET_PROJECTS:-$ROOT/projects}
if [ -d "$PROJ" ]; then
	n=0
	if [ -x "$BIN" ]; then
		while IFS=$'\t' read -r name path; do
			[ -z "$name" ] && continue
			n=$((n + 1))
			if git -C "$path" rev-parse --git-dir >/dev/null 2>&1; then
				ok "project $name -> $path"
				# leftover = fleet/* branches that no worktree has checked out (live crewmates use theirs)
				inuse=$(git -C "$path" worktree list --porcelain | sed -n 's#^branch refs/heads/##p')
				br=$(git -C "$path" branch --list 'fleet/*' --format='%(refname:short)' | grep -vxF -e "$inuse" -e '' 2>/dev/null | wc -l)
				[ -z "$inuse" ] && br=$(git -C "$path" branch --list 'fleet/*' | wc -l)
				[ "$br" -gt 0 ] && warn "$name has $br leftover fleet/* branch(es)" "after merging, delete them: git -C $path branch --list 'fleet/*'"
			else
				fail "project $name points at $path, which is not a git repo" "fleet project rm $name && fleet project add $name <path>"
			fi
		done < <("$BIN" project list 2>/dev/null | awk 'NR>1 {print $1 "\t" $3}')
	fi
	[ "$n" -eq 0 ] && warn "no projects registered" "fleet project new <name> | clone <name> <url> | add <name> <path>"
else
	warn "projects folder $PROJ does not exist yet" "fleet project new <name>"
fi

section "Housekeeping"
if [ -d "$FLEET_HOME_DIR/tasks" ]; then
	stopped=$(grep -l '"state": "stopped"' "$FLEET_HOME_DIR"/tasks/*.json 2>/dev/null | wc -l)
	[ "$stopped" -gt 5 ] && warn "$stopped stopped task records in $FLEET_HOME_DIR/tasks" "fleet prune --dry-run, then fleet prune" || ok "task ledger: $stopped stopped record(s)"
fi
if command -v herdr >/dev/null 2>&1 && [ "${HERDR_ENV:-}" = 1 ]; then
	sw=$(herdr workspace list 2>/dev/null | grep -o '"label":"fleet-supervisor"' | wc -l)
	case $sw in
	0) ok "no supervisor workspace open (spawn or 'fleet up' will start one)" ;;
	1) ok "one fleet-supervisor workspace" ;;
	*) warn "$sw fleet-supervisor workspaces are open" "close the extras in herdr; only one supervisor runs" ;;
	esac
else
	warn "not inside herdr; skipped workspace checks" "run this from a herdr pane"
fi

if [ $FULL -eq 1 ]; then
	section "Tests"
	if "$ROOT/scripts/install.sh" test >/dev/null 2>&1; then ok "go vet + go test -race passed"; else fail "tests failed" "scripts/install.sh test"; fi
fi

printf '\n%d failure(s), %d warning(s)\n' "$fails" "$warns"
[ "$fails" -eq 0 ]
