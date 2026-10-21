package graphics

import (
	"bytes"
	"strings"
)

// transfer is a chunked transmission (m=1 ... m=0) being assembled. Chunks
// after the first carry no keys but m, so the first chunk's command rules.
type transfer struct {
	cmd     command
	data    []byte
	discard bool // failed already; swallow the remaining chunks
}

// defaultCell is the cell size assumed to size a placement when no client has
// reported one; it is what omp itself assumes.
const defaultCellWidth, defaultCellHeight = 9, 18

// Handle runs one Kitty graphics command: the payload of an APC G sequence
// without the leading G. It returns the reply to write to the app, or "".
func (p *Pane) Handle(payload []byte) string {
	control, data, _ := bytes.Cut(payload, []byte{';'})
	cmd, parseErr := parseCommand(control)

	if t := p.pending; t != nil {
		return p.chunk(t, cmd, data)
	}
	if parseErr != nil {
		return reply(cmd, "EINVAL:"+parseErr.Error())
	}
	if cmd.more && cmd.medium == 'd' && (cmd.action == 't' || cmd.action == 'T' || cmd.action == 'q') {
		p.pending = &transfer{cmd: cmd}
		return p.chunk(p.pending, command{more: true}, data)
	}
	return p.run(cmd, data)
}

func (p *Pane) chunk(t *transfer, next command, data []byte) string {
	if !t.discard {
		// Base64 of the largest image the pane may hold, plus slack.
		if int64(len(t.data)+len(data)) > p.store.limits.Pane/3*4+8 {
			t.discard, t.data = true, nil
			if !next.more {
				p.pending = nil
			}
			return reply(t.cmd, "EFBIG:image too large")
		}
		t.data = append(t.data, data...)
	}
	if next.more {
		return ""
	}
	p.pending = nil
	if t.discard {
		return ""
	}
	t.cmd.more = false
	return p.run(t.cmd, t.data)
}

func (p *Pane) run(cmd command, data []byte) string {
	switch cmd.action {
	case 't', 'T', 'q':
		return p.transmit(cmd, data)
	case 'p':
		return p.display(cmd)
	case 'd':
		p.delete(cmd)
		return ""
	}
	return reply(cmd, "EINVAL:unsupported action")
}

func (p *Pane) supported() bool {
	return p.hooks.Supported != nil && p.hooks.Supported()
}

func (p *Pane) transmit(cmd command, data []byte) string {
	if cmd.action == 'q' && !p.supported() {
		return reply(cmd, "ENOTSUPPORTED:no attached terminal can display images")
	}
	if cmd.id > MaxAppID {
		return reply(cmd, "EINVAL:image id out of range")
	}
	img, errText := load(cmd, data, p.store.limits.Pane)
	if errText != "" {
		return reply(cmd, errText)
	}
	if cmd.action == 'q' {
		return reply(cmd, "OK")
	}
	if cmd.id == 0 {
		// An anonymous image can only be shown by a direct placement.
		return ""
	}
	info, errText := p.put(uint32(cmd.id), img)
	if errText != "" {
		return reply(cmd, errText)
	}
	if cmd.action == 'T' && cmd.virtual {
		cols, rows := p.grid(cmd, info)
		p.place(uint32(cmd.id), cols, rows)
	}
	return reply(cmd, "OK")
}

// display handles a=p. Only virtual placements (U=1), which placeholder cells
// show, are supported; others do nothing.
func (p *Pane) display(cmd command) string {
	if !cmd.virtual {
		return ""
	}
	if cmd.id == 0 || cmd.id > MaxAppID {
		return reply(cmd, "EINVAL:image id required")
	}
	info, ok := p.Image(uint32(cmd.id))
	if !ok {
		return reply(cmd, "ENOENT:image not found")
	}
	cols, rows := p.grid(cmd, info)
	p.place(uint32(cmd.id), cols, rows)
	return reply(cmd, "OK")
}

// grid is a virtual placement's size in cells: c and r when given, else the
// image's size over the client's cell size, keeping the aspect ratio when only
// one is given.
func (p *Pane) grid(cmd command, info Info) (cols, rows int) {
	cw, ch := defaultCellWidth, defaultCellHeight
	if p.hooks.CellPixels != nil {
		if w, h := p.hooks.CellPixels(); w > 0 && h > 0 {
			cw, ch = w, h
		}
	}
	cols, rows = cmd.cols, cmd.rows
	ceil := func(a, b int) int { return (a + b - 1) / b }
	switch {
	case cols <= 0 && rows <= 0:
		cols, rows = ceil(info.Width, cw), ceil(info.Height, ch)
	case rows <= 0:
		rows = ceil(info.Height*cols*cw, info.Width*ch)
	case cols <= 0:
		cols = ceil(info.Width*rows*ch, info.Height*cw)
	}
	return max(1, min(cols, maxGridSide)), max(1, min(rows, maxGridSide))
}

// delete handles a=d. Lowercase keys remove placements and keep the image
// data; uppercase free it too. Only deletes by id and range, and all, apply:
// the rest address direct placements, which bmux doesn't keep.
func (p *Pane) delete(cmd command) {
	free := cmd.del >= 'A' && cmd.del <= 'Z'
	switch strings.ToLower(string(cmd.del)) {
	case "a":
		p.unplace(free, func(uint32) bool { return true })
	case "i":
		p.unplace(free, func(appID uint32) bool { return uint64(appID) == cmd.id })
	case "r":
		p.unplace(free, func(appID uint32) bool { return int64(appID) >= cmd.from && int64(appID) <= cmd.to })
	}
}
