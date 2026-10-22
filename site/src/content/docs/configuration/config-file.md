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
accent = "cyan"

[sidebar]
width = 24
```

| Setting | Default | What it does |
| --- | --- | --- |
| `theme` | `"dark"` | `"dark"` or `"light"` |
| `prefix` | `"C-a"` | The command key, written like Helix keys: `C-a` to `C-z`, except `C-h/j/k/l` (navigation) and `C-i`/`C-m` (Tab and Enter). `ctrl-a` also works. Status-line help follows it. |
| `onboarding` | `true` | Show the welcome screen on the next attach; bmux sets `false` once it has been seen |
| `ui.accent` | `"cyan"` | `cyan`, `amber`, `green` or `"#RRGGBB"` |
| `ui.powerline-ascii` | `false` | Plain `<` `>` separators instead of powerline glyphs |
| `ui.terminal-padding` | `false` | Keep physical-terminal padding; read when a client starts |
| `ui.set-title` | `true` | Name the outer terminal's window after the session and, when its app set one, the focused pane's title (`api · nvim README.md`); `bmux` in the picker. Like tmux's `set-titles`; your terminal's own title comes back when bmux exits. Turning it off stops updates; the current title stays until then. |
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
| `keys.root.KEY` / `keys.prefix.KEY` | the keys above | Bind a key to an action, or `"none"`; see [Rebinding keys](/bmux/configuration/rebinding-keys/) |
| `keys.pass-through` | `[]` | Programs that get Ctrl-h/j/k/l themselves, like Vim and Neovim do, e.g. `["fzf", "lazygit"]` |

**Ctrl-a r** reloads the file in your client and the server; `bmux reload` does
the same from a shell, and, like Helix, so does sending the server `SIGUSR1`
(`pkill -USR1 -f 'bmux --serve'`). Every attached client follows. If a setting
is invalid, the status line (or `bmux reload`) says which, and that setting
keeps its default, so a typo never leaves you without keys. Unknown settings
are reported too.
