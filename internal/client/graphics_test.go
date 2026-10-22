package client

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/josephferrero/bmux/internal/protocol"
)

func TestProbeParserReadsRepliesAmongKeystrokes(t *testing.T) {
	tests := []struct {
		name, input string
		want        terminalProbe
	}{
		{"kitty terminal", "\x1b_Gi=31;OK\x1b\\\x1b[6;18;9t\x1b[?62;4c", terminalProbe{kitty: true, cellWidth: 9, cellHeight: 18, done: true}},
		{"no kitty support", "\x1b[?62c", terminalProbe{done: true}},
		{"kitty error", "\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c", terminalProbe{done: true}},
		{"another image id", "\x1b_Gi=311;OK\x1b\\\x1b[?62c", terminalProbe{done: true}},
		{"BEL terminated", "\x1b_Gi=31;OK\x07\x1b[?1;2c", terminalProbe{kitty: true, done: true}},
		{"typed keys around replies", "ab\x1b[A\x1b_Gi=31;OK\x1b\\jk\x1b[6;20;10tq\x1b[?62c", terminalProbe{kitty: true, cellWidth: 10, cellHeight: 20, done: true}},
		{"absurd cell size", "\x1b[6;20000;10t\x1b[?62c", terminalProbe{done: true}},
		{"other window reports", "\x1b[8;24;80t\x1b[4;480;720t\x1b[?62c", terminalProbe{done: true}},
		{"colors, BEL and ST terminated", "\x1b]10;rgb:cdcd/d6d6/f4f4\x07\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\\x1b[?62c", terminalProbe{done: true, foreground: "#cdd6f4", background: "#1e1e2e"}},
		{"two-digit colors", "\x1b]11;rgb:11/22/33\x07\x1b[?62c", terminalProbe{done: true, background: "#112233"}},
		{"unparsable and other OSCs", "\x1b]11;nonsense\x07\x1b]12;rgb:ffff/0000/0000\x07\x1b]0;title\x07\x1b[?62c", terminalProbe{done: true}},
		{"no attributes reply yet", "\x1b_Gi=31;OK\x1b\\", terminalProbe{kitty: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var whole, bytewise probeParser
			whole.feed([]byte(tt.input))
			for i := range tt.input {
				bytewise.feed([]byte{tt.input[i]})
			}
			if whole.terminalProbe != tt.want {
				t.Errorf("one read: %+v, want %+v", whole.terminalProbe, tt.want)
			}
			if bytewise.terminalProbe != tt.want {
				t.Errorf("fragmented: %+v, want %+v", bytewise.terminalProbe, tt.want)
			}
		})
	}
}

// terminalLog is what a fake terminal received from the image sender.
type terminalLog struct{ writes [][]byte }

func (l *terminalLog) write(b []byte) error {
	l.writes = append(l.writes, append([]byte(nil), b...))
	return nil
}

// graphicsCommand is the only shape of sequence the client may send an image
// terminal: transmissions of pixels inline in the sequence, placements and
// deletes by id. Nothing names a file or shared memory (t=f, t=t, t=s).
var graphicsCommand = regexp.MustCompile(`^\x1b_G(a=t,f=100,t=d,i=\d+,q=2,m=[01]|m=[01],q=2|a=p,U=1,i=\d+,c=\d+,r=\d+,q=2|a=d,d=I,i=\d+,q=2)(;[A-Za-z0-9+/=]*)?\x1b\\$`)

// commands splits writes into graphics commands, checking each is allowed and
// that every write is wrapped in synchronized output.
func (l *terminalLog) commands(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, w := range l.writes {
		s := string(w)
		if !strings.HasPrefix(s, syncBegin) || !strings.HasSuffix(s, syncEnd) {
			t.Fatalf("write not in synchronized output: %.60q", s)
		}
		s = strings.TrimSuffix(strings.TrimPrefix(s, syncBegin), syncEnd)
		for len(s) > 0 {
			end := strings.Index(s, "\x1b\\")
			if end < 0 {
				t.Fatalf("unterminated command: %.60q", s)
			}
			cmd := s[:end+2]
			if !graphicsCommand.MatchString(cmd) {
				t.Fatalf("command outside the allowed set: %.80q", cmd)
			}
			out = append(out, cmd)
			s = s[end+2:]
		}
	}
	return out
}

