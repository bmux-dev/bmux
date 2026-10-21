package graphics

import (
	"fmt"
	"strconv"
	"strings"
)

// Kitty graphics protocol values bmux understands. Keys and defaults follow
// https://sw.kovidgoyal.net/kitty/graphics-protocol/.
const (
	formatRGB  = 24
	formatRGBA = 32
	formatPNG  = 100

	// MaxAppID is the largest image id an app can use. Placeholder cells
	// carry the id in their 24-bit foreground color; the extra high byte
	// (a third diacritic) is not supported.
	MaxAppID = 0xFFFFFF

	// maxGridSide bounds a virtual placement's columns and rows.
	maxGridSide = 1000
)

// command is one parsed graphics control string: the key=value pairs before
// the ';' of an APC G sequence.
type command struct {
	action        byte // a: t, T, p, q, d (others are unsupported)
	quiet         int  // q: 0 reply always, 1 suppress OK, 2 suppress all
	id            uint64
	placement     uint64
	format        int  // f
	medium        byte // t: d, f, t, s
	compressed    bool // o=z
	more          bool // m=1
	width, height int  // s, v: pixel size of raw formats
	size, offset  int64
	cols, rows    int  // c, r
	virtual       bool // U=1
	del           byte // d, for a=d
	from, to      int64
}

func parseCommand(control []byte) (command, error) {
	cmd := command{action: 't', format: formatRGBA, medium: 'd', del: 'a'}
	var bad error
	for _, field := range strings.Split(string(control), ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok || len(key) != 1 {
			if field != "" && bad == nil {
				bad = fmt.Errorf("malformed key %q", field)
			}
			continue
		}
		letter := func() byte {
			if value == "" {
				return 0
			}
			return value[0]
		}
		var n int64
		switch key[0] {
		case 'a':
			cmd.action = letter()
			continue
		case 't':
			cmd.medium = letter()
			continue
		case 'o':
			cmd.compressed = letter() == 'z'
			continue
		case 'd':
			cmd.del = letter()
			continue
		}
		if !strings.ContainsRune("iqfmsvSOcrUpxy", rune(key[0])) {
			continue
		}
		var err error
		if n, err = strconv.ParseInt(value, 10, 64); err != nil && bad == nil {
			bad = fmt.Errorf("bad value for %s", key)
		}
		switch key[0] {
		case 'i':
			cmd.id = uint64(max(n, 0))
		case 'p':
			cmd.placement = uint64(max(n, 0))
		case 'q':
			cmd.quiet = int(n)
		case 'f':
			cmd.format = int(n)
		case 'm':
			cmd.more = n == 1
		case 's':
			cmd.width = int(n)
		case 'v':
			cmd.height = int(n)
		case 'S':
			cmd.size = n
		case 'O':
			cmd.offset = n
		case 'c':
			cmd.cols = int(n)
		case 'r':
			cmd.rows = int(n)
		case 'U':
			cmd.virtual = n == 1
		case 'x':
			cmd.from = n
		case 'y':
			cmd.to = n
		}
	}
	return cmd, bad
}

// reply formats the response an app expects for cmd, or "" when it asked for
// none: without an image id, or as quiet allows. msg is "OK" or a Kitty error
// such as "ENOENT:image not found".
func reply(cmd command, msg string) string {
	if cmd.id == 0 || cmd.quiet == 2 || (cmd.quiet == 1 && msg == "OK") {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b_Gi=")
	b.WriteString(strconv.FormatUint(cmd.id, 10))
	if cmd.placement != 0 {
		b.WriteString(",p=")
		b.WriteString(strconv.FormatUint(cmd.placement, 10))
	}
	b.WriteByte(';')
	b.WriteString(msg)
	b.WriteString("\x1b\\")
	return b.String()
}
