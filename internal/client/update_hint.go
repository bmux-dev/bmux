package client

import "github.com/josephferrero/bmux/internal/protocol"

// The update hint sits at the right end of the bottom bar: ↑ when a newer
// release is out, ↻ once bmux update installed it and the server still runs
// the old binary. It only uses space the footer leaves free.

func (u *ui) updateHint() *protocol.UpdateHint {
	if u.picker || u.workspace == nil {
		return nil
	}
	return u.workspace.Update
}

func updateHintLabels(h *protocol.UpdateHint) (long, short string) {
	mark := "↑"
	if h.Installed {
		mark = "↻"
	}
	return " " + mark + " " + h.Version + " ", " " + mark + " "
}

// updateHintMessage says what to do about the hint, for clicks and Ctrl-a ?.
func updateHintMessage(h *protocol.UpdateHint) string {
	if h.Installed {
		return "bmux " + h.Version + " is installed: run bmux restart to switch (programs running in panes end, sessions come back)"
	}
	return "bmux " + h.Version + " is available: run bmux update"
}

// drawUpdateHint draws the hint after a footer of footerWidth columns,
// dropping the version, then the hint, when it doesn't fit.
func (u *ui) drawUpdateHint(w, h, footerWidth int) {
	u.updateHintFrom = 0
	hint := u.updateHint()
	if hint == nil || h < 2 {
		return
	}
	long, short := updateHintLabels(hint)
	label := long
	if free := w - footerWidth; free < displayWidth(long)+1 {
		label = short
		if free < displayWidth(short)+1 {
			return
		}
	}
	x := w - displayWidth(label)
	u.screen.PutStrStyled(x, h-1, label, barStyle.Foreground(accentColor).Bold(true))
	u.updateHintFrom = x
}
