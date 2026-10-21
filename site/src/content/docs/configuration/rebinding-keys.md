---
title: "Rebinding keys"
description: "Bind or unbind any key in [keys.root] and [keys.prefix] in config.toml."
sidebar:
  order: 2
---

`[keys.root]` holds keys that work without the prefix; `[keys.prefix]` holds
the keys you press after it. Write only the keys you change; the rest keep
their defaults (`bmux default-config` lists them). `"none"` unbinds a key:
a root key then reaches the application, and a prefix key does nothing.

```toml
[keys.root]
"C-l" = "none"            # Ctrl-l clears the shell; Ctrl-a arrows still move right
"A-h" = "navigate-left"   # Alt-h/j/k/l as well as Ctrl-h/j/k/l

[keys.prefix]
"v" = "split-right"
"s" = "split-down"
"w" = "sessions"
"H" = "resize-left"       # repeats with plain H, like h does by default
```

Keys are written like Helix's: `C-` Ctrl, `A-` Alt, `S-` Shift, then a
character or one of `left`, `right`, `up`, `down`, `home`, `end`, `pageup`,
`pagedown`, `ins`, `del`, `backspace`, `tab`, `ret`, `esc`, `space`, `minus`
(or `"-"`), `F1` to `F12`. Uppercase letters are shifted keys; Ctrl-Shift
letters are the same as Ctrl letters in most terminals.

| Action | Default key | What it does |
| --- | --- | --- |
| `navigate-left` / `-down` / `-up` / `-right` | root `C-h/j/k/l` | Focus a pane, or move between Vim/Neovim splits |
| `focus-left` / `-down` / `-up` / `-right` | prefix arrows | Focus a pane, even with an editor in front |
| `next-pane` | prefix `o` | Cycle panes |
| `resize-left` / `-down` / `-up` / `-right` | prefix `h/j/k/l` | Move a divider five cells; prefix keys bound to resize repeat without the prefix for a second |
| `split-right` / `split-down` | prefix `\|` / `-` | Split side by side / stacked |
| `zoom`, `close-pane` | prefix `m`, `x` | |
| `last-pane`, `swap-pane-previous` / `swap-pane-next`, `rotate-window` | prefix `;`, `{` / `}`, `C-o` | |
| `break-pane`, `join-pane` | prefix `!`, `@` | Join goes into the last window, beside its focused pane |
| `next-layout`, `equalize-layout` | prefix `space`, `E` | |
| `layout-even-horizontal`, `layout-even-vertical`, `layout-main-horizontal`, `layout-main-vertical`, `layout-tiled` | prefix `A-1` … `A-5` | |
| `new-window`, `close-window`, `rename-window` | prefix `c`, `&`, `,` | New window in the focused pane's directory; close asks first if a program is running |
| `next-window` / `previous-window`, `last-window` | prefix `]` / `[`, `tab` | |
| `select-window-1` … `select-window-9` | prefix `1` … `9` | |
| `sessions`, `new-session`, `last-session` | prefix `s`, `n`, `p` | `new-session` opens the [popup](/bmux/guides/project-picker/) that finds a project's session or creates one |
| `previous-session` / `next-session` | prefix `(` / `)` | |
| `detach`, `copy`, `scrollback`, `search-scrollback` | prefix `d`, `y`, `v`, `/` | `scrollback` opens the [keyboard scrollback](/bmux/guides/selection-scrollback/#keyboard-scrollback) view; `search-scrollback` opens it with a search prompt |
| `scroll-up` / `scroll-down` | root `S-pageup` / `S-pagedown` | Ten lines of history |
| `toggle-sidebar`, `reload-config`, `help`, `integrations` | prefix `b`, `r`, `?`, `i` | `help` lists the bindings in effect; `integrations` opens the integrations panel |
| `send-prefix` | the prefix again | Send the prefix key to the application |

The prefix can't be a root key. The status line's hints and the prefix help
show your bindings.
