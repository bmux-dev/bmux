---
name: bmux-screenshots
description: Renders crisp 2x Retina PNG screenshots and real live-agent MP4/WebM/WebP demo videos directly from the live bmux binary using its built-in VT emulator and Pillow/ffmpeg. Use when regenerating docs screenshots in site/src/assets/screenshots/, recording homepage/README hero videos with real coding agents, or verifying UI and SVG renders visually across dark and light themes.
---

# bmux-screenshots

Captures pixel-sharp 2x Retina screenshots and videos of `bmux` without relying on `vhs` or `ttyd`. A Go PTY driver (`site/screenshots/capture/main.go`) runs the real `bmux` binary inside isolated temporary repositories, feeds its output through `internal/vt`, and hands packed terminal cell frames to `.agents/skills/bmux-screenshots/scripts/render_media.py` (Pillow + `ffmpeg`).

## Prerequisites

- `go` (builds `./cmd/bmux` from source)
- `python3` with `Pillow` (`PIL`)
- `ffmpeg` (only required for `-scene demo` MP4/WebM rendering)
- Monospace font installed (prefers `AdwaitaMono` + `SymbolsNerdFontMono`, falls back to system monospace fonts)

## Quick Commands

Regenerate all 10 documentation screenshots (`dark` and `light` across all 5 scenes) into `site/src/assets/screenshots/`:

```bash
go run ./site/screenshots/capture -scene all -theme both
```

Regenerate a single scene and theme (fast iteration):

```bash
go run ./site/screenshots/capture -scene project-picker -theme light
```

Record the scripted fallback demo video (for smoke-testing layout only; do **not** ship static `fake-agent` recordings as the homepage/README demo):

```bash
go run ./site/screenshots/capture -scene demo -theme dark
```

## Live Coding Agent Demo Recording (Homepage & README)

For the public homepage and README demo (`site/public/demo/demo.{mp4,webm,webp}` and `demo-poster.png`), record **real coding agents** (`claude`, `codex`, `opencode`) running inside an isolated `bmux` server rather than static `fake-agent` scripts:

1. **Isolate server & hooks completely** (never touch or kill the user's running `bmux` server):
   - Build `./cmd/bmux` into a temp dir (e.g., `/tmp/bmux-live-demo/bin/bmux`) and prepend that directory to `PATH` so agent hooks (`bmux integration hook ...`) talk only to the demo binary.
   - Set isolated `BMUX_RUNTIME_DIR` and `BMUX_CONFIG`, create throwaway git repos/worktrees under `/tmp/projects/`, and `kill-server` + `rm -rf` temp dirs on exit.
2. **Agent CLI & prompt gotchas:**
   - **Claude Code:** run `claude --model haiku --permission-mode default` (never `--no-session-persistence`, which only works with `--print`; never `exec claude`, or declining trust closes the pane). On the folder-trust prompt, `No, exit` is preselected—send `--key down --key enter`.
   - **Codex:** run `codex -c check_for_update_on_startup=false`; send `--key escape` if a hook-review or skip banner appears.
   - **OpenCode:** run `opencode --standalone`.
   - **State polling:** `bmux agents --json` outputs a single JSON array (`[{...}]`), not NDJSON lines.
   - **Cleanup:** Delete any temporary live-capture harness before committing.
3. **Render video, poster, and looping README WebP:**
   ```bash
   python3 .agents/skills/bmux-screenshots/scripts/render_media.py \
     --frames /tmp/live-demo.bin --mode video --theme dark --out-dir site/public/demo

   # Looping animated WebP for README (15 fps, 1664px wide; stay <= 2.5 MB)
   ffmpeg -y -i site/public/demo/demo.mp4 \
     -vf "fps=15,scale=1664:-2:flags=lanczos" \
     -c:v libwebp_anim -lossless 1 -compression_level 6 -q:v 75 -loop 0 \
     site/public/demo/demo.webp
   # If > 2.5 MB, re-encode with: -lossless 0 -compression_level 6 -q:v 85 -preset drawing -loop 0
   ```
   Verify `is_animated == True` and `loop == 0` with Pillow, and update `chapters` timestamps in `site/src/components/Demo.astro`.

Available scenes:
- `workspace` (`104x32`): multi-pane workspace with live sidebar and agent states
- `session-picker` (`104x32`): `Ctrl-a s` session switcher filtered with `/api`
- `project-picker` (`104x32`): `Ctrl-a n` two-tab New Session / Projects popup
- `scrollback-search` (`104x32`): `Ctrl-a /` live scrollback search with match highlight
- `key-help` (`104x32`): `Ctrl-a ?` keybindings overlay
- `demo` (`120x34`, 30 fps): scripted walkthrough outputting video + poster

## Direct Renderer Usage

`scripts/render_media.py` consumes the packed frame stream (`<HHH` header `cols, rows, fps`; per-frame `<hh` cursor + `cols*rows` cells of `<IIIBB`):

```bash
python3 .agents/skills/bmux-screenshots/scripts/render_media.py \
  --frames /path/to/frames.bin \
  --mode png \
  --theme dark \
  --out site/src/assets/screenshots/workspace-dark.png
```

Use `--mode video --out-dir site/public/demo` to encode `demo.mp4`, `demo.webm`, and `demo-poster.png`.

## Verification Checklist

After regenerating screenshots, demos, or rendered SVG/social-card graphics, inspect the rendered PNGs directly with `read_file` before committing:
- Dark theme uses **One Dark** (`onedark`) and light theme uses **Catppuccin Latte** (`catppuccin-latte`)
- Text and box borders are crisp at 2x Retina (`20x40` cell grid, no subpixel blur)
- Light mode (`catppuccin-latte`) and dark mode (`onedark`) text, borders, and highlights have strong contrast
- Agent sidebar shows distinct states (`working`, `blocked`, `idle`) and git branch/worktree hierarchy
- Keybinding hints match current defaults (`Ctrl-a n`, `Ctrl-a s`, `Ctrl-a /`, `Ctrl-a ?`)
- **Live demo motion:** extract frames from `demo.mp4` (including two frames ~0.4s apart during active agent work) and verify real spinner/timer/token progression and chapter transitions
- **Rendered SVGs:** verify rasterized output has populated terminal content (no blank panels or collapsed `<tspan>` spacing)
- **Privacy:** confirm zero leaked emails, API keys, auth tokens, cloud account IDs, or private repo paths in any frame
