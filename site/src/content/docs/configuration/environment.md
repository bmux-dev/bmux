---
title: "Environment variables"
description: "Environment variables override the config file; set them before starting the client."
sidebar:
  order: 3
---

These override the config file. Set them before starting the client, or
export them in your shell startup file.

| Variable | Values and default |
| --- | --- |
| `BMUX_CONFIG` | Path of the config file |
| `BMUX_ACCENT` | Overrides `ui.accent` |
| `BMUX_THEME` | Overrides `theme`; takes the same names, such as `light` or `catppuccin-latte`. An unknown name is reported and uses `dark` |
| `BMUX_POWERLINE_ASCII` | `1` forces `<` and `>` separators |
| `BMUX_TERMINAL_PADDING` | `1` preserves physical-terminal padding |
| `BMUX_SOUND` | `0` turns off agent alert sounds |
| `BMUX_RUNTIME_DIR` | Custom runtime directory; default follows the rules below |

Appearance settings affect bmux's bars and highlights, not application colors.

```sh
BMUX_THEME=light BMUX_ACCENT=amber bmux
BMUX_ACCENT='#aabbcc' bmux
BMUX_POWERLINE_ASCII=1 bmux
```

Inside panes, bmux sets `BMUX_SESSION`, `BMUX_SESSION_ID` and `BMUX_PANE` for
[editor mappings](/bmux/guides/editor-navigation/) and [agent reports](/bmux/agents/reporting-state/).
These are pane information, not settings.