func testPNG(t testing.TB, w, h int, seed int64) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	_, _ = rand.New(rand.NewSource(seed)).Read(img.Pix)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type senderRig struct {
	*imageSender
	log     *terminalLog
	results chan imageResult
	pngs    map[uint32][]byte
	mu      sync.Mutex
	fetched []uint32
}

func newSenderRig(t *testing.T, pngs map[uint32][]byte) *senderRig {
	r := &senderRig{log: &terminalLog{}, results: make(chan imageResult, 16), pngs: pngs}
	r.imageSender = newImageSender(r.log.write, func(id uint32) ([]byte, error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.fetched = append(r.fetched, id)
		return r.pngs[id], nil
	}, r.results, make(chan struct{}))
	return r
}

// show redraws with images on screen and waits for their fetches.
func (r *senderRig) show(t *testing.T, images ...protocol.Image) bool {
	t.Helper()
	r.want(images)
	for len(r.loading) > 0 {
		select {
		case res := <-r.results:
			r.accept(res)
		case <-time.After(2 * time.Second):
			t.Fatal("fetch never finished")
		}
	}
	more, err := r.flush(images)
	if err != nil {
		t.Fatal(err)
	}
	return more
}

func image5(cols, rows int) protocol.Image {
	return protocol.Image{ID: 5, Width: 40, Height: 20, Cols: cols, Rows: rows}
}

func TestEachImageIsSentOncePerConnection(t *testing.T) {
	data := testPNG(t, 64, 64, 1)
	r := newSenderRig(t, map[uint32][]byte{5: data})
	for range 3 {
		r.show(t, image5(4, 2))
	}
	cmds := r.log.commands(t)
	if len(r.fetched) != 1 {
		t.Fatalf("fetched %d times", len(r.fetched))
	}
	if len(cmds) < 3 || !strings.HasPrefix(cmds[0], "\x1b_Ga=t,f=100,t=d,i=5,q=2,m=") {
		t.Fatalf("first command %.60q", cmds[0])
	}
	if got := cmds[len(cmds)-1]; got != "\x1b_Ga=p,U=1,i=5,c=4,r=2,q=2\x1b\\" {
		t.Fatalf("last command %q, want the virtual placement", got)
	}
	var encoded strings.Builder
	for i, c := range cmds[:len(cmds)-1] {
		payload := c[strings.Index(c, ";")+1 : len(c)-2]
		if len(payload) > transmitChunk {
			t.Errorf("chunk %d is %d bytes", i, len(payload))
		}
		wantMore := "m=1"
		if i == len(cmds)-2 {
			wantMore = "m=0"
		}
		if !strings.Contains(c[:strings.Index(c, ";")], wantMore) {
			t.Errorf("chunk %d: %.40q, want %s", i, c, wantMore)
		}
		encoded.WriteString(payload)
	}
	if got, err := base64.StdEncoding.DecodeString(encoded.String()); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("transmitted bytes differ from the image: %v", err)
	}
	if len(r.log.writes) != 1 {
		t.Errorf("%d writes; redraws with nothing new must write nothing", len(r.log.writes))
	}

	// A lost connection deletes what the terminal holds and starts over.
	if err := r.reset(); err != nil {
		t.Fatal(err)
	}
	r.show(t, image5(4, 2))
	cmds = r.log.commands(t)
	if len(r.fetched) != 2 {
		t.Fatalf("after reset fetched %d times, want 2", len(r.fetched))
	}
	deletes := 0
	for _, c := range cmds {
		if c == "\x1b_Ga=d,d=I,i=5,q=2\x1b\\" {
			deletes++
		}
	}
	if deletes != 1 {
		t.Fatalf("reset deleted image 5 %d times", deletes)
	}
}

func TestPlacementIsResentWhenTheGridChanges(t *testing.T) {
	r := newSenderRig(t, map[uint32][]byte{5: testPNG(t, 8, 8, 2)})
	r.show(t, image5(4, 2))
	before := len(r.log.writes)
	r.show(t, image5(6, 3))
	cmds := r.log.commands(t)
	if len(r.log.writes) != before+1 || cmds[len(cmds)-1] != "\x1b_Ga=p,U=1,i=5,c=6,r=3,q=2\x1b\\" {
		t.Fatalf("grid change wrote %v", cmds[len(cmds)-1])
	}
	if len(r.fetched) != 1 {
		t.Fatal("re-placing refetched the image")
	}
}

