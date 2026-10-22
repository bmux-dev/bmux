---
title: "Configuration file"
description: "bmux reads config.toml from ~/.config/bmux; every setting is optional and has a default."
sidebar:
  order: 1
---

bmux reads `~/.config/bmux/config.toml` (or `$XDG_CONFIG_HOME/bmux/config.toml`,
or the path in `BMUX_CONFIG`). It's TOML laid out like Helix's `config.toml`: a
theme and the prefix at the top, then one table per area with kebab-case keys.
The file and every line in it are optional; write only what you change.
`bmux default-config` prints every setting, commented out, with its default.

You don't have to edit the file by hand for the common settings: **Ctrl-a ,**
opens a settings page with one tab per area (General, Appearance, Sidebar,
Sound, Agents, Projects, Sessions). Move with **←/→** (or **h/l**) and
**j/k**. **Enter** or **Space** flips a switch or cycles a choice; on a number,
path, list or duration it opens an editor (**Enter** saves, **Esc** cancels).
On the accent color, **e** types a `#RRGGBB` value. Each change is written to
the config file at once and applied without a restart. Comments and the other
lines are kept, and a value the file's rules reject isn't saved; the page says
why. Keybindings, per-agent resume commands and `onboarding` stay in the file.
The page shows what the file says, so a `BMUX_*` environment variable can
still override it.

The defaults look like this:

```toml
theme = "dark"
prefix = "C-a"

[ui]
accent = "amber"

[sidebar]
width = 24
```

