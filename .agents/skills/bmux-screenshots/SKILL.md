---
name: bmux-screenshots
description: Renders crisp 2x Retina PNG screenshots and MP4/WebM demo videos directly from the live bmux binary using its built-in VT emulator and Pillow/ffmpeg. Use when regenerating docs screenshots in site/src/assets/screenshots/, recording hero videos, or verifying UI changes visually across dark and light themes.
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

Record the 2x Retina homepage demo video (`demo.mp4`, `demo.webm`, `demo-poster.png`) into `site/public/demo/`:

```bash
go run ./site/screenshots/capture -scene demo -theme dark
```

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

After regenerating screenshots, inspect at least one dark and one light PNG (plus any changed popup/overlay scene) directly before committing:
- Dark theme uses **One Dark** (`onedark`) and light theme uses **Catppuccin Latte** (`catppuccin-latte`)
- Text and box borders are crisp at 2x Retina (`20x40` cell grid, no subpixel blur)
- Light mode (`catppuccin-latte`) and dark mode (`onedark`) text, borders, and highlights have strong contrast
- Agent sidebar shows distinct states (`working`, `blocked`, `idle`) and git branch/worktree hierarchy
- Keybinding hints match current defaults (`Ctrl-a n`, `Ctrl-a s`, `Ctrl-a /`, `Ctrl-a ?`)