func TestLargeImagesAreSpreadAcrossRedraws(t *testing.T) {
	pngs := map[uint32][]byte{}
	var images []protocol.Image
	for id := uint32(1); id <= 3; id++ {
		pngs[id] = testPNG(t, 500, 500, int64(id)) // ~1 MB of incompressible pixels
		images = append(images, protocol.Image{ID: int(id), Width: 500, Height: 500, Cols: 10, Rows: 5})
	}
	r := newSenderRig(t, pngs)
	rounds := 0
	for more := true; more; rounds++ {
		more = r.show(t, images...)
		if rounds > 5 {
			t.Fatal("never finished")
		}
	}
	if rounds < 3 {
		t.Fatalf("sent three ~1 MB images in %d redraws", rounds)
	}
	r.log.commands(t)
	if len(r.sent) != 3 {
		t.Fatalf("sent %d images", len(r.sent))
	}
}

func TestLeastRecentlyShownImagesAreDeletedPastTheLimit(t *testing.T) {
	pngs := map[uint32][]byte{1: testPNG(t, 30, 30, 1), 2: testPNG(t, 30, 30, 2), 3: testPNG(t, 30, 30, 3)}
	r := newSenderRig(t, pngs)
	r.maxBytes = len(pngs[1])*2 + len(pngs[1])/2
	img := func(id int) protocol.Image { return protocol.Image{ID: id, Width: 30, Height: 30, Cols: 2, Rows: 2} }
	r.show(t, img(1))
	r.show(t, img(2))
	r.show(t, img(1)) // 1 is newer than 2 again
	r.show(t, img(3), img(1))
	r.log.commands(t)
	if r.sent[2] != nil || r.sent[1] == nil || r.sent[3] == nil {
		t.Fatalf("kept %v; want 2 (least recently shown) deleted", r.sent)
	}
	found := false
	for _, w := range r.log.writes {
		found = found || strings.Contains(string(w), "\x1b_Ga=d,d=I,i=2,q=2\x1b\\")
	}
	if !found {
		t.Fatal("image 2 not deleted from the terminal")
	}
	if r.bytes > r.maxBytes {
		t.Fatalf("%d bytes kept, limit %d", r.bytes, r.maxBytes)
	}
	// Coming back, it's fetched and sent again.
	r.show(t, img(2), img(1), img(3))
	if len(r.fetched) != 4 {
		t.Fatalf("fetches %v", r.fetched)
	}
}

func TestFetchesFromBeforeAResetAreIgnored(t *testing.T) {
	r := newSenderRig(t, map[uint32][]byte{5: testPNG(t, 8, 8, 3)})
	r.want([]protocol.Image{image5(1, 1)})
	stale := <-r.results
	if err := r.reset(); err != nil {
		t.Fatal(err)
	}
	r.accept(stale)
	if _, ok := r.ready[5]; ok || r.loading[5] {
		t.Fatal("a fetch from the old server was accepted")
	}
	r.show(t, image5(1, 1))
	if len(r.fetched) != 2 || r.sent[5] == nil {
		t.Fatalf("fetched %v, sent %v", r.fetched, r.sent)
	}
}

func TestFailedFetchesRetryLater(t *testing.T) {
	r := newSenderRig(t, nil)
	r.show(t, image5(1, 1)) // the server has no such image
	if len(r.sent) != 0 || len(r.log.writes) != 0 {
		t.Fatal("sent an image that failed to load")
	}
	r.want([]protocol.Image{image5(1, 1)})
	if len(r.fetched) != 1 {
		t.Fatal("retried immediately")
	}
	r.failedAt[5] = time.Now().Add(-retryFetch)
	r.pngs = map[uint32][]byte{5: testPNG(t, 8, 8, 4)}
	r.show(t, image5(1, 1))
	if r.sent[5] == nil {
		t.Fatal("never retried")
	}
}