| Setting | Default | What it does |
| --- | --- | --- |
| `theme` | `"dark"` | Colors for bmux's chrome: status bars, sidebar, window tabs, pickers, dividers and selection. `dark`, `light`, or a Neovim colorscheme: [see the list below](#themes) |
| `prefix` | `"C-a"` | The command key, written like Helix keys: `C-a` to `C-z`, except `C-h/j/k/l` (navigation) and `C-i`/`C-m` (Tab and Enter). `ctrl-a` also works. Status-line help follows it. |
| `onboarding` | `true` | Show the welcome screen on the next attach; bmux sets `false` once it has been seen |
| `ui.accent` | `"theme"` | `theme` uses the theme's own accent; `cyan`, `amber`, `green` or `"#RRGGBB"` override it |
| `ui.powerline-ascii` | `false` | Plain `<` `>` separators instead of powerline glyphs |
| `ui.terminal-padding` | `false` | Keep physical-terminal padding; read when a client starts |
| `ui.set-title` | `true` | Name the outer terminal's window after the session and, when its app set one, the focused pane's title (`api · nvim README.md`); `bmux` in the picker. Like tmux's `set-titles`; your terminal's own title comes back when bmux exits. Turning it off stops updates; the current title stays until then. |
| `ui.theme-panes` | `true` | Draw panes in a named theme's background and foreground wherever the program sets none, such as a plain shell prompt; `false` keeps your terminal's colors. `dark` and `light` never recolor panes. See [Themes](#themes). |
| `ui.pane-background` | `"theme"` | `"terminal"` keeps your terminal's own background in panes and blank space, with the theme's text colors, for a terminal background you like better than the theme's. Chrome (bars, sidebar, pickers) keeps the theme's. |
| `ui.graphics` | `"auto"` | Show the images programs draw in panes. `"auto"` shows them when the terminal answers a Kitty graphics query at startup, `"on"` assumes it does, `"off"` draws blanks with an `[image W×H]` marker. Read when a client starts. See [images](/bmux/reference/troubleshooting/#images-in-panes). |
| `ui.agent-colors.working` / `waiting` / `blocked` / `idle` | herdr's palette | Sidebar state colors as `"#RRGGBB"` |
| `sidebar.show` | `true` | Show the sidebar when a client starts; Ctrl-a b still toggles it |
| `sidebar.width` | `24` | Columns including its border, 12 to 60 |
| `sidebar.min-screen-width` | `80` | Hide the sidebar on narrower terminals |
| `sound.enabled` | `true` | Alert sounds for background agents |
| `sound.ready` / `sound.blocked` | system sounds | Sound files (absolute or `~/` paths), played with `afplay` on macOS or `pw-play`/`paplay` on Linux (`aplay` for WAV files) |
| `agents.detect` | `true` | Recognize agents in panes; reported agents still show when off |
| `agents.extra` | `[]` | More program names to recognize as agents |
| `agents.working-after` / `agents.waiting-after` | `"2s"` / `"2s"` | How long an agent must draw before it counts as working, and stay quiet before it counts as waiting (1s to 1m) |
| `restore.enabled` | `true` | Save and restore sessions. When false, nothing is restored or saved and the existing save file is left alone. |
| `restore.resume.AGENT` | built-in | Replace an agent's resume command, e.g. `claude = ["claude", "--continue", "--model", "opus"]`; `[]` restores it as a plain shell |
| `projects.roots` | `["~/code", "~/src", "~/projects", "~/dev"]` | Folders the [new-session popup](/bmux/guides/project-picker/) lists for git repositories; absolute or `~/` paths. Missing ones are skipped. |
| `projects.depth` | `3` | How many levels below each root to look (1 to 6) |
| `projects.start-dir` | `""` | Folder the new-session popup's Create tab starts in, and what its relative paths are relative to, e.g. `"~"`; absolute or `~/` path. Empty means the directory bmux was started in. |
| `scrollback.lines` | `10000` | History kept per pane, 100 to 200,000; `bmux reload` applies it to running panes |
| `update.check` | `true` | Look for a new release once a day and show `↑` in the status bar; see [Updates](/bmux/start/installation/#updates) |
| `hooks.EVENT` | none | Shell commands to run when an event happens; see [Hooks](#hooks) |
| `keys.root.KEY` / `keys.prefix.KEY` | the keys above | Bind a key to an action, or `"none"`; see [Rebinding keys](/bmux/configuration/rebinding-keys/) |
| `keys.pass-through` | `[]` | Programs that get Ctrl-h/j/k/l themselves, like Vim and Neovim do, e.g. `["fzf", "lazygit"]` |

**Ctrl-a r** reloads the file in your client and the server; `bmux reload` does
the same from a shell, and, like Helix, so does sending the server `SIGUSR1`
(`pkill -USR1 -f 'bmux --serve'`). Every attached client follows. If a setting
is invalid, the status line (or `bmux reload`) says which, and that setting
keeps its default, so a typo never leaves you without keys. Unknown settings
are reported too.

## Themes

`theme` colors bmux's own chrome with a colorscheme's published palette:
foreground, background, muted text, status bars, accent, selection, and the
working, waiting, blocked and idle agent colors.

:::note
A theme works like a terminal theme: it supplies the default text and background
colors, and nothing a program chose. With a named theme, panes use its
foreground and background wherever a program sets none, so a plain shell matches
the status bars and sidebar. Colors a program sets itself are left alone: Neovim's
colorscheme, a CLI's own theme, `ls --color`, and ANSI palette colors, which
still come from your terminal (set it to the same colorscheme for those to
match).

Programs that ask the terminal for its colors (OSC 10 and 11, which Neovim and
omp use to pick a dark or light theme) are told the colors panes really show: the
theme's, or your terminal's own where bmux leaves them alone.

`ui.theme-panes = false` keeps your terminal's colors in panes, and `dark` and
`light` always do.
:::

| Scheme | `theme` values |
| --- | --- |
| bmux | `dark` (default), `light` |
| Catppuccin | `catppuccin-mocha`, `catppuccin-macchiato`, `catppuccin-frappe`, `catppuccin-latte` |
| Tokyo Night | `tokyonight-night`, `tokyonight-storm`, `tokyonight-moon`, `tokyonight-day` |
| Gruvbox | `gruvbox-dark`, `gruvbox-light` |
| Nord | `nord` |
| Rosé Pine | `rose-pine`, `rose-pine-moon`, `rose-pine-dawn` |
| Kanagawa | `kanagawa-wave`, `kanagawa-dragon`, `kanagawa-lotus` |
| One Dark | `onedark` |
| Dracula | `dracula` |
| Everforest | `everforest-dark`, `everforest-light` |
| Solarized | `solarized-dark`, `solarized-light` |

A scheme name without a variant picks its default: `catppuccin` is
`catppuccin-mocha`, `tokyonight` is `tokyonight-night`, `gruvbox` is
`gruvbox-dark`, `kanagawa` is `kanagawa-wave`, `everforest` is
`everforest-dark` and `solarized` is `solarized-dark`.

`ui.accent` and `ui.agent-colors` override the theme's colors when set. The
default accent, `theme`, keeps the theme's own; `cyan`, `amber` and `green`
pick a fixed shade suited to dark or light themes. An unknown theme name is
reported in the status line and falls back to `dark`. The `BMUX_THEME`
environment variable takes the same names.

### Switching themes from a script or Neovim

`bmux theme NAME` saves the theme to `config.toml` and reloads the server, so
attached clients change at once; the rest of the file is left as it is. Run it
without a name to print the current theme, or with `--list` to print every
name. An unknown name is an error and changes nothing. If `BMUX_THEME` is set
it still overrides the file, and `bmux theme` warns about it.

To follow Neovim's colorscheme, add an autocmd. This runs only inside bmux, and
Neovim's name for most schemes (`catppuccin-latte`, `tokyonight-storm`,
`rose-pine-moon`, `kanagawa-dragon`, ...) is already a bmux theme name:

```lua
vim.api.nvim_create_autocmd("ColorScheme", {
  callback = function(args)
    if not vim.env.BMUX_SESSION then return end
    local name = args.match
    -- gruvbox has one colorscheme name; its variant is the background.
    if name == "gruvbox" then name = "gruvbox-" .. vim.o.background end
    vim.system({ "bmux", "theme", name }) -- needs Neovim 0.10+
  end,
})
```

A colorscheme bmux has no theme for, such as `habamax`, makes `bmux theme`
fail without changing anything.

## Hooks

`[hooks]` runs shell commands when something happens, like tmux's `set-hook`:

```toml
[hooks]
agent-blocked = ["notify-send 'bmux' '#{session_name}: #{pane_agent} needs you'"]
session-created = ["bmux split -h -d"]
pane-exited = ["echo '#{pane_target} exited' >> ~/.local/state/bmux/exits.log"]
```

| Event | When | Its pane |
| --- | --- | --- |
| `session-created` | A session is created (not when one is restored) | Its first pane |
| `session-closed` | Its last pane's shell exits | That pane: session and pane number only |
| `window-created` | A window opens (Ctrl-a c, `bmux new-window`) | The window's pane |
| `pane-exited` | A pane's shell exits, or `bmux kill-pane` closes it | That pane: session, window and pane number only |
| `client-attached`, `client-detached` | A client attaches to or leaves the session | Its focused pane |
| `agent-blocked` | A pane's agent starts waiting on a decision | That pane |
| `agent-idle` | A pane's agent finishes a turn | That pane |

Each command runs with `/bin/sh -c` after its [`#{...}` variables](/bmux/reference/commands/#format-variables)
are filled in for the event's pane. It runs in that pane's directory, with
`BMUX_SESSION`, `BMUX_SESSION_ID` and `BMUX_PANE` set for it and `BMUX_HOOK` set
to the event, so `bmux` commands in a hook act on that pane. Hooks run in the
background and never hold anything up: each gets one minute, at most eight run
at once (more are skipped), and their output and failures go to `server.log`.
Stopping the server (`bmux restart`) doesn't count as panes exiting. A hook
that creates sessions on `session-created` would create them forever.
