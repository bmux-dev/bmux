---
title: "Vim and Neovim navigation"
description: "Cross Vim and Neovim splits into bmux panes and the sidebar with Ctrl-h/j/k/l."
sidebar:
  order: 8
---

## How the keys work

| Key | Direction |
| --- | --- |
| Ctrl-h | Left |
| Ctrl-j | Down |
| Ctrl-k | Up |
| Ctrl-l | Right |

Outside Vim/Neovim, bmux takes these keys to navigate panes. By default they
do not reach other programs, including fzf and your shell, so **Ctrl-l will
not clear the shell.** List programs that should get them too in the config
file, and unbind a key to give it back everywhere:

```toml
[keys]
pass-through = ["fzf", "lazygit"]

[keys.root]
"C-l" = "none"
```

When Vim or Neovim is the foreground application, bmux forwards the keys to the
editor. Editor split navigation needs mappings. To cross from an editor edge
into a bmux pane, the mapping must call `bmux focus DIRECTION`.

The mappings below first try an editor split. If focus did not move, they ask
bmux to move to a neighboring pane. They also work in tmux. Install one recipe
in your editor configuration; no plugin manager is needed.

**Ctrl-a arrows**, a **click**, and **Ctrl-a o** navigate bmux panes directly,
including while an editor is running. Moving to another pane while zoomed
unzooms first, like tmux.

## Neovim

Add this to `init.lua`. It uses `vim.system`, available in **Neovim 0.10+**,
and maps normal mode only. Keep `bmux` on the editor's `PATH`.

```lua
local function navigate(direction)
  local before = vim.fn.winnr()
  local key = ({ left = "h", down = "j", up = "k", right = "l" })[direction]
  pcall(vim.cmd, "silent! wincmd " .. key)
  if vim.fn.winnr() ~= before then
    return
  end
  if vim.env.BMUX_SESSION then
    vim.system({ "bmux", "focus", direction }, { detach = true })
  elseif vim.env.TMUX then
    local socket = vim.env.TMUX:match("^[^,]+")
    local flag = ({ left = "L", down = "D", up = "U", right = "R" })[direction]
    vim.system({ "tmux", "-S", socket, "select-pane", "-" .. flag }, { detach = true })
  end
end

for key, direction in pairs({ h = "left", j = "down", k = "up", l = "right" }) do
  vim.keymap.set("n", "<C-" .. key .. ">", function() navigate(direction) end,
    { silent = true, desc = "Navigate split / bmux pane " .. direction })
end
```

## Vim

Add this to `.vimrc`. It maps normal mode only and uses `system()` to call
bmux or tmux when the editor cannot move farther in that direction.

```vim
function! s:Navigate(direction) abort
  let before = winnr()
  let keys = {'left': 'h', 'down': 'j', 'up': 'k', 'right': 'l'}
  execute 'silent! wincmd ' . keys[a:direction]
  if winnr() != before
    return
  endif
  if !empty($BMUX_SESSION)
    call system('bmux focus ' . a:direction)
  elseif !empty($TMUX)
    let socket = split($TMUX, ',')[0]
    let flags = {'left': 'L', 'down': 'D', 'up': 'U', 'right': 'R'}
    call system('tmux -S ' . shellescape(socket) . ' select-pane -' . flags[a:direction])
  endif
endfunction

nnoremap <silent> <C-h> :call <SID>Navigate('left')<CR>
nnoremap <silent> <C-j> :call <SID>Navigate('down')<CR>
nnoremap <silent> <C-k> :call <SID>Navigate('up')<CR>
nnoremap <silent> <C-l> :call <SID>Navigate('right')<CR>
```

## Pane information

bmux sets these variables in each pane:

| Variable | Value |
| --- | --- |
| `BMUX_SESSION` | Session name when the pane started |
| `BMUX_SESSION_ID` | Session identifier; unchanged by renames |
| `BMUX_PANE` | Pane identifier |

`bmux focus left`, `right`, `up`, or `down` reads these variables to move from
the editor's pane. Run it inside a bmux pane. With no neighbor in that direction,
focus stays put, except that `left` from the leftmost pane moves into the
sidebar when it is shown. These variables identify the pane; they are not
configuration options to set yourself.

## Backspace and troubleshooting

- If Backspace moves focus, configure the terminal to send **DEL (`0x7f`)**.
  ASCII BS (`0x08`) is indistinguishable from Ctrl-h.
- If navigation stops at an editor edge, confirm the mapping is loaded and
  `bmux` is on the editor's `PATH`.
- To check the bmux side without a mapping, run `bmux focus right` in a pane
  with a neighbor on its right.
- Use Ctrl-a arrows or Ctrl-a o when you want to bypass editor mappings.
