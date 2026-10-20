package client

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

// The scrollback view (Ctrl-a v) puts a cursor on the focused pane's history
// for keyboard selection and search. It is optional: nothing needs it, and
// the mouse works as always. Rows are absolute row IDs, as frames number them
// (TerminalFrame.Base + y) within one epoch; copies come from the server, so
// a selection may reach rows this client never received.
type scrollView struct {
	active bool
	pane   int
	epoch  uint64
	cursor point // x is a column, y an absolute row
	anchor point
	mode   string // "" (no selection), "char", "line" or "block"
	top    int    // the row expected at the top of the viewport
	// Search: prompting while searching; query is the last search, which
	// matches on screen highlight and n/N repeat, up toward older rows.
	searching bool
	input     string
	query     string
	up        bool
}

// openScrollView starts the view at the pane's cursor.
func (u *ui) openScrollView() {
	f := u.frame
	if f == nil || u.conn == nil {
		return
	}
	if f.Alt {
		u.notice("Full-screen programs have no scrollback")
		return
	}
	u.prefix = false
	u.selection = selection{}
	u.view = scrollView{active: true, pane: u.focused, epoch: f.Epoch, top: f.Base}
	u.view.cursor = point{f.CursorX, f.Base + f.Offset + f.CursorY}
	u.viewMove(u.view.cursor)
}

// closeScrollView leaves the view and returns the pane to live output.
func (u *ui) closeScrollView() {
	if !u.view.active {
		return
	}
	u.view = scrollView{}
	u.send(protocol.Request{Op: "scroll"})
}

// viewBounds are the oldest and newest rows and the width the view can reach.
func (u *ui) viewBounds() (oldest, newest, width int) {
	f := u.frame
	if f == nil || len(f.Rows) == 0 {
		return 0, 0, 1
	}
	return f.HistoryStart, f.Base + f.Offset + len(f.Rows) - 1, max(1, len(f.Rows[0]))
}

// viewRow is the frame's copy of a row, or nil when it isn't on screen.
func (u *ui) viewRow(id int) []protocol.Cell {
	f := u.frame
	if f == nil || id < f.Base || id >= f.Base+len(f.Rows) {
		return nil
	}
	return f.Rows[id-f.Base]
}

// viewMove puts the cursor at p, within history, and scrolls so it shows.
func (u *ui) viewMove(p point) {
	oldest, newest, width := u.viewBounds()
	p.y = max(oldest, min(p.y, newest))
	p.x = max(0, min(p.x, width-1))
	u.view.cursor = p
	height := 1
	if u.frame != nil {
		height = max(1, len(u.frame.Rows))
	}
	switch {
	case p.y < u.view.top:
		u.scroll(u.view.top - p.y)
		u.view.top = p.y
	case p.y >= u.view.top+height:
		u.scroll(u.view.top + height - 1 - p.y)
		u.view.top = p.y - height + 1
	}
}

// noteViewFrame follows the focused pane's frames: the expected top row, and
// closing the view when its rows stop meaning what they did.
func (u *ui) noteViewFrame(f *protocol.TerminalFrame) {
	if !u.view.active {
		return
	}
	if u.focused != u.view.pane || f == nil || f.Alt || f.Epoch != u.view.epoch {
		u.view = scrollView{}
		u.notice("Scrollback view closed: the pane's history changed")
		return
	}
	u.view.top = f.Base
}

// viewSelected reports whether the cell at column x of row id is selected.
func (v scrollView) selected(x, id int) bool {
	if v.mode == "" {
		return false
	}
	a, b := v.anchor, v.cursor
	if before(b, a) {
		a, b = b, a
	}
	switch v.mode {
	case "line":
		return id >= a.y && id <= b.y
	case "block":
		lo, hi := min(v.anchor.x, v.cursor.x), max(v.anchor.x, v.cursor.x)
		return id >= a.y && id <= b.y && x >= lo && x <= hi
	}
	return !before(point{x, id}, a) && !before(b, point{x, id})
}

// isBlank reports a cell that holds no text.
func isBlank(c protocol.Cell) bool { return strings.TrimSpace(c.Text) == "" && c.Width != 0 }

