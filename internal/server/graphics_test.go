package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/josephferrero/bmux/internal/graphics"
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
