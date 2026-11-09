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

## Compact snapshot experiment

`BMUX_EXPERIMENT_FRAME_ENCODING=cell-palette-runs-v1` opts an attaching client
into experimental compact **full snapshots**. It is disabled by default; unset
the variable to compare ordinary transport. Other values leave it disabled.

```sh
BMUX_EXPERIMENT_FRAME_ENCODING=cell-palette-runs-v1 bmux attach SESSION
```

The client and server must both support the experiment. An older protocol-10
server does not acknowledge the capability, so the client automatically uses
ordinary snapshots. Older clients keep receiving ordinary snapshots from a
supporting server. Other protocol versions retain the usual mismatch error.
The setting is read on each attachment, not through the config file or reload.
It does not change previews, the 30 Hz snapshot cap, input handling or rendering.

Compact snapshots carry a per-pane dictionary of complete cells and indexed or
repeated-cell rows. They are lossless, independent of earlier frames, and are
**not incremental row updates**. High-entropy panes and panes outside the compact
expansion budget remain ordinary even on an opted-in connection. This experiment
reduces repeated cell serialization; it does not establish that transport causes
input lag or promise a latency improvement. Use isolated test servers for
comparisons; enabling it does not require restarting a production server.
