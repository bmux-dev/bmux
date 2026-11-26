// Package format expands tmux-style #{name} format strings with a pane's
// variables, for bmux list-panes, list-windows and display-message.
package format

import (
	"strconv"
	"strings"

	"github.com/bmux-dev/bmux/internal/protocol"
)

type shellQuoteState int

const (
	shellUnquoted shellQuoteState = iota
	shellSingle
	shellDouble
	shellCmdSub
	shellGroup
)

// Names lists every variable, in the order the docs give them.
var Names = []string{
	"session_name", "session_id", "session_path", "session_windows", "session_attached",
	"window_index", "window_name", "window_active", "window_zoomed", "window_panes",
	"pane_id", "pane_index", "pane_target", "pane_active",
	"pane_current_path", "pane_current_command", "pane_title", "pane_width", "pane_height",
	"pane_agent", "pane_agent_state", "pane_pipe",
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
		"pane_pipe":            flag(info.Pipe),
	}
}

// Expand replaces each #{name} in text with vars[name], or nothing for an
// unknown name; ## is a literal #. Anything else, including an unterminated
// #{, is kept as written.
func Expand(text string, vars map[string]string) string {
	return expand(text, vars, nil)
}

// ExpandShell replaces each #{name} in text using vars like Expand, while
// escaping expanded values for the POSIX shell quoting context at each site so
// untrusted metadata passed to /bin/sh -c cannot inject shell syntax.
func ExpandShell(text string, vars map[string]string) string {
	return expand(text, vars, quoteShellValue)
}

func expand(text string, vars map[string]string, quote func(shellQuoteState, string) string) string {
	var out strings.Builder
	stack := []shellQuoteState{shellUnquoted}
	for i := 0; i < len(text); i++ {
		if text[i] == '#' && i+1 < len(text) {
			switch text[i+1] {
			case '#':
				out.WriteByte('#')
				i++
				continue
			case '{':
				if end := strings.IndexByte(text[i+2:], '}'); end >= 0 {
					val := vars[text[i+2:i+2+end]]
					if quote != nil {
						val = quote(stack[len(stack)-1], val)
					}
					out.WriteString(val)
					i += 2 + end
					continue
				}
				out.WriteString(text[i:])
				return out.String()
			}
		}
		b := text[i]
		out.WriteByte(b)
		if quote == nil {
			continue
		}
		top := stack[len(stack)-1]
		switch top {
		case shellSingle:
			if b == '\'' {
				stack = stack[:len(stack)-1]
			}
		case shellDouble:
			switch {
			case b == '"':
				stack = stack[:len(stack)-1]
			case b == '\\' && i+1 < len(text):
				switch text[i+1] {
				case '\\', '"', '$', '`', '\n':
					i++
					out.WriteByte(text[i])
				}
			case b == '$' && i+1 < len(text) && text[i+1] == '(':
				i++
				out.WriteByte('(')
				stack = append(stack, shellCmdSub)
			}
		default: // shellUnquoted, shellCmdSub, shellGroup
			switch {
			case b == '\\' && i+1 < len(text):
				i++
				out.WriteByte(text[i])
			case b == '\'':
				stack = append(stack, shellSingle)
			case b == '"':
				stack = append(stack, shellDouble)
			case b == '$' && i+1 < len(text) && text[i+1] == '(':
				i++
				out.WriteByte('(')
				stack = append(stack, shellCmdSub)
			case b == '(':
				stack = append(stack, shellGroup)
			case b == ')' && (top == shellCmdSub || top == shellGroup):
				stack = stack[:len(stack)-1]
			}
		}
	}
	return out.String()
}

func quoteShellValue(ctx shellQuoteState, val string) string {
	val = strings.ReplaceAll(val, "\x00", "")
	switch ctx {
	case shellSingle:
		return strings.ReplaceAll(val, "'", `'\''`)
	case shellDouble:
		var b strings.Builder
		for i := 0; i < len(val); i++ {
			switch val[i] {
			case '\\', '"', '$', '`':
				b.WriteByte('\\')
			}
			b.WriteByte(val[i])
		}
		return b.String()
	default:
		if val == "" || isShellBare(val) {
			return val
		}
		return "'" + strings.ReplaceAll(val, "'", `'\''`) + "'"
	}
}

func isShellBare(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '/', c == ':', c == '@', c == '-':
		default:
			return false
		}
	}
	return true
}

// Env returns pane metadata environment variables ("KEY=VALUE") for shell
// hooks, run-shell, and pipe-pane commands, stripping NUL bytes for exec safety.
func Env(info protocol.PaneInfo) []string {
	clean := func(s string) string { return strings.ReplaceAll(s, "\x00", "") }
	return []string{
		"BMUX_SESSION_PATH=" + clean(info.SessionPath),
		"BMUX_WINDOW_INDEX=" + strconv.Itoa(info.Window),
		"BMUX_WINDOW_NAME=" + clean(info.WindowName),
		"BMUX_PANE_TARGET=" + clean(info.Session) + ":" + strconv.Itoa(info.Pane),
		"BMUX_PANE_PATH=" + clean(info.Path),
		"BMUX_PANE_COMMAND=" + clean(info.Command),
		"BMUX_PANE_TITLE=" + clean(info.Title),
		"BMUX_PANE_AGENT=" + clean(info.Agent),
		"BMUX_PANE_AGENT_STATE=" + clean(info.State),
	}
}

// ApplyEnv returns base with any inherited pane metadata variables replaced by
// info's values.
func ApplyEnv(base []string, info protocol.PaneInfo) []string {
	out := make([]string, 0, len(base)+9)
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "BMUX_SESSION_PATH", "BMUX_WINDOW_INDEX", "BMUX_WINDOW_NAME",
			"BMUX_PANE_TARGET", "BMUX_PANE_PATH", "BMUX_PANE_COMMAND",
			"BMUX_PANE_TITLE", "BMUX_PANE_AGENT", "BMUX_PANE_AGENT_STATE":
			continue
		}
		out = append(out, entry)
	}
	return append(out, Env(info)...)
}
