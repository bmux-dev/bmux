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
| `BMUX_RUNTIME_DIR` | Custom runtime directory; default follows the [runtime directory rules](/bmux/reference/runtime-files/) |
| `BMUX_NO_UPDATE_CHECK` | Non-empty value (`1`) disables background update checks (same as `[update] check = false`) |
| `BMUX_PPROF_DIR` | Directory where opted-in clients and servers create private `pprof` sockets; disabled by default. See [CPU and allocation profiles](/bmux/reference/troubleshooting/#cpu-and-allocation-profiles) |

Appearance variables affect bmux chrome—and, for named themes when `ui.theme-panes` is enabled, unstyled panes. Variables read by the server (such as `BMUX_RUNTIME_DIR`, `BMUX_CONFIG`, and `BMUX_PPROF_DIR`) take effect when that server starts.

```sh
BMUX_THEME=light BMUX_ACCENT=amber bmux
BMUX_ACCENT='#aabbcc' bmux
BMUX_POWERLINE_ASCII=1 bmux
```

Inside panes, bmux sets `BMUX_SESSION`, `BMUX_SESSION_ID` and `BMUX_PANE` for
[editor mappings](/bmux/guides/editor-navigation/) and [agent reports](/bmux/agents/reporting-state/).
These are pane information, not settings.
