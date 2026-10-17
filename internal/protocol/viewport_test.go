package protocol

import "testing"

func TestViewportOriginMatchesCursorFramingAndBounds(t *testing.T) {
	for _, tc := range []struct{ cursor, source, panel, want int }{
		{0, 100, 20, 0}, {40, 100, 20, 34}, {95, 100, 20, 80},
		{25, 30, 6, 23}, {29, 30, 6, 24}, {10, 20, 5, 9},
		{5, 10, 20, 0}, {0, 0, 20, 0}, {0, 1, 1, 0},
	} {
		if got := ViewportOrigin(tc.cursor, tc.source, tc.panel); got != tc.want {
			t.Fatalf("origin(%d,%d,%d)=%d, want %d", tc.cursor, tc.source, tc.panel, got, tc.want)
		}
	}
}
