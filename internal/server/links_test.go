package server

import "testing"

func TestFrameCarriesOSC8Links(t *testing.T) {
	p := shellless(20, 3)
	p.write("\x1b]8;;https://a.io/\x1b\\one\x1b]8;;\x1b\\ plain \x1b]8;id=x;https://b.io/\x1b\\two\x1b]8;;\x1b\\ \x1b]8;;https://a.io/\x1b\\again\x1b]8;;\x1b\\")
	f, _ := p.frame(0)
	if len(f.Links) != 2 || f.Links[0] != "https://a.io/" || f.Links[1] != "https://b.io/" {
		t.Fatalf("links %q", f.Links)
	}
	row := f.Rows[0]
	for x, want := range map[int]int{0: 1, 2: 1, 3: 0, 4: 0, 10: 2, 12: 2, 13: 0, 14: 1, 18: 1} {
		if row[x].Link != want {
			t.Errorf("column %d (%q): link %d, want %d", x, row[x].Text, row[x].Link, want)
		}
	}
	prev := p.preview(20, 3)
	if len(prev.Links) != 0 {
		t.Fatalf("preview carried links %q", prev.Links)
	}
}