// viewKey handles every key while the view is open; none reach the pane.
func (u *ui) viewKey(e *tcell.EventKey) {
	if u.view.searching {
		u.searchPromptKey(e)
		return
	}
	if u.isPrefix(e) {
		u.closeScrollView()
		u.prefix = true
		return
	}
	c := u.view.cursor
	height := 1
	if u.frame != nil {
		height = max(1, len(u.frame.Rows))
	}
	oldest, newest, width := u.viewBounds()
	switch e.Key() {
	case tcell.KeyEscape:
		if u.view.mode != "" {
			u.view.mode = ""
		} else {
			u.closeScrollView()
		}
		return
	case tcell.KeyCtrlC:
		u.closeScrollView()
		return
	case tcell.KeyEnter:
		u.copyView()
		return
	case tcell.KeyLeft:
		u.viewMove(point{c.x - 1, c.y})
	case tcell.KeyRight:
		u.viewMove(point{c.x + 1, c.y})
	case tcell.KeyUp:
		u.viewMove(point{c.x, c.y - 1})
	case tcell.KeyDown:
		u.viewMove(point{c.x, c.y + 1})
	case tcell.KeyHome:
		u.viewMove(point{0, c.y})
	case tcell.KeyEnd:
		u.viewMove(point{u.lineEnd(c.y, width), c.y})
	case tcell.KeyPgUp, tcell.KeyCtrlB:
		u.viewMove(point{c.x, c.y - height})
	case tcell.KeyPgDn, tcell.KeyCtrlF:
		u.viewMove(point{c.x, c.y + height})
	case tcell.KeyCtrlU:
		u.viewMove(point{c.x, c.y - height/2})
	case tcell.KeyCtrlD:
		u.viewMove(point{c.x, c.y + height/2})
	case tcell.KeyCtrlV:
		u.toggleViewMode("block")
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
			return
		}
		switch e.Rune() {
		case 'q':
			u.closeScrollView()
		case 'h':
			u.viewMove(point{c.x - 1, c.y})
		case 'l':
			u.viewMove(point{c.x + 1, c.y})
		case 'k':
			u.viewMove(point{c.x, c.y - 1})
		case 'j':
			u.viewMove(point{c.x, c.y + 1})
		case '0':
			u.viewMove(point{0, c.y})
		case '^':
			u.viewMove(point{u.lineStart(c.y), c.y})
		case '$':
			u.viewMove(point{u.lineEnd(c.y, width), c.y})
		case 'w':
			u.viewMove(u.nextWord(c))
		case 'b':
			u.viewMove(u.previousWord(c))
		case 'e':
			u.viewMove(u.wordEnd(c))
		case 'g':
			u.viewMove(point{0, oldest})
		case 'G':
			u.viewMove(point{0, newest})
		case 'H':
			u.viewMove(point{c.x, u.view.top})
		case 'M':
			u.viewMove(point{c.x, u.view.top + height/2})
		case 'L':
			u.viewMove(point{c.x, u.view.top + height - 1})
		case 'v':
			u.toggleViewMode("char")
		case 'V':
			u.toggleViewMode("line")
		case 'y':
			u.copyView()
		case '/', '?':
			u.view.searching, u.view.input, u.view.up = true, "", e.Rune() == '?'
		case 'n':
			u.runSearch(u.view.query, u.view.up)
		case 'N':
			u.runSearch(u.view.query, !u.view.up)
		}
	}
}

// toggleViewMode starts a selection at the cursor, switches its kind, or
// ends it when it already is that kind, like vi's visual modes.
func (u *ui) toggleViewMode(mode string) {
	switch u.view.mode {
	case mode:
		u.view.mode = ""
	case "":
		u.view.mode, u.view.anchor = mode, u.view.cursor
	default:
		u.view.mode = mode
	}
}

// copyView copies the selection from the server's history and leaves.
func (u *ui) copyView() {
	if u.view.mode == "" {
		u.notice("Nothing selected: v, V or Ctrl-v starts a selection")
		return
	}
	a, b := u.view.anchor, u.view.cursor
	r, err := Request(u.socket, protocol.Request{Op: "history-text", Name: u.name, PaneID: u.view.pane, Range: &protocol.TextRange{
		Epoch: u.view.epoch, FromX: a.x, FromY: a.y, ToX: b.x, ToY: b.y, Mode: u.view.mode,
	}})
	if err != nil {
		u.notice("Could not copy: " + err.Error())
		return
	}
	u.closeScrollView()
	u.copyText(r.Text)
}

// lineStart and lineEnd are the first and last columns with text in row id,
// when the row is on screen.
func (u *ui) lineStart(id int) int {
	row := u.viewRow(id)
	for x, c := range row {
		if !isBlank(c) {
			return x
		}
	}
	return 0
}

func (u *ui) lineEnd(id, width int) int {
	row := u.viewRow(id)
	if row == nil {
		return width - 1
	}
	for x := len(row) - 1; x > 0; x-- {
		if !isBlank(row[x]) {
			return cellOwner(row, x)
		}
	}
	return 0
}

