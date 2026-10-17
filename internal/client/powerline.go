package client

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

var powerlineIdentity = func() string {
	user := os.Getenv("USER")
	host, _ := os.Hostname()
	host = strings.Split(host, ".")[0]
	if user == "" {
		return host
	}
	if host == "" {
		return user
	}
	return user + "@" + host
}()

type powerlineSegment struct {
	text   string
	fg, bg tcell.Color
}

func (u *ui) drawPowerline(width int, now time.Time) {
	if width <= 0 {
		return
	}
	u.screen.PutStrStyled(0, 0, fit("", width), barStyle)
	if u.picker && u.searching {
		u.screen.PutStrStyled(0, 0, fit(" / "+u.query, width), barStyle.Bold(true))
		return
	}
	clock := " " + now.Format("15:04:05") + " "
	if width >= 76 {
		clock += " " + now.Format("2006-01-02") + " "
	}
	rightWidth := min(width, displayWidth(clock)+1)
	leftWidth := max(0, width-rightWidth)
	context := "sessions"
	if (u.conn != nil || !u.picker) && u.name != "" {
		context = u.name
	}
	detail := fmt.Sprintf(" %d sessions ", len(u.filtered()))
	if !u.picker {
		detail = " " + u.focusedPaneLabel() + " "
	}
	segments := []powerlineSegment{{" bmux ", chrome.background, accentColor}, {" " + context + " ", chrome.foreground, chrome.bar}, {detail, chrome.foreground, chrome.background}}
	if width >= 100 {
		segments[0].text = " " + strings.TrimSpace(fit(powerlineIdentity, 28)) + " "
	}
	arrow, back := "", ""
	if os.Getenv("BMUX_POWERLINE_ASCII") == "1" || !u.screen.CanDisplay('', false) {
		arrow, back = ">", "<"
	}
	x := 0
	for i, segment := range segments {
		available := leftWidth - x
		if available <= 0 {
			break
		}
		label := segment.text
		if i == 1 {
			label = " " + strings.TrimSpace(fit(context, min(24, max(0, available-2)))) + " "
		}
		label = fit(label, min(displayWidth(label), available))
		u.screen.PutStrStyled(x, 0, label, tcell.StyleDefault.Foreground(segment.fg).Background(segment.bg).Bold(i == 0))
		x += displayWidth(label)
		if x < leftWidth {
			next := chrome.bar
			if i+1 < len(segments) {
				next = segments[i+1].bg
			}
			u.screen.Put(x, 0, arrow, tcell.StyleDefault.Foreground(segment.bg).Background(next))
			x++
		}
	}
	if rightWidth > 0 {
		start := width - rightWidth
		u.screen.Put(start, 0, back, tcell.StyleDefault.Foreground(chrome.selection).Background(chrome.bar))
		u.screen.PutStrStyled(start+1, 0, fit(clock, max(0, rightWidth-1)), tcell.StyleDefault.Foreground(chrome.foreground).Background(chrome.selection))
	}
}
