package vt

import (
	"fmt"
	"testing"
)

// DECRQCRA answers with DEC's checksum: the 16-bit two's complement of the
// sum of the rectangle's character codes, blanks counting as spaces.
func TestChecksumOfRectangularArea(t *testing.T) {
	reply := func(id, sum int) string { return fmt.Sprintf("\x1bP%d!~%04X\x1b\\", id, (0x10000-sum)&0xffff) }
	for _, tc := range []struct {
		name, input, query, want string
	}{
		{"one cell", "AB", "\x1b[7;0;1;1;1;1*y", reply(7, 'A')},
		{"a row", "AB", "\x1b[8;0;1;1;1;2*y", reply(8, 'A'+'B')},
		{"a blank counts as a space", "AB", "\x1b[9;0;1;3;1;3*y", reply(9, ' ')},
		{"a wide character once", "界", "\x1b[1;0;1;1;1;2*y", reply(1, '界')},
		// Origin mode: row 1 of the region (rows 2-3) is screen row 2.
		{"relative to the margins", "AB\r\nCD", "\x1b[2;3r\x1b[?6h\x1b[3;0;1;1;1;1*y", reply(3, 'C')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			term := newTestTerminal(t, 10, 4)
			read := replies(term)
			term.WriteString(tc.input + tc.query)
			if got := read(); got != tc.want {
				t.Fatalf("reply %q, want %q", got, tc.want)
			}
		})
	}
}
