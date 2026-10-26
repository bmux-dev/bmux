package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// The sidebar runs along the left edge of the attached view, between the
// powerline and status rows, like herdr's: a Sessions section, a divider, and
// an Agents section listing every agent across sessions. Its last column is a
// border. Narrow terminals hide it so panes keep usable widths. Width and the
// hiding threshold come from the config file.
func (u *ui) sidebarCols() int {
	if u.settings.Sidebar.Width > 0 {
		return u.settings.Sidebar.Width
	}
	return config.Default().Sidebar.Width
}

func (u *ui) sidebarMinScreen() int {
	if u.settings.Sidebar.MinScreenWidth > 0 {
		return u.settings.Sidebar.MinScreenWidth
	}
	return config.Default().Sidebar.MinScreenWidth
}

// sidebarRow is a session (pane == 0) or an agent/attention pane in it.
type sidebarRow struct {
	session  protocol.Session
	pane     int
	activity protocol.PaneActivity
}

// sidebarTarget identifies the selected row across refreshes, which may add,
// remove or reorder rows.
type sidebarTarget struct {
	name string
	pane int
}

type sidebarResult struct {
	sessions []protocol.Session
	err      error
}

func (u *ui) sidebarShown(w int) bool { return !u.sidebarHidden && w >= u.sidebarMinScreen() }

func (u *ui) paneOriginX(w int) int {
	if u.sidebarShown(w) {
		return u.sidebarCols()
	}
	return 0
}

func (u *ui) paneViewportWidth(w int) int { return max(1, w-u.paneOriginX(w)) }

