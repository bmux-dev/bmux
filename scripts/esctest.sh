#!/usr/bin/env bash
# Runs esctest, a suite of escape-sequence conformance tests, inside a bmux
# pane, so it tests bmux's emulator. Prints the summary line and writes the
# full log, with each failure's details, to esctest.log in the current
# directory. Runs take about two minutes.
#
# Usage: scripts/esctest.sh [REGEX]   (needs go, git, python3 and tmux)
# REGEX picks tests by name, for example 'CUUTests|EDTests'; the default is all.
set -euo pipefail
include=${1:-.*}
esctest_commit=2798f12149a19c3295e9b4853ab2da4b2eff1b2b
root=$(cd "$(dirname "$0")/.." && pwd)
log=$PWD/esctest.log
work=$(mktemp -d)
cleanup() {
	tmux -L "esctest-$$" kill-server 2>/dev/null || true
	for p in $(pgrep -f "$work/bmux --serve" || true); do kill "$p" 2>/dev/null || true; done
	# The server saves its sessions before it exits.
	for _ in $(seq 1 50); do
		pgrep -f "$work/bmux --serve" >/dev/null || break
		sleep 0.1
	done
	rm -rf "$work"
}
trap cleanup EXIT

git clone -q https://github.com/ThomasDickey/esctest2 "$work/esctest2"
git -C "$work/esctest2" checkout -q "$esctest_commit"
(cd "$root" && go build -o "$work/bmux" ./cmd/bmux)
mkdir -m 700 "$work/home" "$work/run"
printf 'onboarding = false\n[sidebar]\nshow = false\n' >"$work/config.toml"
run() {
	env -i PATH="$PATH" HOME="$work/home" TERM=xterm-256color SHELL=/bin/sh \
		BMUX_RUNTIME_DIR="$work/run" BMUX_CONFIG="$work/config.toml" "$@"
}

rm -f "$log"
run "$work/bmux" new --detach --dir "$work/esctest2/esctest" esctest >/dev/null
run tmux -L "esctest-$$" -f /dev/null new-session -d -x 120 -y 40 "$work/bmux attach esctest"
sleep 1
# esctest runs in the pane's shell and talks to the pane's terminal.
run "$work/bmux" send esctest:1 --text "python3 esctest.py --expected-terminal=xterm \
--max-vt-level=4 --timeout=1 --logfile='$log' --include='$include'; touch '$work/done'" --key Enter
for _ in $(seq 1 600); do
	[ -f "$work/done" ] && break
	sleep 1
done
[ -f "$work/done" ] || { echo "esctest didn't finish in 10 minutes" >&2; exit 1; }
grep -E 'tests passed|TESTS FAILED' "$log" | tail -n 1
echo "details: $log"
