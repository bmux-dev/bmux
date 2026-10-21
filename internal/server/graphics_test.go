package server

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/josephferrero/bmux/internal/graphics"
	"github.com/josephferrero/bmux/internal/protocol"
)

// testImage is an 8x8 PNG whose pixels all have the given value.
func testImage(t *testing.T, shade uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for i := range img.Pix {
		img.Pix[i] = shade
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func pngAPC(t *testing.T, control string, shade uint8) string {
	t.Helper()
	return "\x1b_G" + control + ";" + base64.StdEncoding.EncodeToString(testImage(t, shade)) + "\x1b\\"
}

func TestPaneStoresImagesAndAnswersFromTheEmulator(t *testing.T) {
	out := "replies"
	p, dir := scriptedPane(t, "stty raw -echo; echo ready > ready; head -c 21 > "+out+"; sleep 30")
	awaitFile(t, filepath.Join(dir, "ready"))
	if err := p.setGraphics(true, 9, 20); err != nil {
		t.Fatal(err)
	}
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=9,c=2,r=1,q=2", 7)))
	// The app's reply bytes travel emulator -> bridge -> PTY -> shell input.
	p.apply([]byte("\x1b_Ga=q,i=31,s=1,v=1,t=d,f=24;AAAA\x1b\\\x1b[16t"))

	info, ok := p.graphics.Image(9)
	if !ok || info.Cols != 2 || info.Rows != 1 || info.Width != 8 {
		t.Fatalf("stored image: %+v %v", info, ok)
	}
	if got, want := awaitFile(t, filepath.Join(dir, out)), "\x1b_Gi=31;OK\x1b\\\x1b[6;20;9t"; got != want {
		t.Fatalf("app received %q, want %q", got, want)
	}

	rows, cols, err := sizePTY(p.pty)
	if err != nil {
		t.Fatal(err)
	}
	var size *unix.Winsize
	conn, _ := p.pty.SyscallConn()
	_ = conn.Control(func(fd uintptr) { size, err = unix.IoctlGetWinsize(int(fd), unix.TIOCGWINSZ) })
	if err != nil || int(size.Xpixel) != cols*9 || int(size.Ypixel) != rows*20 {
		t.Fatalf("pty pixel size %dx%d for %dx%d cells of 9x20 (%v)", size.Xpixel, size.Ypixel, cols, rows, err)
	}

	p.apply([]byte("\x1b[18t")) // answered by size alone
	p.closeIO()
	if _, ok := p.graphics.Image(9); ok {
		t.Fatal("closed pane keeps its images")
	}
	if _, ok := images.Data(info.ID); ok {
		t.Fatal("closed pane's image is still served")
	}
}

func TestQueryIsRefusedWithoutAGraphicsClient(t *testing.T) {
	p, _ := scriptedPane(t, "sleep 30")
	p.mu.Lock()
	reply := p.graphics.Handle([]byte("a=q,i=31,s=1,v=1,t=d,f=24;AAAA"))
	p.mu.Unlock()
	if want := "\x1b_Gi=31;ENOTSUPPORTED:no attached terminal can display images\x1b\\"; reply != want {
		t.Fatalf("reply %q, want %q", reply, want)
	}
}

func TestEvictionPrefersImagesNoCellShows(t *testing.T) {
	// Room for two images of this size, not three.
	size := int64(len(testImage(t, 0)))
	store := graphics.NewStore(graphics.Limits{Pane: 1 << 20, Total: size*2 + size/2})
	p, _ := scriptedPane(t, "sleep 30")
	p.trackGraphics(store)
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=1,c=1,r=1,q=2", 1) + "\x1b[38;2;0;0;1m\U0010EEEE\x1b[0m"))
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=2,c=1,r=1,q=2", 2)))
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=3,c=1,r=1,q=2", 3)))
	for id, want := range map[uint32]bool{1: true, 2: false, 3: true} {
		if _, ok := p.graphics.Image(id); ok != want {
			t.Errorf("image %d stored=%v, want %v (image 1 is the only one a cell shows)", id, ok, want)
		}
	}
}

func TestEvictionDoesNotWaitForABusyPane(t *testing.T) {
	size := int64(len(testImage(t, 0)))
	store := graphics.NewStore(graphics.Limits{Pane: 1 << 20, Total: size*2 + size/2})
	busy, _ := scriptedPane(t, "sleep 30")
	busy.trackGraphics(store)
	busy.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=1,c=1,r=1,q=2", 0)))
	active, _ := scriptedPane(t, "sleep 30")
	active.trackGraphics(store)
	active.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=1,c=1,r=1,q=2", 0)))

	// busy is mid-output: its lock is taken. Evicting for active's next image
	// must neither block on it nor take its (possibly shown) image first.
	busy.mu.Lock()
	done := make(chan struct{})
	go func() {
		active.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=2,c=1,r=1,q=2", 0)))
		close(done)
	}()
	<-done
	busy.mu.Unlock()
	if _, ok := busy.graphics.Image(1); !ok {
		t.Error("evicted the busy pane's image while another image was unshown")
	}
	if _, ok := active.graphics.Image(1); ok {
		t.Error("kept the unshown image")
	}
}

const placeholderCell = "\U0010EEEE\u0305\u0305"

func firstRow(f *protocol.TerminalFrame) []protocol.Cell { return f.Rows[0] }

func TestFramesRemapPlaceholdersToServerWideIDs(t *testing.T) {
	a, _ := scriptedPane(t, "sleep 30")
	b, _ := scriptedPane(t, "sleep 30")
	// Both programs call their image 9, with different pixels.
	a.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=9,c=2,r=1,q=2", 1)))
	b.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=9,c=3,r=1,q=2", 2)))
	cells := "\x1b[38;2;0;0;9m" + placeholderCell + "\x1b[38;5;9m" + placeholderCell + "\x1b[0m"
	a.apply([]byte(cells))
	b.apply([]byte("\x1b[38;2;0;0;9m" + placeholderCell + "\x1b[0m"))

	infoA, _ := a.graphics.Image(9)
	infoB, _ := b.graphics.Image(9)
	if infoA.ID == infoB.ID {
		t.Fatalf("panes share server id %d", infoA.ID)
	}
	fa, _ := a.frame(0)
	fb, _ := b.frame(0)
	if got, want := firstRow(fa)[0].FG, fmt.Sprintf("#%06x", infoA.ID); got != want {
		t.Errorf("pane a placeholder foreground %q, want %q", got, want)
	}
	if got, want := firstRow(fb)[0].FG, fmt.Sprintf("#%06x", infoB.ID); got != want {
		t.Errorf("pane b placeholder foreground %q, want %q", got, want)
	}
	if got := firstRow(fa)[0].Text; got != placeholderCell {
		t.Errorf("placeholder text %q, want it kept with its diacritics", got)
	}
	// 256-color id 9 isn't image 9 here: pane a has no image 9 in that form...
	// it does: the palette index is the id, so it is remapped too, to exact color.
	if got, want := firstRow(fa)[1].FG, fmt.Sprintf("#%06x", infoA.ID); got != want {
		t.Errorf("palette-form placeholder foreground %q, want %q", got, want)
	}
	if len(fa.Images) != 1 || fa.Images[0] != (protocol.Image{ID: int(infoA.ID), Width: 8, Height: 8, Cols: 2, Rows: 1}) {
		t.Errorf("pane a frame images %+v", fa.Images)
	}
	if len(fb.Images) != 1 || fb.Images[0].Cols != 3 {
		t.Errorf("pane b frame images %+v", fb.Images)
	}
}

func TestFramesBlankPlaceholdersWithoutAnImage(t *testing.T) {
	p, _ := scriptedPane(t, "sleep 30")
	p.apply([]byte("\x1b[38;2;0;0;4m" + placeholderCell + "\x1b[0m"))
	p.apply([]byte(pngAPC(t, "a=t,f=100,i=5,q=2", 1) + "\x1b[38;2;0;0;5m" + placeholderCell + "\x1b[0m"))
	f, _ := p.frame(0)
	for x, why := range []string{"never transmitted", "transmitted but not placed"} {
		if c := f.Rows[0][x]; c.Text != " " || c.FG != "" || c.Width != 1 {
			t.Errorf("placeholder of an image that was %s: %+v", why, c)
		}
	}
	if len(f.Images) != 0 {
		t.Errorf("frame lists images %+v for blank cells", f.Images)
	}
	p.apply([]byte("\x1b[1;1H\x1b[38;2;0;0;4mplain\x1b[0m"))
	f, _ = p.frame(0)
	if c := f.Rows[0][0]; c.Text != "p" || c.FG != "#000004" {
		t.Errorf("ordinary text with an image-id color was changed: %+v", c)
	}
}

func TestEvictedImagesDrawBlank(t *testing.T) {
	size := int64(len(testImage(t, 0)))
	store := graphics.NewStore(graphics.Limits{Pane: size + size/2, Total: size + size/2})
	p, _ := scriptedPane(t, "sleep 30")
	p.trackGraphics(store)
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=1,c=1,r=1,q=2", 1) + "\x1b[38;2;0;0;1m" + placeholderCell + "\x1b[0m"))
	if f, _ := p.frame(0); firstRow(f)[0].Text != placeholderCell {
		t.Fatal("image not shown before eviction")
	}
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=2,c=1,r=1,q=2", 2)))
	f, _ := p.frame(0)
	if c := firstRow(f)[0]; c.Text != " " || len(f.Images) != 0 {
		t.Errorf("cell of an evicted image: %+v, images %+v", c, f.Images)
	}
}

func TestPreviewFramesRemapToo(t *testing.T) {
	p, _ := scriptedPane(t, "sleep 30")
	p.apply([]byte(pngAPC(t, "a=T,U=1,f=100,i=3,c=1,r=1,q=2", 1) + "\x1b[38;2;0;0;3m" + placeholderCell + "\x1b[0m"))
	info, _ := p.graphics.Image(3)
	f := p.preview(10, 3)
	if got, want := firstRow(f)[0].FG, fmt.Sprintf("#%06x", info.ID); got != want || len(f.Images) != 1 {
		t.Errorf("preview foreground %q (want %q), images %+v", got, want, f.Images)
	}
}

func TestImageDataIsServedInChunks(t *testing.T) {
	p, _ := scriptedPane(t, "sleep 30")
	// Large enough for several chunks: random pixels do not compress.
	raw := make([]byte, 300*300*4)
	_, _ = rand.New(rand.NewSource(1)).Read(raw)
	p.apply([]byte("\x1b_Ga=T,U=1,f=32,s=300,v=300,i=1,c=1,r=1,q=2;" + base64.StdEncoding.EncodeToString(raw) + "\x1b\\"))
	info, ok := p.graphics.Image(1)
	if !ok {
		t.Fatal("image not stored")
	}
	srv := New("/bin/sh")
	var got []byte
	chunks := 0
	for {
		resp := srv.request(protocol.Request{Op: "image-data", ImageID: info.ID, Offset: len(got)})
		if resp.Error != "" {
			t.Fatal(resp.Error)
		}
		c := resp.ImageChunk
		if c.ID != int(info.ID) || c.Offset != len(got) || len(c.Data) > protocol.MaxImageChunk {
			t.Fatalf("bad chunk %d/%d at %d", len(c.Data), c.Total, len(got))
		}
		got = append(got, c.Data...)
		chunks++
		if len(got) == c.Total {
			break
		}
	}
	if chunks < 2 {
		t.Fatalf("image arrived in %d chunk; the test image is too small to prove chunking", chunks)
	}
	img, err := png.Decode(bytes.NewReader(got))
	if err != nil || img.Bounds().Dx() != 300 {
		t.Fatalf("reassembled image: %v %v", img, err)
	}
	for _, r := range []protocol.Request{
		{Op: "image-data", ImageID: info.ID + 1000000},
		{Op: "image-data", ImageID: info.ID, Offset: len(got) + 1},
		{Op: "image-data", ImageID: info.ID, Offset: -1},
	} {
		if resp := srv.request(r); resp.Error == "" {
			t.Errorf("%+v succeeded", r)
		}
	}
}

func TestSessionAppliesTheBestAttachedDisplay(t *testing.T) {
	s, err := newSession("graphics", t.TempDir(), "/bin/sh", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.stop)
	p := s.panes[1]
	state := func() (bool, int, int) {
		p.mu.Lock()
		defer p.mu.Unlock()
		w, h := p.term.CellPixels()
		return p.graphicsOK.Load(), w, h
	}
	plain, shiny := &clientGraphics{}, &clientGraphics{}

	s.reportGraphics(plain, &protocol.Graphics{CellWidth: 7, CellHeight: 14})
	if ok, w, h := state(); ok || w != 7 || h != 14 {
		t.Fatalf("one plain client: supported=%v cell %dx%d", ok, w, h)
	}
	s.reportGraphics(shiny, &protocol.Graphics{Kitty: true, CellWidth: 9, CellHeight: 18})
	if ok, w, h := state(); !ok || w != 9 || h != 18 {
		t.Fatalf("with a graphics client: supported=%v cell %dx%d, want it and its size", ok, w, h)
	}
	s.reportGraphics(plain, &protocol.Graphics{CellWidth: 8, CellHeight: 16})
	if _, w, h := state(); w != 9 || h != 18 {
		t.Fatalf("a newer plain report displaced the graphics client's cell size: %dx%d", w, h)
	}
	s.reportGraphics(shiny, &protocol.Graphics{Kitty: true, CellWidth: 9999, CellHeight: 18})
	if ok, w, h := state(); !ok || w != 8 || h != 16 {
		t.Fatalf("an absurd cell size was believed: supported=%v cell %dx%d", ok, w, h)
	}
	s.dropGraphics(shiny)
	if ok, _, _ := state(); ok {
		t.Fatal("images still supported after the graphics client left")
	}
	s.dropGraphics(plain)
	if _, w, h := state(); w != 8 || h != 16 {
		t.Fatalf("last known cell size lost when the last client left: %dx%d", w, h)
	}

	// A pane split later starts with what the clients said.
	s.reportGraphics(shiny, &protocol.Graphics{Kitty: true, CellWidth: 10, CellHeight: 20})
	if err := s.input(protocol.Request{Op: "split", Direction: "horizontal"}); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	var created *pane
	for id, pn := range s.panes {
		if id != 1 {
			created = pn
		}
	}
	s.mu.Unlock()
	created.mu.Lock()
	w, h := created.term.CellPixels()
	created.mu.Unlock()
	if !created.graphicsOK.Load() || w != 10 || h != 20 {
		t.Fatalf("new pane: supported=%v cell %dx%d", created.graphicsOK.Load(), w, h)
	}
}

func TestAppSequencesNeverReachFramesOrFiles(t *testing.T) {
	p, dir := scriptedPane(t, "sleep 30")
	secret := filepath.Join(dir, "secret.png")
	if err := os.WriteFile(secret, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	for _, seq := range []string{
		"\x1b_Ga=T,t=f,f=100,U=1,i=1,q=2;" + enc(secret) + "\x1b\\",
		"\x1b_Ga=T,t=t,f=100,U=1,i=2,q=2;" + enc(secret) + "\x1b\\",
		"\x1b_Ga=T,t=s,f=100,U=1,i=3,q=2;" + enc("/bmux-shm") + "\x1b\\",
		"\x1b_Ga=p,t=f,U=1,i=4,q=2;" + enc(secret) + "\x1b\\",
	} {
		p.apply([]byte(seq + "after"))
	}
	for id := uint32(1); id <= 4; id++ {
		if _, ok := p.graphics.Image(id); ok {
			t.Errorf("image %d stored from a refused transmission", id)
		}
	}
	if _, err := os.Stat(secret); err != nil {
		t.Errorf("the app got a file deleted: %v", err)
	}
	f, _ := p.frame(0)
	var row strings.Builder
	for _, c := range f.Rows[0] {
		row.WriteString(c.Text)
	}
	if got := strings.TrimSpace(row.String()); got != strings.Repeat("after", 4) {
		t.Errorf("screen shows %q; the sequences must leave nothing but the text after each", got)
	}
	if len(f.Images) != 0 {
		t.Errorf("frame lists images %+v", f.Images)
	}
}