func TestPlaceholderMarkersFillFirstRowOnly(t *testing.T) {
	cell := func(id uint32) protocol.Cell {
		return protocol.Cell{Text: placeholder + "\u0305\u0305", Width: 1, FG: "#" + strings.Repeat("0", 4) + string("0123456789abcdef"[id>>4]) + string("0123456789abcdef"[id&15])}
	}
	row := func(ids ...uint32) []protocol.Cell {
		var out []protocol.Cell
		for _, id := range ids {
			if id == 0 {
				out = append(out, protocol.Cell{Text: "x", Width: 1})
			} else {
				out = append(out, cell(id))
			}
		}
		return out
	}
	big := make([]uint32, 16)
	for i := range big {
		big[i] = 1
	}
	f := &protocol.TerminalFrame{
		Rows:   [][]protocol.Cell{row(append([]uint32{0}, big...)...), row(big...), row(2, 2, 2, 2, 2, 2, 2, 2), row(3, 3)},
		Images: []protocol.Image{{ID: 1, Width: 800, Height: 600}, {ID: 2, Width: 10, Height: 20}, {ID: 3, Width: 5, Height: 5}},
	}
	m := placeholderMarkers(f)
	label := func(x, y, n int) string {
		var s strings.Builder
		for i := 0; i < n; i++ {
			s.WriteString(m.at(x+i, y))
		}
		return s.String()
	}
	if got := label(1, 0, 16); got != "[image 800×600] " {
		t.Errorf("image 1 first row: %q", got)
	}
	if got := label(0, 1, 16); strings.TrimSpace(got) != "" {
		t.Errorf("image 1 second row must be blank: %q", got)
	}
	if got := label(0, 2, 8); got != "[image] " {
		t.Errorf("image 2 (long label won't fit 8 cells): %q", got)
	}
	if got := label(0, 3, 2); got != "  " {
		t.Errorf("image 3 (nothing fits 2 cells): %q", got)
	}
	if got := m.at(0, 0); got != " " {
		t.Errorf("a plain cell is not a marker: %q", got)
	}
	if placeholderMarkers(&protocol.TerminalFrame{Rows: f.Rows}) != nil {
		t.Error("frames without images need no markers")
	}
}

func imageFrame() *protocol.Frame {
	cell := protocol.Cell{Text: placeholder + "\u0305\u0305", Width: 1, FG: "#000009"}
	row := []protocol.Cell{{Text: "a", Width: 1}, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell, cell}
	return &protocol.Frame{Name: "api", Focused: 1, Panes: []protocol.PaneFrame{{ID: 1, Width: 20, Height: 4, Terminal: &protocol.TerminalFrame{
		Rows:   [][]protocol.Cell{row},
		Images: []protocol.Image{{ID: 9, Width: 320, Height: 100, Cols: 16, Rows: 1}},
	}}}}
}

func imageScreen(t *testing.T) tcell.SimulationScreen {
	t.Helper()
	t.Setenv("NO_COLOR", "") // images need exact colors, whatever the developer's environment says
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Fini)
	screen.SetSize(30, 8)
	return screen
}

func TestImageCellsKeepTheirExactColorWhenImagesAreOn(t *testing.T) {
	screen := imageScreen(t)
	log := &terminalLog{}
	u := &ui{screen: screen, graphics: graphicsEnv{write: log.write}}
	u.settings.UI.Graphics = "on"
	u.applyFrame(imageFrame())
	u.draw()
	text, style, _ := screen.Get(1, paneOriginY)
	if text != placeholder+"\u0305\u0305" {
		t.Fatalf("image cell drawn as %q", text)
	}
	if fg, _, _ := style.Decompose(); fg != tcell.NewRGBColor(0, 0, 9) {
		t.Fatalf("image cell foreground %v, want the exact id color", fg)
	}
	// A selection across the image must not recolor (and so hide) it.
	u.selection = selection{active: true, start: point{0, 0}, end: point{10, 0}}
	u.selection.remember(u.frame)
	u.draw()
	if _, style, _ = screen.Get(1, paneOriginY); func() bool { fg, _, _ := style.Decompose(); return fg != tcell.NewRGBColor(0, 0, 9) }() {
		t.Fatal("selection recolored an image cell")
	}
}

func TestImageCellsAreBlankWithAMarkerWhenImagesAreOff(t *testing.T) {
	for _, mode := range []string{"off", "auto", "on"} {
		t.Run(mode, func(t *testing.T) {
			screen := imageScreen(t)
			env := graphicsEnv{write: (&terminalLog{}).write} // auto: probe found no support
			if mode == "on" {
				env.write = nil // nothing to draw images on
			}
			u := &ui{screen: screen, graphics: env}
			u.settings.UI.Graphics = mode
			u.applyFrame(imageFrame())
			u.draw()
			var row strings.Builder
			for x := 0; x < 17; x++ {
				text, style, _ := screen.Get(x, paneOriginY)
				row.WriteString(text)
				if fg, _, _ := style.Decompose(); x > 0 && fg == tcell.NewRGBColor(0, 0, 9) {
					t.Fatalf("cell %d still carries the image id color", x)
				}
			}
			if got := row.String(); got != "a[image 320×100] " {
				t.Fatalf("row %q, want the marker after the plain cell", got)
			}
			if strings.Contains(row.String(), placeholder) {
				t.Fatal("placeholder rune reached the screen")
			}
		})
	}
}

