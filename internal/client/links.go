package client

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/josephferrero/bmux/internal/protocol"
)

// plainURL finds the start of a link printed as ordinary text; trimURL then
// drops the punctuation that follows a URL in prose.
var plainURL = regexp.MustCompile(`(?:https?://|mailto:)[^\s<>"'\\` + "`" + `]+`)

// openableLink reports whether raw is a link bmux opens or hands to the
// attached terminal: http, https or mailto, with no control characters. A
// program in a pane chooses the text, so anything else (file:, custom
// schemes, escape sequences) stays inert.
func openableLink(raw string) bool {
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != ""
	}
	return false
}

// trimURL drops trailing punctuation from a URL found in text. A closing
// bracket stays when the URL holds its opening one, as in Wikipedia links.
func trimURL(s string) string {
	for s != "" {
		last := s[len(s)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '\'', '"':
		case ')', ']', '}':
			open := map[byte]byte{')': '(', ']': '[', '}': '{'}[last]
			if strings.Count(s, string(open)) >= strings.Count(s, string(last)) {
				return s
			}
		default:
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// linkAt returns the link under column x of row y: the OSC 8 hyperlink the
// application set there, or a URL printed as text. Rows the terminal wrapped
// are read as one line, so a URL split across rows is found whole. "" means
// there is no openable link.
func linkAt(f *protocol.TerminalFrame, x, y int) string {
	if f == nil || y < 0 || y >= len(f.Rows) || x < 0 || x >= len(f.Rows[y]) {
		return ""
	}
	row := f.Rows[y]
	x = cellOwner(row, x)
	if n := row[x].Link; n > 0 {
		if n <= len(f.Links) && openableLink(f.Links[n-1]) {
			return f.Links[n-1]
		}
		return ""
	}
	first, last := y, y
	for first > 0 && first-1 < len(f.Wrapped) && f.Wrapped[first-1] {
		first--
	}
	for last < len(f.Rows)-1 && last < len(f.Wrapped) && f.Wrapped[last] {
		last++
	}
	var text strings.Builder
	at := -1 // byte offset of the clicked cell in text
	for r := first; r <= last; r++ {
		for c, cell := range f.Rows[r] {
			if r == y && c == x {
				at = text.Len()
			}
			switch {
			case cell.Width == 0 && c > 0 && f.Rows[r][c-1].Width > 0:
				// A continuation cell has no text of its own.
			case cell.Text == "" || cell.Attrs&uv.AttrConceal != 0:
				text.WriteByte(' ')
			default:
				text.WriteString(cell.Text)
			}
		}
	}
	s := text.String()
	for _, m := range plainURL.FindAllStringIndex(s, -1) {
		found := trimURL(s[m[0]:m[1]])
		if at >= m[0] && at < m[0]+len(found) && openableLink(found) {
			return found
		}
	}
	return ""
}

// openLink opens raw with the desktop's handler on the machine running this
// client, so it works from a remote session too.
func openLink(raw string) error {
	if !openableLink(raw) {
		return fmt.Errorf("won't open %q", raw)
	}
	var name string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "linux", "freebsd", "openbsd", "netbsd":
		name = "xdg-open"
	default:
		return errors.New("opening links isn't supported on " + runtime.GOOS)
	}
	cmd := exec.Command(name, raw)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