// creationOrder sorts sessions the way the sidebar shows them. Unlike the
// recency-ordered picker, this order doesn't move while you switch.
func creationOrder(sessions []protocol.Session) []protocol.Session {
	list := append([]protocol.Session(nil), sessions...)
	sort.SliceStable(list, func(i, j int) bool {
		if !list[i].Created.Equal(list[j].Created) {
			return list[i].Created.Before(list[j].Created)
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// sidebarRows lists sessions, then every agent row in the same session order;
// j/k walk this order across the divider.
func (u *ui) sidebarRows() (rows []sidebarRow, sessions int) {
	list := creationOrder(u.sessions)
	for _, s := range list {
		rows = append(rows, sidebarRow{session: s})
	}
	for _, s := range list {
		for _, a := range s.Activity {
			rows = append(rows, sidebarRow{session: s, pane: a.Pane, activity: a})
		}
	}
	return rows, len(list)
}

// cycleSession switches to the next (delta 1) or previous (-1) session in
// sidebar order, wrapping around, like tmux's switch-client -n/-p.
func (u *ui) cycleSession(delta int) {
	if r, err := Request(u.socket, protocol.Request{Op: "list"}); err == nil {
		u.updateSessions(r.Sessions)
	}
	next := adjacentSession(u.sessions, u.name, delta)
	if next == "" {
		u.notice("No other session")
		return
	}
	if err := u.switchTo(next); err != nil {
		u.notice(err.Error())
	}
}

// adjacentSession is the session delta steps from current in sidebar order,
// or "" when there is no other session.
func adjacentSession(sessions []protocol.Session, current string, delta int) string {
	list := creationOrder(sessions)
	for i, s := range list {
		if s.Name == current && len(list) > 1 {
			return list[((i+delta)%len(list)+len(list))%len(list)].Name
		}
	}
	return ""
}

// sidebarSections places the Sessions heading at paneOriginY, its rows from
// sessionTop, a divider, the Agents heading, then two-line agent entries from
// agentTop. divider < 0 means there is no room for the Agents section.
type sidebarSections struct {
	sessionTop, sessionSlots int
	divider                  int
	agentTop, agentSlots     int
}

func sidebarLayout(sessions, h int) sidebarSections {
	available := max(0, h-1-paneOriginY)
	// Sessions take what they need, up to half, so agents get the rest.
	sessionHeight := min(available, max(2, min(sessions+1, available/2)))
	l := sidebarSections{sessionTop: paneOriginY + 1, sessionSlots: max(0, sessionHeight-1), divider: -1}
	if rest := available - sessionHeight; rest >= 3 {
		l.divider = paneOriginY + sessionHeight
		l.agentTop = l.divider + 2
		l.agentSlots = (rest - 2) / 2
	}
	return l
}

func windowStart(selected, slots int) int {
	if selected < 0 {
		return 0
	}
	return max(0, selected-slots+1)
}

// sidebarWindows returns the selected row (-1 unless the sidebar has focus)
// and the first visible row of each section, keeping the selection visible.
func (u *ui) sidebarWindows(sessions int, l sidebarSections, rows []sidebarRow) (selected, sessionStart, agentStart int) {
	selected = -1
	if u.sidebarFocus {
		selected = u.sidebarIndex(rows)
	}
	if selected >= 0 && selected < sessions {
		sessionStart = windowStart(selected, l.sessionSlots)
	}
	if selected >= sessions {
		agentStart = windowStart(selected-sessions, l.agentSlots)
	}
	return selected, sessionStart, agentStart
}

func (u *ui) sidebarIndex(rows []sidebarRow) int {
	for i, r := range rows {
		if r.session.Name == u.sidebarSelected.name && r.pane == u.sidebarSelected.pane {
			return i
		}
	}
	for i, r := range rows {
		if r.session.Name == u.name && r.pane == 0 {
			return i
		}
	}
	return 0
}

func (u *ui) focusSidebar() {
	u.sidebarFocus = true
	u.sidebarSelected = sidebarTarget{name: u.name}
	u.prefix = false
	u.refreshSidebar()
}

func (u *ui) toggleSidebar() {
	u.sidebarHidden = !u.sidebarHidden
	u.sidebarFocus = false
	u.sendResize()
	u.refreshSidebar()
}

func (u *ui) moveSidebar(delta int) {
	rows, _ := u.sidebarRows()
	if len(rows) == 0 {
		return
	}
	i := max(0, min(u.sidebarIndex(rows)+delta, len(rows)-1))
	u.sidebarSelected = sidebarTarget{rows[i].session.Name, rows[i].pane}
}

// sidebarKey handles every key while the sidebar has focus; none reach panes.
// Keys bound to navigate move through it; right opens the selected row. While
// a delete confirmation is open, it gets the key instead.
func (u *ui) sidebarKey(e *tcell.EventKey) {
	if u.prompt == "delete" {
		u.answerDelete(e)
		return
	}
	if kind, direction := actionDirection(u.rootAction(e)); kind == "navigate" {
		switch direction {
		case "down":
			u.moveSidebar(1)
		case "up":
			u.moveSidebar(-1)
		case "right":
			u.openSidebarRow()
		}
		return
	}
	if u.isPrefix(e) {
		u.sidebarFocus = false
		u.prefix = true
		return
	}
	switch e.Key() {
	case tcell.KeyEscape:
		u.sidebarFocus = false
	case tcell.KeyDown:
		u.moveSidebar(1)
	case tcell.KeyUp:
		u.moveSidebar(-1)
	case tcell.KeyEnter, tcell.KeyRight:
		u.openSidebarRow()
	case tcell.KeyRune:
		if e.Modifiers() != tcell.ModNone {
			return
		}
		switch e.Rune() {
		case 'j':
			u.moveSidebar(1)
		case 'k':
			u.moveSidebar(-1)
		case 'l':
			u.openSidebarRow()
		case 'd':
			u.deleteSidebarRow()
		case 'n':
			u.openProjects()
		}
	}
}

// deleteSidebarRow asks to delete the selected session row, like d in the
// picker. Agent rows stand for one pane, so they never delete a session.
func (u *ui) deleteSidebarRow() {
	rows, sessions := u.sidebarRows()
	if len(rows) == 0 {
		return
	}
	if i := u.sidebarIndex(rows); i < sessions {
		u.beginDelete(rows[i].session)
	} else {
		u.notice("Select a session row to delete it")
	}
}

// openSidebarRow attaches the selected session if needed and focuses the
// selected pane, or for a session row the first pane waiting on you.
func (u *ui) openSidebarRow() {
	rows, _ := u.sidebarRows()
	u.sidebarFocus = false
	if len(rows) == 0 {
		return
	}
	row := rows[u.sidebarIndex(rows)]
	pane := row.pane
	if pane == 0 {
		for _, a := range row.session.Activity {
			if a.Attention || a.State == protocol.AgentBlocked {
				pane = a.Pane
				break
			}
		}
	}
	if row.session.Name != u.name || u.conn == nil {
		if err := u.switchTo(row.session.Name); err != nil {
			u.notice(err.Error())
			return
		}
	}
	if pane != 0 && pane != u.focused {
		u.send(protocol.Request{Op: "select-pane", PaneID: pane})
	}
}

// sidebarClick selects the row under a click at screen row y and gives the
// sidebar focus, so keys like d reach it right away instead of switching
// sessions underfoot. A second click on the same row within the double-click
// window (as for selecting words in a pane) opens it, like Enter.
func (u *ui) sidebarClick(y, h int) {
	rows, sessions := u.sidebarRows()
	l := sidebarLayout(sessions, h)
	_, sessionStart, agentStart := u.sidebarWindows(sessions, l, rows)
	index := -1
	switch {
	case y >= l.sessionTop && y < l.sessionTop+l.sessionSlots:
		if i := sessionStart + y - l.sessionTop; i < sessions {
			index = i
		}
	case l.divider >= 0 && y >= l.agentTop && y < l.agentTop+2*l.agentSlots:
		if i := sessions + agentStart + (y-l.agentTop)/2; i < len(rows) {
			index = i
		}
	}
	if index < 0 {
		return
	}
	doubleClick := index == u.sidebarClickIndex && time.Since(u.sidebarLastClick) < 350*time.Millisecond
	u.sidebarLastClick, u.sidebarClickIndex = time.Now(), index
	u.sidebarSelected = sidebarTarget{rows[index].session.Name, rows[index].pane}
	if doubleClick {
		u.openSidebarRow()
		return
	}
	u.sidebarFocus = true
	u.prefix = false
	u.refreshSidebar()
}

func (u *ui) refreshSidebar() {
	if u.sidebarBusy || u.sidebarResults == nil || u.socket == "" {
		return
	}
	u.sidebarBusy = true
	socket, results := u.socket, u.sidebarResults
	go func() {
		r, err := Request(socket, protocol.Request{Op: "list"})
		results <- sidebarResult{r.Sessions, err}
	}()
}

// The picker refreshes the same list synchronously and owns it while open.
func (u *ui) finishSidebarRefresh(result sidebarResult) {
	u.sidebarBusy = false
	if result.err == nil && !u.picker {
		u.updateSessions(result.sessions)
	}
}

// updateSessions replaces the session list and plays a sound when an agent
// becomes ready (stopped working while unseen) or blocked, like herdr's
// background notifications. The first list only sets the baseline.
func (u *ui) updateSessions(sessions []protocol.Session) {
	ready, blocked := agentAlerts(u.sessions, sessions)
	known := u.alertsKnown
	u.sessions, u.alertsKnown = sessions, true
	if known && (ready || blocked) && u.sound != nil {
		u.sound(blocked)
	}
}

// agentAlerts reports agents that newly need you between two session lists.
// Bells from ordinary programs mark their pane but never play a sound.
func agentAlerts(previous, current []protocol.Session) (ready, blocked bool) {
	type key struct {
		name string
		pane int
	}
	before := make(map[key]protocol.PaneActivity)
	for _, s := range previous {
		for _, a := range s.Activity {
			before[key{s.Name, a.Pane}] = a
		}
	}
	for _, s := range current {
		for _, a := range s.Activity {
			if a.Agent == "" {
				continue
			}
			old := before[key{s.Name, a.Pane}]
			switch {
			case a.State == protocol.AgentBlocked && old.State != protocol.AgentBlocked:
				blocked = true
			case a.Attention && !old.Attention:
				ready = true
			}
		}
	}
	return ready, blocked
}

// herdr's default "dots" indicators: ● for working, blocked and done, ○ for
// idle, each in its state color.
func agentIcon(a protocol.PaneActivity) (string, tcell.Style) {
	switch {
	case a.State == protocol.AgentBlocked:
		return "●", sidebarBlockedStyle
	case a.Attention:
		return "●", sidebarDoneStyle
	case a.State == protocol.AgentWorking:
		return "●", sidebarWorkingStyle
	}
	return "○", sidebarIdleStyle
}

// activityLabel returns the agent row's icon, state text and their style.
func activityLabel(a protocol.PaneActivity) (string, string, tcell.Style) {
	icon, style := agentIcon(a)
	switch {
	case a.State == protocol.AgentBlocked:
		return icon, "blocked", style
	case a.Attention && a.State == protocol.AgentIdle:
		return icon, "waiting", style
	case a.Attention:
		return icon, "bell", style
	}
	return icon, a.State, style
}

// sessionRollup is the most urgent agent state in a session, like herdr's
// workspace rollup: blocked, then done, then working, then idle.
func sessionRollup(s protocol.Session) (string, tcell.Style) {
	best, rank := protocol.PaneActivity{}, -1
	for _, a := range s.Activity {
		r := 0
		switch {
		case a.State == protocol.AgentBlocked:
			r = 3
		case a.Attention:
			r = 2
		case a.State == protocol.AgentWorking:
			r = 1
		}
		if r > rank {
			best, rank = a, r
		}
	}
	if rank < 0 {
		return "", sidebarBaseStyle
	}
	return agentIcon(best)
}

func (u *ui) drawSidebar(w, h int) {
	if !u.sidebarShown(w) {
		return
	}
	content := u.sidebarCols() - 1
	border := dividerStyle
	if u.sidebarFocus {
		border = border.Foreground(accentColor).Bold(true)
	}
	for y := paneOriginY; y < h-1; y++ {
		u.screen.PutStrStyled(0, y, fit("", content), sidebarBaseStyle)
		u.screen.Put(content, y, "│", border)
	}
	if h-1 <= paneOriginY {
		return
	}
	heading := sidebarMutedStyle.Bold(true)
	if u.sidebarFocus {
		heading = sidebarAccentStyle
	}
	rows, sessions := u.sidebarRows()
	l := sidebarLayout(sessions, h)
	selected, sessionStart, agentStart := u.sidebarWindows(sessions, l, rows)
	u.screen.PutStrStyled(0, paneOriginY, fit(" SESSIONS", content), heading)
	for i := sessionStart; i < sessions && i < sessionStart+l.sessionSlots; i++ {
		u.drawSessionRow(rows[i], l.sessionTop+i-sessionStart, content, i == selected)
	}
	if l.divider < 0 {
		return
	}
	u.screen.PutStrStyled(0, l.divider, strings.Repeat(u.sidebarGlyph("─"), content), sidebarMutedStyle)
	agents := rows[sessions:]
	title := " AGENTS"
	if len(agents) > 0 {
		title = fmt.Sprintf(" AGENTS %d", len(agents))
	}
	u.screen.PutStrStyled(0, l.divider+1, fit(title, content), heading)
	if len(agents) == 0 {
		u.screen.PutStrStyled(0, l.agentTop, fit("   none running", content), sidebarMutedStyle)
		return
	}
	for i := agentStart; i < len(agents) && i < agentStart+l.agentSlots; i++ {
		u.drawAgentRow(agents[i], l.agentTop+2*(i-agentStart), content, sessions+i == selected)
	}
}

func (u *ui) sidebarGlyph(glyph string) string {
	if fallback, ok := sidebarASCII[glyph]; ok && !u.screen.CanDisplay([]rune(glyph)[0], false) {
		return fallback
	}
	return glyph
}

// putSidebarLine draws left-aligned text with an optional right-aligned label.
func (u *ui) putSidebarLine(y, width int, left string, style tcell.Style, right string, rightStyle tcell.Style) {
	rightWidth := 0
	if right != "" {
		rightWidth = displayWidth(right) + 1
	}
	u.screen.PutStrStyled(0, y, fit(left, max(0, width-rightWidth)), style)
	if right != "" && rightWidth <= width {
		u.screen.PutStrStyled(width-rightWidth, y, fit(right+" ", rightWidth), rightStyle)
	}
}

func (u *ui) drawSessionRow(row sidebarRow, y, width int, selected bool) {
	icon, iconStyle := sessionRollup(row.session)
	style := sidebarBaseStyle
	if row.session.Name == u.name {
		style = sidebarAccentStyle
	}
	if selected {
		style, iconStyle = sidebarSelectedStyle, sidebarSelectedStyle
	}
	right, rightStyle := "", style
	if row.session.Restored {
		right = "restored"
		if !selected {
			rightStyle = sidebarMutedStyle
		}
	}
	u.putSidebarLine(y, width, "   "+row.session.Name, style, right, rightStyle)
	if icon != "" {
		u.screen.PutStrStyled(1, y, u.sidebarGlyph(icon), iconStyle)
	}
}

// Agent entries take two lines: icon, agent and state, then its session.
func (u *ui) drawAgentRow(row sidebarRow, y, width int, selected bool) {
	name := row.activity.Agent
	if name == "" {
		name = "terminal"
	}
	icon, state, stateStyle := activityLabel(row.activity)
	iconStyle, nameStyle, sessionStyle := stateStyle, sidebarBaseStyle, sidebarMutedStyle
	if row.activity.State == protocol.AgentIdle && !row.activity.Attention {
		stateStyle = sidebarMutedStyle
	}
	if selected {
		iconStyle, nameStyle, stateStyle, sessionStyle = sidebarSelectedStyle, sidebarSelectedStyle, sidebarSelectedStyle, sidebarSelectedStyle
	}
	u.putSidebarLine(y, width, "   "+name, nameStyle, state, stateStyle)
	u.screen.PutStrStyled(1, y, u.sidebarGlyph(icon), iconStyle)
	u.putSidebarLine(y+1, width, "   "+row.session.Name, sessionStyle, "", sessionStyle)
}

var sidebarASCII = map[string]string{"●": "*", "○": "o", "─": "-"}