func TestGraphicsReport(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TCELL_TRUECOLOR", "")
	cells := func() (int, int) { return 9, 18 }
	tests := []struct {
		name string
		mode string
		env  graphicsEnv
		want *protocol.Graphics
	}{
		{"detected", "auto", graphicsEnv{probe: terminalProbe{kitty: true}, cellPixels: cells, write: func([]byte) error { return nil }}, &protocol.Graphics{Kitty: true, CellWidth: 9, CellHeight: 18}},
		{"not detected still reports cells", "auto", graphicsEnv{cellPixels: cells, write: func([]byte) error { return nil }}, &protocol.Graphics{CellWidth: 9, CellHeight: 18}},
		{"off", "off", graphicsEnv{probe: terminalProbe{kitty: true}, cellPixels: cells, write: func([]byte) error { return nil }}, &protocol.Graphics{CellWidth: 9, CellHeight: 18}},
		{"forced on", "on", graphicsEnv{write: func([]byte) error { return nil }}, &protocol.Graphics{Kitty: true}},
		{"nothing known", "auto", graphicsEnv{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &ui{graphics: tt.env}
			u.settings.UI.Graphics = tt.mode
			got := u.graphicsReport()
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("report %+v, want %+v", got, tt.want)
			}
		})
	}
	for name, value := range map[string]string{"TCELL_TRUECOLOR": "disable", "NO_COLOR": "1"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			u := &ui{graphics: graphicsEnv{probe: terminalProbe{kitty: true}, write: func([]byte) error { return nil }}}
			if u.graphicsOn() {
				t.Fatal("images on although tcell would not send the ids in the colors exactly")
			}
		})
	}
}

func TestDrawSendsShownImagesBeforeShowing(t *testing.T) {
	screen := imageScreen(t)
	log := &terminalLog{}
	u := &ui{screen: screen, graphics: graphicsEnv{probe: terminalProbe{kitty: true}, write: log.write}, done: make(chan struct{})}
	u.settings.UI.Graphics = "auto"
	u.imageResults = make(chan imageResult, 4)
	u.imageWake = make(chan struct{}, 1)
	u.images = newImageSender(log.write, func(uint32) ([]byte, error) { return testPNG(t, 16, 16, 9), nil }, u.imageResults, u.done)
	u.applyFrame(imageFrame())
	u.draw()
	if len(log.writes) != 0 {
		t.Fatal("wrote image data before the fetch finished")
	}
	u.images.accept(<-u.imageResults)
	u.draw()
	cmds := log.commands(t)
	if len(cmds) == 0 || !strings.HasPrefix(cmds[0], "\x1b_Ga=t,f=100,t=d,i=9,") || cmds[len(cmds)-1] != "\x1b_Ga=p,U=1,i=9,c=16,r=1,q=2\x1b\\" {
		t.Fatalf("commands %.80q", cmds)
	}
}

func TestSelectionCopiesAndSearchSkipImageCells(t *testing.T) {
	ph := protocol.Cell{Text: placeholder + "\u0305\u0305", Width: 1, FG: "#000009"}
	row := []protocol.Cell{{Text: "a", Width: 1}, {Text: "b", Width: 1}, ph, ph, {Text: "c", Width: 1}, {Text: "d", Width: 1}}
	frame := &protocol.TerminalFrame{Rows: [][]protocol.Cell{row, {ph, ph, ph}}}
	sel := selection{active: true, start: point{0, 0}, end: point{5, 1}}
	sel.remember(frame)
	got := sel.text()
	if strings.ContainsRune(got, 0x10EEEE) || got != "ab  cd\n" {
		t.Errorf("copied %q, want the image cells as blanks", got)
	}
	matches := searchMatches(row, "b  c")
	hit := 0
	for _, m := range matches {
		if m {
			hit++
		}
	}
	if hit != 4 {
		t.Errorf("search across the image highlighted %d cells, want 4: %v", hit, matches)
	}
	for _, m := range searchMatches(row, placeholder) {
		if m {
			t.Fatal("search matched the placeholder rune")
		}
	}
	if !isBlank(ph) {
		t.Error("an image cell counts as text for word and prompt motions")
	}
}
