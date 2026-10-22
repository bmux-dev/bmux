#!/usr/bin/env bash
# Renders the documentation screenshots into site/src/assets/screenshots,
# once with bmux's dark theme and once with its light theme, and the demo
# video (a scene whose tape mentions @VIDEO@, dark only) into
# site/public/demo. Every render uses a throwaway HOME, runtime directory and
# server, so your own sessions are never touched.
# Usage: render.sh [SCENE...]  (default: every scenes/*.tape)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
out="$repo/site/src/assets/screenshots"
video_out="$repo/site/public/demo"

is_video() { grep -q '@VIDEO@' "$here/scenes/$1.tape"; }

for tool in vhs ttyd ffmpeg go git; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "render.sh: $tool not found; install Go, and vhs (brew install vhs brings ttyd and ffmpeg)" >&2
    exit 1
  fi
done

scenes=()
if [[ $# -gt 0 ]]; then
  scenes=("$@")
else
  for f in "$here"/scenes/*.tape; do scenes+=("$(basename "$f" .tape)"); done
fi
for scene in "${scenes[@]}"; do
  if [[ ! -f "$here/scenes/$scene.tape" ]]; then
    echo "render.sh: no such scene: $here/scenes/$scene.tape" >&2
    exit 1
  fi
done

# Under /tmp so the server socket path stays below bmux's 100-byte limit.
work="$(cd "$(mktemp -d /tmp/bmux-shots.XXXXXX)" && pwd -P)" # resolved, so ~ paths in panes stay short
run=""

stop_server() { # stops the current render's server, if one is running
  [[ -n "$run" && -f "$run/server.pid" ]] || return 0
  local pid i
  pid="$(cat "$run/server.pid")"
  kill -TERM "$pid" 2>/dev/null || true
  for i in $(seq 1 50); do
    [[ -f "$run/server.pid" ]] || return 0
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
}
cleanup() { stop_server; rm -rf "$work"; }
trap cleanup EXIT

mkdir -p "$work/bin" "$work/home/projects" "$out" "$video_out"
(cd "$repo" && go build -o "$work/bin/bmux" ./cmd/bmux)
cp "$here/bin/fake-agent" "$here/bin/demo-shell" "$work/bin/"
chmod +x "$work/bin/fake-agent" "$work/bin/demo-shell"

cat >"$work/config.toml" <<'EOF'
onboarding = false

[projects]
roots = ["~/projects"]
EOF

make_repo() { # make_repo NAME MESSAGE...: a git repository with these empty commits, oldest first
  local dir="$work/home/projects/$1" msg
  shift
  mkdir -p "$dir"
  git -c init.defaultBranch=main -C "$dir" init -q
  for msg in "$@"; do
    git -C "$dir" -c user.name=bmux -c user.email=docs@bmux.invalid -c commit.gpgsign=false \
      commit -q --allow-empty -m "$msg"
  done
}
make_repo api "Initial commit" "Add session store" "Handle client reconnects" "Fix flaky restart test" "Add /health endpoint"
make_repo web "Initial commit" "Login form validation"
make_repo docs "Initial commit"
make_repo cli "Initial commit"
make_repo infra "Initial commit"

# api: two windows; "code" is an agent beside a stack of an agent and a shell.
cat >"$work/home/projects/api/.bmux.toml" <<'EOF'
[[windows]]
name = "code"
focused = 3
[windows.layout]
direction = "horizontal"
ratio = 5000
[windows.layout.first]
pane = 1
[windows.layout.second]
direction = "vertical"
[windows.layout.second.first]
pane = 2
[windows.layout.second.second]
pane = 3

[[windows]]
name = "server"
[windows.layout]
pane = 4

[[panes]]
id = 1
command = "fake-agent blocked claude"

[[panes]]
id = 2
command = "fake-agent working codex"

[[panes]]
id = 3
command = "git --no-pager log --oneline"

[[panes]]
id = 4
EOF

cat >"$work/home/projects/web/.bmux.toml" <<'EOF'
[[windows]]
name = "web"
[windows.layout]
pane = 1

[[panes]]
id = 1
command = "fake-agent idle opencode"
EOF

cat >"$work/home/projects/docs/.bmux.toml" <<'EOF'
[[windows]]
name = "logs"
[windows.layout]
pane = 1

[[panes]]
id = 1
command = "cat server.log"
EOF

cat >"$work/home/projects/docs/server.log" <<'EOF'
12:00:01 INFO  listening on :8080
12:00:02 INFO  connected to postgres (pool 10)
12:00:04 INFO  GET /api/users 200 12ms
12:00:05 INFO  GET /api/sessions 200 8ms
12:00:07 WARN  upstream timeout after 5s (GET /api/billing)
12:00:07 INFO  retrying GET /api/billing (1/3)
12:00:09 INFO  GET /api/billing 200 1840ms
12:00:12 INFO  POST /api/login 200 31ms
12:00:15 INFO  GET /api/users/42 200 9ms
12:00:18 WARN  slow query 812ms: SELECT * FROM sessions
12:00:21 INFO  GET /health 200 1ms
12:00:24 ERROR upstream timeout after 5s (POST /api/invoices)
12:00:24 INFO  retrying POST /api/invoices (1/3)
12:00:26 INFO  POST /api/invoices 201 1220ms
12:00:30 INFO  GET /api/sessions 200 7ms
12:00:33 INFO  GET /api/users 200 11ms
12:00:36 WARN  cache miss rate 41% over 60s
12:00:40 INFO  GET /health 200 1ms
12:00:44 ERROR upstream timeout after 5s (GET /api/billing)
12:00:44 INFO  circuit open for billing (30s)
12:00:47 INFO  GET /api/users 200 10ms
12:00:50 INFO  GET /health 200 1ms
EOF

wait_for_agents() { # until bmux agents lists the three fake agents
  local i list
  for i in $(seq 1 50); do
    list="$(bmux agents 2>/dev/null || true)"
    if [[ "$list" == *claude* && "$list" == *codex* && "$list" == *opencode* ]]; then
      sleep 1 # let cat and git finish drawing
      return 0
    fi
    sleep 0.2
  done
  echo "render.sh: agents did not report within 10s; see $BMUX_RUNTIME_DIR/server.log" >&2
  return 1
}

render() { # render SCENE THEME
  local scene="$1" theme="$2" vhs_theme
  case "$theme" in
    dark) vhs_theme="Catppuccin Mocha" ;;
    light) vhs_theme="Catppuccin Latte" ;;
  esac
  run="$work/run-$scene-$theme"
  mkdir -m 700 "$run"
  cat >"$run/env.sh" <<EOF
# Forget any bmux session this script was started from, and NO_COLOR, which
# would make bmux draw in reverse video instead of its theme colors.
for v in \$(compgen -e | grep '^BMUX_' || true); do unset "\$v"; done
unset NO_COLOR
export HOME="$work/home"
export PATH="$work/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export SHELL="$work/bin/demo-shell"
export BMUX_RUNTIME_DIR="$run"
export BMUX_CONFIG="$work/config.toml"
export BMUX_THEME="$theme"
export BMUX_SOUND=0
export USER=you
export TERM=xterm-256color
export COLORTERM=truecolor
export BASH_SILENCE_DEPRECATION_WARNING=1
EOF
  ( # The first command starts the server, which gives every pane this environment.
    source "$run/env.sh"
    cd "$HOME"
    for dir in docs web api; do
      bmux layout trust "$HOME/projects/$dir" >/dev/null
      bmux new --detach --dir "$HOME/projects/$dir" "$dir" >/dev/null
    done
    wait_for_agents
  )
  extra_outputs=""
  if is_video "$scene"; then
    extra_outputs=$'Output "'"$video_out/$scene.mp4"$'"\nOutput "'"$video_out/$scene.webm"$'"'
  fi
  {
    cat <<EOF
Output "$work/$scene-$theme.gif"
$extra_outputs
Set Shell "bash"
Set FontSize 16
Set Width 920
Set Height 700
Set Padding 16
Set WindowBar Colorful
Set Theme "$vhs_theme"
Set TypingSpeed 40ms
Set CursorBlink false
Hide
Type "exec bash --noprofile --norc"
Enter
Sleep 1s
Type "PS1='$ ' && source $run/env.sh && clear"
Enter
Sleep 500ms
EOF
    sed -e "s|@OUT@|$out/$scene-$theme.png|g" \
      -e "s|@VIDEO@|$video_out/$scene|g" -e "s|@POSTER@|$video_out/$scene-poster.png|g" \
      "$here/scenes/$scene.tape"
    echo "Sleep 1s" # vhs writes the screenshot asynchronously; let it finish
  } >"$work/$scene-$theme.tape"
  if is_video "$scene"; then
    rm -f "$video_out/$scene".{mp4,webm} "$video_out/$scene-poster.png"
  else
    rm -f "$out/$scene-$theme.png"
  fi
  vhs "$work/$scene-$theme.tape"
  if is_video "$scene"; then
    for f in "$video_out/$scene.mp4" "$video_out/$scene.webm" "$video_out/$scene-poster.png"; do
      if [[ ! -s "$f" ]]; then
        echo "render.sh: vhs wrote no $f" >&2
        return 1
      fi
    done
  elif [[ ! -s "$out/$scene-$theme.png" ]]; then
    echo "render.sh: vhs wrote no screenshot for $scene ($theme)" >&2
    return 1
  fi
  stop_server
  run=""
}

for scene in "${scenes[@]}"; do
  if is_video "$scene"; then
    echo "render.sh: $scene (video, dark)"
    render "$scene" dark
    continue
  fi
  for theme in dark light; do
    echo "render.sh: $scene ($theme)"
    render "$scene" "$theme"
  done
done
echo "render.sh: done: screenshots in $out, videos in $video_out"
