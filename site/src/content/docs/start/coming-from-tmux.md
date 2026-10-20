---
title: "Coming from tmux"
description: "Common tmux-plugin workflows built into bmux, with no plugin manager."
sidebar:
  order: 2
---

Everything below would take a plugin (or a separate app) with tmux. In bmux it
works out of the box, with no plugin manager.

| Instead of | Built into bmux |
| --- | --- |
| tmux-sessionx, tmux-fzf (sessions) | List-first session picker with fuzzy filtering and a live preview of the running terminal |
| tmux-sessionist | Create (Ctrl-a n), rename and terminate sessions from the picker, toggle to the last session (Ctrl-a p), step to the previous or next one (Ctrl-a ( / )); bare `bmux` opens a session named after the current directory |
| tmux-resurrect + tmux-continuum | Sessions, windows, splits, focus and each pane's directory saved every 5 seconds and on shutdown, restored automatically with agents reopening their conversations; [`bmux restart`](/bmux/guides/restarts/) for upgrades |
| sesh, tmux-sessionizer | [Project picker](/bmux/guides/project-picker/) (Ctrl-a f, `bmux project`): zoxide's folders and your git repositories, landing in each folder's session or creating it |
| tmuxinator, tmuxp | [Layout files](/bmux/guides/layout-files/): a `.bmux.toml` declares a folder's windows, splits and pane commands, which run once you trust the file; `bmux layout save` writes one |
| vim-tmux-navigator | Vim/Neovim-aware [Ctrl-h/j/k/l](/bmux/guides/editor-navigation/) across bmux panes and into the sidebar; editor edges cross with one small mapping |
| tmux-pain-control | Ctrl-a \| and - splits in the focused pane's directory, Ctrl-a x close (asks first if a program is running), Ctrl-h/j/k/l focus, Ctrl-a h/j/k/l resize that repeats with plain h/j/k/l, Ctrl-a m zoom |
| tmux-yank | Drag, double-click or triple-click to select; copied on release with pbcopy, wl-copy, xclip, xsel, or OSC 52 over SSH. Apps' own OSC 52 copies (Neovim yanks) go to the same clipboard |
| tmux-better-mouse-mode | Click to focus, drag dividers to resize, wheel scrolls history without copy mode, hold Alt to select or scroll in mouse-aware apps |
| tmux-copycat, copy mode | An optional [scrollback view](/bmux/guides/selection-scrollback/#keyboard-scrollback) (Ctrl-a v) with vi keys, search (Ctrl-a /), and jumps between shell prompts; the mouse never needs it |
| tmux-notify | A sound when an agent you aren't watching finishes or blocks; bells and OSC 9/777 notifications mark their pane in the sidebar |
| tmux-prefix-highlight | The status line lists the prefix keys while Ctrl-a is pending and shows when repeat resizing is active |
| tmux-powerline, tmux themes | Powerline status bars, accent colors and a light theme |
| herdr (separate app) | [Agent sidebar](/bmux/agents/sidebar/): every coding agent across sessions with working, waiting and blocked state |

The picker starts as a list: browse first, press **/** to search. See what's
running before attaching. Mouse selection and scrolling work without entering
copy mode. Split layouts and running shells stay available when you detach.

For tmux or Zellij users, bmux focuses on list-first session switching and
modeless mouse selection.

**Coming from tmux:** the prefix is **Ctrl-a**; set `prefix = "C-b"` in the
[config file](/bmux/configuration/config-file/) for tmux's. Splits use **|** and **-**, as in many
tmux configs. Every key can be [rebound](/bmux/configuration/rebinding-keys/),
including giving Ctrl-l back to the shell.
