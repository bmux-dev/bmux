// Package format expands tmux-style #{name} format strings with a pane's
// variables, for bmux list-panes, list-windows and display-message.
package format

import (
	"strconv"
	"strings"

	"github.com/josephferrero/bmux/internal/protocol"
)

// Names lists every variable, in the order the docs give them.
var Names = []string{
	"session_name", "session_id", "session_path", "session_windows", "session_attached",
	"window_index", "window_name", "window_active", "window_zoomed", "window_panes",
	"pane_id", "pane_index", "pane_target", "pane_active",
	"pane_current_path", "pane_current_command", "pane_title", "pane_width", "pane_height",
	"pane_agent", "pane_agent_state",
}

// Vars are info's format variables. Booleans are "1" or "0".
func Vars(info protocol.PaneInfo) map[string]string {
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	itoa := strconv.Itoa
	return map[string]string{
		"session_name":         info.Session,
		"session_id":           strconv.FormatUint(info.SessionID, 10),
		"session_path":         info.SessionPath,
		"session_windows":      itoa(info.SessionWindows),
		"session_attached":     itoa(info.SessionAttached),
		"window_index":         itoa(info.Window),
		"window_name":          info.WindowName,
		"window_active":        flag(info.WindowActive),
		"window_zoomed":        flag(info.WindowZoomed),
		"window_panes":         itoa(info.WindowPanes),
		"pane_id":              itoa(info.Pane),
		"pane_index":           itoa(info.Index),
		"pane_target":          info.Session + ":" + itoa(info.Pane),
		"pane_active":          flag(info.Active),
		"pane_current_path":    info.Path,
		"pane_current_command": info.Command,
		"pane_title":           info.Title,
		"pane_width":           itoa(info.Width),
		"pane_height":          itoa(info.Height),
		"pane_agent":           info.Agent,
		"pane_agent_state":     info.State,
	}
}

// Expand replaces each #{name} in text with vars[name], or nothing for an
// unknown name; ## is a literal #. Anything else, including an unterminated
// #{, is kept as written.
func Expand(text string, vars map[string]string) string {
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if text[i] != '#' || i+1 == len(text) {
			out.WriteByte(text[i])
			continue
		}
		switch text[i+1] {
		case '#':
			out.WriteByte('#')
			i++
		case '{':
			end := strings.IndexByte(text[i+2:], '}')
			if end < 0 {
				out.WriteString(text[i:])
				return out.String()
			}
			out.WriteString(vars[text[i+2:i+2+end]])
			i += 2 + end
		default:
			out.WriteByte('#')
		}
	}
	return out.String()
}
