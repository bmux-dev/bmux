package client

import (
	"bytes"
	"strconv"
	"strings"
	"time"
)

// probeTimeout bounds how long startup waits for a terminal that doesn't
// answer the device attributes request every terminal is expected to.
const probeTimeout = 300 * time.Millisecond

// kittyProbe asks whether the terminal can show Kitty graphics: the query
// transmits a 1x1 pixel image without displaying it. The cell size query and
// device attributes follow; terminals answer in order, so the attributes reply
// means no more answers are coming.
const (
	kittyProbe  = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	cellProbe   = "\x1b[16t"
	attributes  = "\x1b[c"
	probeKittyI = "i=31;"
)

// terminalProbe is what the terminal answered.
type terminalProbe struct {
	kitty                 bool // answered the Kitty graphics query with OK
	cellWidth, cellHeight int  // from CSI 16t; 0 when unanswered
	done                  bool // the device attributes reply arrived
}

// probeParser reads terminal replies out of whatever else arrives, such as
// keys typed during startup, which it ignores.
type probeParser struct {
	buf []byte
	terminalProbe
}

// feed adds bytes the terminal sent and reads every complete reply.
func (p *probeParser) feed(data []byte) {
	p.buf = append(p.buf, data...)
	for {
		i := bytes.IndexByte(p.buf, 0x1b)
		if i < 0 {
			p.buf = p.buf[:0]
			return
		}
		p.buf = p.buf[i:]
		if len(p.buf) < 2 {
			return
		}
		var end int
		switch p.buf[1] {
		case '_': // APC, ends with ESC \ or BEL
			end = apcEnd(p.buf)
			if end < 0 {
				return
			}
			p.apc(string(p.buf[2 : end-stLength(p.buf[:end])]))
		case '[':
			end = csiEnd(p.buf)
			if end == -1 {
				return
			}
			if end == -2 {
				end = 2 // malformed: skip the introducer
			} else {
				p.csi(string(p.buf[2:end-1]), p.buf[end-1])
			}
		default:
			end = 2
		}
		p.buf = p.buf[end:]
	}
}

// stLength is the length of the string terminator ending an APC.
func stLength(apc []byte) int {
	if len(apc) > 0 && apc[len(apc)-1] == 0x07 {
		return 1
	}
	return 2
}

// apcEnd returns the index just past an APC's terminator, or -1 if incomplete.
func apcEnd(b []byte) int {
	for i := 2; i < len(b); i++ {
		if b[i] == 0x07 {
			return i + 1
		}
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
			return i + 2
		}
	}
	return -1
}

// csiEnd returns the index just past a CSI's final byte, -1 if incomplete, or
// -2 if what follows the introducer isn't a CSI.
func csiEnd(b []byte) int {
	for i := 2; i < len(b); i++ {
		if b[i] >= 0x40 && b[i] <= 0x7e {
			return i + 1
		}
		if b[i] < 0x20 || b[i] > 0x3f {
			return -2
		}
	}
	return -1
}

func (p *probeParser) apc(payload string) {
	if !strings.HasPrefix(payload, "G") {
		return
	}
	control, message, _ := strings.Cut(payload[1:], ";")
	if strings.Contains(control+";", probeKittyI) && message == "OK" {
		p.kitty = true
	}
}

func (p *probeParser) csi(params string, final byte) {
	switch {
	case final == 'c' && strings.HasPrefix(params, "?"):
		p.done = true
	case final == 't':
		var op, h, w int
		fields := strings.Split(params, ";")
		if len(fields) != 3 {
			return
		}
		var err error
		if op, err = strconv.Atoi(fields[0]); err != nil || op != 6 {
			return
		}
		if h, err = strconv.Atoi(fields[1]); err != nil {
			return
		}
		if w, err = strconv.Atoi(fields[2]); err != nil {
			return
		}
		if w > 0 && h > 0 && w <= maxCellPixels && h <= maxCellPixels {
			p.cellWidth, p.cellHeight = w, h
		}
	}
}

// maxCellPixels is the largest cell size believed; the server applies the same bound.
const maxCellPixels = 512

// probeTerminal asks the terminal what it can show. It runs before the screen
// starts, so replies are read here and tcell never sees them. mode is
// ui.graphics: "off" skips the probe, and "on" skips the Kitty query since the
// answer is assumed.
func probeTerminal(tty *navigationTty, mode string) terminalProbe {
	if mode == "off" {
		return terminalProbe{}
	}
	if err := tty.Start(); err != nil {
		return terminalProbe{}
	}
	defer func() {
		_ = tty.Drain()
		_ = tty.Stop()
	}()
	query := cellProbe + attributes
	if mode == "auto" {
		query = kittyProbe + query
	}
	if _, err := tty.Write([]byte(query)); err != nil {
		return terminalProbe{}
	}
	var parser probeParser
	deadline := time.Now().Add(probeTimeout)
	buf := make([]byte, 512)
	for !parser.done {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		n, err := tty.pump.read(buf, left)
		if err != nil {
			break
		}
		parser.feed(buf[:n])
	}
	return parser.terminalProbe
}