// Word motions work on rows that are on screen, which the cursor's always
// is; past the end of a row they continue on the next.
func (u *ui) nextWord(p point) point {
	_, newest, _ := u.viewBounds()
	row := u.viewRow(p.y)
	x := p.x
	for x < len(row) && !isBlank(row[x]) {
		x++
	}
	for x < len(row) && isBlank(row[x]) {
		x++
	}
	if x < len(row) || p.y >= newest {
		return point{min(x, max(0, len(row)-1)), p.y}
	}
	return point{u.lineStart(p.y + 1), p.y + 1}
}

func (u *ui) previousWord(p point) point {
	oldest, _, width := u.viewBounds()
	row := u.viewRow(p.y)
	x := min(p.x, len(row)) - 1
	for x >= 0 && isBlank(row[x]) {
		x--
	}
	if x < 0 {
		if p.y <= oldest {
			return point{0, p.y}
		}
		return point{u.lineEnd(p.y-1, width), p.y - 1}
	}
	for x > 0 && !isBlank(row[x-1]) {
		x--
	}
	return point{x, p.y}
}

func (u *ui) wordEnd(p point) point {
	row := u.viewRow(p.y)
	x := p.x + 1
	for x < len(row) && isBlank(row[x]) {
		x++
	}
	for x+1 < len(row) && !isBlank(row[x+1]) {
		x++
	}
	return point{min(x, max(0, len(row)-1)), p.y}
}

// openSearch (Ctrl-a /) opens the view with a prompt that searches up from
// the bottom, most recent first.
func (u *ui) openSearch() {
	u.openScrollView()
	if u.view.active {
		u.view.searching, u.view.input, u.view.up = true, "", true
	}
}

// searchPromptKey edits the search prompt; Enter searches, Esc stays in the
// view without searching.
func (u *ui) searchPromptKey(e *tcell.EventKey) {
	switch e.Key() {
	case tcell.KeyEscape, tcell.KeyCtrlC:
		u.view.searching = false
	case tcell.KeyEnter:
		u.view.searching = false
		if u.view.input != "" {
			u.runSearch(u.view.input, u.view.up)
		}
	case tcell.KeyBackspace, tcell.KeyBackspace2, tcell.KeyCtrlH:
		u.view.input = dropRune(u.view.input)
	case tcell.KeyCtrlU:
		u.view.input = ""
	case tcell.KeyRune:
		if e.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			u.view.input += string(e.Rune())
		}
	}
}

// runSearch asks the server for the next match from the cursor, up toward
// older rows or down toward newer ones, and moves the cursor onto it.
func (u *ui) runSearch(query string, up bool) {
	if query == "" {
		u.notice("No search yet: / searches down, ? searches up")
		return
	}
	u.view.query, u.view.up = query, up
	direction := "down"
	if up {
		direction = "up"
	}
	c := u.view.cursor
	r, err := Request(u.socket, protocol.Request{Op: "search-history", Name: u.name, PaneID: u.view.pane, Text: query, Direction: direction,
		Range: &protocol.TextRange{Epoch: u.view.epoch, FromX: c.x, FromY: c.y}})
	if err != nil || r.Range == nil {
		message := "no match"
		if err != nil {
			message = err.Error()
		}
		u.notice("Search: " + message)
		return
	}
	u.viewMove(point{r.Range.FromX, r.Range.FromY})
}

// searchMatches marks the cells of a row inside matches of query, with the
// server's smartcase: a lowercase query ignores case.
func searchMatches(row []protocol.Cell, query string) []bool {
	if query == "" {
		return nil
	}
	fold := strings.ToLower(query) == query
	var text strings.Builder
	var owner []int // owner[i] is the column of the cell text byte i came from
	for x, c := range row {
		if c.Width == 0 {
			continue
		}
		t := c.Text
		if t == "" {
			t = " "
		}
		if fold {
			t = strings.ToLower(t)
		}
		text.WriteString(t)
		for range len(t) {
			owner = append(owner, x)
		}
	}
	if fold {
		query = strings.ToLower(query)
	}
	var marked []bool
	s := text.String()
	for offset := 0; ; {
		i := strings.Index(s[offset:], query)
		if i < 0 {
			return marked
		}
		if marked == nil {
			marked = make([]bool, len(row))
		}
		start := offset + i
		for b := start; b < start+len(query); b++ {
			x := owner[b]
			for w := 0; w < max(1, row[x].Width) && x+w < len(row); w++ {
				marked[x+w] = true
			}
		}
		offset = start + 1
	}
}
