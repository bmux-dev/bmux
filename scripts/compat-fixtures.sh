#!/usr/bin/env bash
# Records what a bmux release produces, for the compatibility tests:
#   testdata/releases/TAG/help.txt            bmux --help
#   testdata/releases/TAG/default-config.toml bmux default-config (v0.3.0 on)
#   testdata/releases/TAG/sessions.json       a save file its server wrote (v0.2.0 on)
# The session has three panes: pane 1 split side by side, the new pane split
# stacked, and from v0.4.0 on a second window. Its last pane cds into sub/.
# The project directory reads @PROJ@ so tests can put it anywhere.
#
# Usage: scripts/compat-fixtures.sh TAG    (needs go, git and tmux)
set -euo pipefail
tag=${1:?usage: scripts/compat-fixtures.sh TAG}
root=$(git rev-parse --show-toplevel)
out=$root/testdata/releases/$tag
work=$(mktemp -d)
cleanup() {
	tmux -L "fixtures-$$" kill-server 2>/dev/null || true
	for p in $(pgrep -f "$work/bmux --serve" || true); do kill "$p" 2>/dev/null || true; done
	git -C "$root" worktree remove --force "$work/src" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

git -C "$root" worktree add -q --detach "$work/src" "$tag"
(cd "$work/src" && go build -o "$work/bmux" ./cmd/bmux)
bmux=$work/bmux
mkdir -p "$out" "$work/home" "$work/proj/sub"
chmod 700 "$work/home"
mkdir -m 700 "$work/run"
printf 'onboarding = false\n' >"$work/config.toml"
run() {
	env -i PATH="$PATH" HOME="$work/home" TERM=xterm-256color SHELL=/bin/sh \
		BMUX_RUNTIME_DIR="$work/run" BMUX_CONFIG="$work/config.toml" "$@"
}

run "$bmux" --help >"$out/help.txt" 2>&1 || true
if run "$bmux" default-config >"$work/default-config" 2>/dev/null; then
	cp "$work/default-config" "$out/default-config.toml"
fi

# Releases before v0.2.0 don't save sessions.
if [ "$(printf '%s\nv0.2.0\n' "$tag" | sort -V | head -n 1)" != v0.2.0 ]; then
	echo "$tag: help only (no saved sessions before v0.2.0)"
	exit 0
fi
cd "$work/proj"
tmux -L "fixtures-$$" new-session -d -x 160 -y 40 \
	"env -i PATH='$PATH' HOME='$work/home' TERM=xterm-256color SHELL=/bin/sh BMUX_RUNTIME_DIR='$work/run' BMUX_CONFIG='$work/config.toml' '$bmux' new work"
sleep 2
keys() { tmux -L "fixtures-$$" send-keys "$@"; sleep 0.7; }
keys C-a '|'
keys C-a -
keys "cd $work/proj/sub" Enter
if [ "$(printf '%s\nv0.4.0\n' "$tag" | sort -V | head -n 1)" = v0.4.0 ]; then
	keys C-a c # a second window, from v0.4.0
fi
sleep 6 # past the 5-second save interval
pid=$(cat "$work/run/server.pid")
kill -TERM "$pid" # a clean stop saves once more
for _ in $(seq 50); do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done
sed "s|$work/proj|@PROJ@|g; s|$work/home|@HOME@|g" "$work/run/sessions.json" >"$out/sessions.json"
echo "$tag: $(ls "$out" | tr '\n' ' ')"
