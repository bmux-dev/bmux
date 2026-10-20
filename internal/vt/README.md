# internal/vt

A copy of [charmbracelet/x/vt](https://github.com/charmbracelet/x/tree/main/vt),
the terminal emulator behind every bmux pane, taken at commit
`faa4adf95555` (pseudo-version `v0.0.0-20261008172826-faa4adf95555`) under
its MIT license (`LICENSE`).

bmux carries its own copy because reflow needs the emulator to remember which
line breaks were soft wraps, which upstream doesn't track. Changes from
upstream, each marked `bmux:` in the source:

- Lines record whether they continue onto the next (soft wrap), on screen and
- Lines record whether they continue onto the next (soft wrap) and whether a
  shell prompt starts on them (`MarkPrompt`, for OSC 133), on screen and in
  scrollback.
- Resizing the main screen's width rewraps wrapped lines, keeping the cursor
  on the same character.
Pull upstream fixes in by hand: diff the files against a newer upstream
commit and keep the `bmux:` changes.
