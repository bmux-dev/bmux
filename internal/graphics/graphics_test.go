package graphics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func testPNG(t testing.TB, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func newPane(limits Limits, hooks Hooks) *Pane { return NewStore(limits).NewPane(hooks) }

func yes() bool { return true }

func mustInfo(t *testing.T, p *Pane, appID uint32) Info {
	t.Helper()
	info, ok := p.Image(appID)
	if !ok {
		t.Fatalf("image %d is not stored", appID)
	}
	return info
}

func TestChunkedTransmitThenVirtualPlacement(t *testing.T) {
	p := newPane(DefaultLimits, Hooks{})
	data := b64(testPNG(t, 4, 2, color.NRGBA{255, 0, 0, 255}))
	if got := p.Handle([]byte("a=t,f=100,i=7,m=1;" + data[:8])); got != "" {
		t.Fatalf("first chunk replied %q", got)
	}
	if got := p.Handle([]byte("m=1;" + data[8:16])); got != "" {
		t.Fatalf("middle chunk replied %q", got)
	}
	if _, ok := p.Image(7); ok {
		t.Fatal("image stored before its last chunk")
	}
	if got, want := p.Handle([]byte("m=0;"+data[16:])), "\x1b_Gi=7;OK\x1b\\"; got != want {
		t.Fatalf("last chunk replied %q, want %q", got, want)
	}
	info := mustInfo(t, p, 7)
	if info.Width != 4 || info.Height != 2 || info.Cols != 0 {
		t.Fatalf("transmit alone must not place the image: %+v", info)
	}
	if got := p.Handle([]byte("a=p,U=1,i=7,c=3,r=2,q=2")); got != "" {
		t.Fatalf("quiet placement replied %q", got)
	}
	if info = mustInfo(t, p, 7); info.Cols != 3 || info.Rows != 2 {
		t.Fatalf("placement grid %dx%d, want 3x2", info.Cols, info.Rows)
	}
}

func TestQuietAndErrorReplies(t *testing.T) {
	p := newPane(DefaultLimits, Hooks{})
	good := b64(testPNG(t, 1, 1, color.NRGBA{A: 255}))
	tests := []struct {
		name, in, want string
	}{
		{"ok by default", "a=t,f=100,i=1;" + good, "\x1b_Gi=1;OK\x1b\\"},
		{"q=1 hides ok", "a=t,f=100,i=2,q=1;" + good, ""},
		{"q=2 hides all", "a=t,f=100,i=3,q=2;bm90IGEgcG5n", ""},
		{"q=1 keeps errors", "a=t,f=100,i=4,q=1;bm90IGEgcG5n", "\x1b_Gi=4;EINVAL:bad PNG data\x1b\\"},
		{"no id no reply", "a=t,f=100;bm90IGEgcG5n", ""},
		{"place missing", "a=p,U=1,i=99", "\x1b_Gi=99;ENOENT:image not found\x1b\\"},
		{"echoes placement id", "a=p,U=1,i=99,p=5", "\x1b_Gi=99,p=5;ENOENT:image not found\x1b\\"},
		{"id beyond 24 bits", "a=t,f=100,i=16777216;" + good, "\x1b_Gi=16777216;EINVAL:image id out of range\x1b\\"},
		{"shared memory refused", "a=t,t=s,f=100,i=5;" + b64([]byte("/shm")), "\x1b_Gi=5;EINVAL:shared memory transmission is not supported\x1b\\"},
		{"delete never replies", "a=d,d=I,i=1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Handle([]byte(tt.in)); got != tt.want {
				t.Fatalf("reply %q, want %q", got, tt.want)
			}
		})
	}
	if _, ok := p.Image(5); ok {
		t.Fatal("refused shared-memory transmission stored an image")
	}
}

func TestFailedChunkedTransmitSwallowsRemainingChunks(t *testing.T) {
	// Limit so small the second chunk overflows; later chunks carry no keys
	// and must not be mistaken for new transmissions.
	p := newPane(Limits{Pane: 30, Total: 30}, Hooks{})
	if got := p.Handle([]byte("a=t,f=100,i=1,m=1;" + strings.Repeat("A", 20))); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := p.Handle([]byte("m=1;" + strings.Repeat("A", 40))); !strings.Contains(got, "EFBIG") {
		t.Fatalf("overflowing chunk replied %q", got)
	}
	if got := p.Handle([]byte("m=1;AAAA")); got != "" {
		t.Fatalf("swallowed chunk replied %q", got)
	}
	if got := p.Handle([]byte("m=0;AAAA")); got != "" {
		t.Fatalf("final swallowed chunk replied %q", got)
	}
	if p.pending != nil {
		t.Fatal("transmission still pending after its last chunk")
	}
	if got := p.Handle([]byte("a=p,U=1,i=1,q=1")); !strings.Contains(got, "ENOENT") {
		t.Fatalf("failed transmission left an image: %q", got)
	}
}

func TestRawFormatsBecomePNG(t *testing.T) {
	zipped := func(b []byte) []byte {
		var out bytes.Buffer
		zw := zlib.NewWriter(&out)
		zw.Write(b)
		zw.Close()
		return out.Bytes()
	}
	rgba := []byte{1, 2, 3, 128, 4, 5, 6, 255}
	rgb := []byte{10, 20, 30, 40, 50, 60}
	tests := []struct {
		name, control string
		data          []byte
		want          []color.NRGBA
	}{
		{"rgba", "f=32,s=2,v=1", rgba, []color.NRGBA{{1, 2, 3, 128}, {4, 5, 6, 255}}},
		{"rgb is opaque", "f=24,s=2,v=1", rgb, []color.NRGBA{{10, 20, 30, 255}, {40, 50, 60, 255}}},
		{"zlib", "f=32,s=2,v=1,o=z", zipped(rgba), []color.NRGBA{{1, 2, 3, 128}, {4, 5, 6, 255}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPane(DefaultLimits, Hooks{})
			if got := p.Handle([]byte("a=t,i=1," + tt.control + ";" + b64(tt.data))); got != "\x1b_Gi=1;OK\x1b\\" {
				t.Fatalf("reply %q", got)
			}
			info := mustInfo(t, p, 1)
			data, ok := p.store.Data(info.ID)
			if !ok {
				t.Fatal("no data for stored image")
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			for x, want := range tt.want {
				if got := color.NRGBAModel.Convert(img.At(x, 0)); got != want {
					t.Errorf("pixel %d = %v, want %v", x, got, want)
				}
			}
		})
	}
}

func TestInvalidPayloadsAreRefused(t *testing.T) {
	good := testPNG(t, 2, 2, color.NRGBA{A: 255})
	var bomb bytes.Buffer
	zw := zlib.NewWriter(&bomb)
	zw.Write(make([]byte, 1<<20))
	zw.Close()
	tests := []struct{ name, in, want string }{
		{"truncated png", "a=t,f=100,i=1;" + b64(good[:len(good)-5]), "EINVAL"},
		{"raw size mismatch", "a=t,f=32,s=2,v=2,i=1;" + b64(make([]byte, 15)), "EINVAL"},
		{"raw without size", "a=t,f=32,i=1;" + b64(make([]byte, 16)), "EINVAL"},
		{"unknown format", "a=t,f=7,i=1;" + b64(good), "EINVAL"},
		{"not base64", "a=t,f=100,i=1;!!!", "EINVAL"},
		{"zlib bomb", "a=t,f=32,s=512,v=512,o=z,i=1;" + b64(bomb.Bytes()), "EFBIG"},
		{"huge raw size", "a=t,f=32,s=100000,v=100000,i=1;" + b64(make([]byte, 16)), "EINVAL"},
		{"bad key value", "a=t,f=100,i=1,s=x;" + b64(good), "EINVAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPane(Limits{Pane: 64 << 10, Total: 64 << 10}, Hooks{})
			if got := p.Handle([]byte(tt.in)); !strings.Contains(got, tt.want) {
				t.Fatalf("reply %q, want %s", got, tt.want)
			}
			if _, ok := p.Image(1); ok {
				t.Fatal("refused payload was stored")
			}
		})
	}
}

func TestQueryAnswersFromAttachedClients(t *testing.T) {
	probe := "a=q,i=31,s=1,v=1,t=d,f=24;AAAA"
	supported := false
	p := newPane(DefaultLimits, Hooks{Supported: func() bool { return supported }})
	if got := p.Handle([]byte(probe)); !strings.HasPrefix(got, "\x1b_Gi=31;ENOTSUPPORTED") {
		t.Fatalf("unsupported query replied %q", got)
	}
	supported = true
	if got, want := p.Handle([]byte(probe)), "\x1b_Gi=31;OK\x1b\\"; got != want {
		t.Fatalf("supported query replied %q, want %q", got, want)
	}
	if _, ok := p.Image(31); ok {
		t.Fatal("a query stored its image")
	}
	if got := p.Handle([]byte("a=q,i=32,s=1,v=1,t=d,f=24;AA")); !strings.Contains(got, "EINVAL") {
		t.Fatalf("query with bad data replied %q", got)
	}
}

func TestDelete(t *testing.T) {
	setup := func(t *testing.T) *Pane {
		p := newPane(DefaultLimits, Hooks{})
		data := b64(testPNG(t, 1, 1, color.NRGBA{A: 255}))
		for _, id := range []int{1, 2, 3, 10} {
			p.Handle([]byte(fmt.Sprintf("a=T,U=1,f=100,i=%d,c=1,r=1,q=2;%s", id, data)))
		}
		return p
	}
	tests := []struct {
		name, in       string
		placed, stored []uint32 // app ids that keep a placement / still exist
	}{
		{"lowercase id keeps data", "a=d,d=i,i=2", []uint32{1, 3, 10}, []uint32{1, 2, 3, 10}},
		{"uppercase id frees data", "a=d,d=I,i=2", []uint32{1, 3, 10}, []uint32{1, 3, 10}},
		{"range", "a=d,d=R,x=2,y=3", []uint32{1, 10}, []uint32{1, 10}},
		{"all lowercase", "a=d,d=a", nil, []uint32{1, 2, 3, 10}},
		{"default is all, uppercase", "a=d,d=A", nil, nil},
		{"cursor-relative deletes are ignored", "a=d,d=c", []uint32{1, 2, 3, 10}, []uint32{1, 2, 3, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := setup(t)
			p.Handle([]byte(tt.in))
			in := func(list []uint32, id uint32) bool {
				for _, v := range list {
					if v == id {
						return true
					}
				}
				return false
			}
			for _, id := range []uint32{1, 2, 3, 10} {
				info, ok := p.Image(id)
				if ok != in(tt.stored, id) {
					t.Errorf("image %d stored=%v, want %v", id, ok, in(tt.stored, id))
				}
				if ok && (info.Cols > 0) != in(tt.placed, id) {
					t.Errorf("image %d placed=%v, want %v", id, info.Cols > 0, in(tt.placed, id))
				}
			}
		})
	}
}

func TestSamePaneIDGetsNewServerIDOnReplace(t *testing.T) {
	s := NewStore(DefaultLimits)
	a, b := s.NewPane(Hooks{}), s.NewPane(Hooks{})
	data := b64(testPNG(t, 1, 1, color.NRGBA{A: 255}))
	other := b64(testPNG(t, 2, 1, color.NRGBA{A: 255}))
	a.Handle([]byte("a=t,f=100,i=1,q=2;" + data))
	b.Handle([]byte("a=t,f=100,i=1,q=2;" + other))
	first, second := mustInfo(t, a, 1), mustInfo(t, b, 1)
	if first.ID == second.ID || second.Width != 2 {
		t.Fatalf("panes share app id 1 but images collide: %+v %+v", first, second)
	}
	a.Handle([]byte("a=t,f=100,i=1,q=2;" + other))
	if replaced := mustInfo(t, a, 1); replaced.ID == first.ID {
		t.Fatal("replaced image kept its server id; clients would keep the stale pixels")
	}
	if _, ok := s.Data(first.ID); ok {
		t.Fatal("replaced image still stored")
	}
}

func TestPlacementSizeFromCellSize(t *testing.T) {
	cw, ch := 10, 20
	p := newPane(DefaultLimits, Hooks{CellPixels: func() (int, int) { return cw, ch }})
	p.Handle([]byte("a=t,f=100,i=1,q=2;" + b64(testPNG(t, 95, 41, color.NRGBA{A: 255}))))
	tests := []struct {
		control    string
		cols, rows int
	}{
		{"", 10, 3},
		{"c=4", 4, 1},
		{"r=6", 28, 6},
		{"c=7,r=2", 7, 2},
	}
	for _, tt := range tests {
		p.Handle([]byte("a=p,U=1,i=1,q=2," + tt.control))
		if info := mustInfo(t, p, 1); info.Cols != tt.cols || info.Rows != tt.rows {
			t.Errorf("%q placed %dx%d, want %dx%d", tt.control, info.Cols, info.Rows, tt.cols, tt.rows)
		}
	}
}

func TestFileTransmission(t *testing.T) {
	dir := t.TempDir()
	good := testPNG(t, 3, 3, color.NRGBA{B: 255, A: 255})
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	regular := write("shot.png", good)
	p := newPane(DefaultLimits, Hooks{})
	if got := p.Handle([]byte("a=t,t=f,f=100,i=1;" + b64([]byte(regular)))); got != "\x1b_Gi=1;OK\x1b\\" {
		t.Fatalf("regular file replied %q", got)
	}
	if _, err := os.Stat(regular); err != nil {
		t.Fatal("t=f must leave the file alone")
	}
	if info := mustInfo(t, p, 1); info.Width != 3 {
		t.Fatalf("read %+v", info)
	}

	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(regular, link); err != nil {
		t.Fatal(err)
	}
	refused := map[string]string{
		"directory":     dir,
		"fifo":          fifo,
		"/dev/zero":     "/dev/zero",
		"missing":       filepath.Join(dir, "nope"),
		"relative path": "shot.png",
	}
	for name, path := range refused {
		if got := p.Handle([]byte("a=t,t=f,f=100,i=2;" + b64([]byte(path)))); !strings.Contains(got, "E") || strings.Contains(got, "OK") {
			t.Errorf("%s: replied %q", name, got)
		}
		if _, ok := p.Image(2); ok {
			t.Errorf("%s: stored an image", name)
		}
	}
	if got := p.Handle([]byte("a=t,t=f,f=100,i=3;" + b64([]byte(link)))); !strings.Contains(got, "OK") {
		t.Errorf("symlink to a regular file replied %q", got)
	}

	big := newPane(Limits{Pane: int64(len(good)) - 1, Total: 1 << 20}, Hooks{})
	if got := big.Handle([]byte("a=t,t=f,f=100,i=1;" + b64([]byte(regular)))); !strings.Contains(got, "EFBIG") {
		t.Errorf("oversized file replied %q", got)
	}
	// S and O select a byte range of the file.
	framed := write("framed.bin", append(append([]byte("junk"), good...), "trailing"...))
	if got := p.Handle([]byte(fmt.Sprintf("a=t,t=f,f=100,i=4,O=4,S=%d;%s", len(good), b64([]byte(framed))))); !strings.Contains(got, "OK") {
		t.Errorf("file range replied %q", got)
	}
}

func TestTemporaryFileRules(t *testing.T) {
	good := testPNG(t, 2, 2, color.NRGBA{G: 255, A: 255})
	temp := t.TempDir() // below os.TempDir()
	p := newPane(DefaultLimits, Hooks{})
	send := func(id int, path string) string {
		return p.Handle([]byte(fmt.Sprintf("a=t,t=t,f=100,i=%d;%s", id, b64([]byte(path)))))
	}

	marked := filepath.Join(temp, "tty-graphics-protocol-a.png")
	os.WriteFile(marked, good, 0o600)
	if got := send(1, marked); !strings.Contains(got, "OK") {
		t.Fatalf("marked temp file replied %q", got)
	}
	if _, err := os.Stat(marked); !os.IsNotExist(err) {
		t.Error("t=t must delete the file after reading it")
	}

	unmarked := filepath.Join(temp, "plain.png")
	os.WriteFile(unmarked, good, 0o600)
	if got := send(2, unmarked); strings.Contains(got, "OK") {
		t.Errorf("temp file without the marker was read: %q", got)
	}
	if _, err := os.Stat(unmarked); err != nil {
		t.Error("a refused t=t file must not be deleted")
	}

	// A marked symlink in a temp dir pointing at a file elsewhere must neither
	// be read nor lead to that file being deleted.
	target := filepath.Join(t.TempDir(), "important.png")
	os.WriteFile(target, good, 0o600)
	link := filepath.Join(temp, "tty-graphics-protocol-link")
	os.Symlink(target, link)
	// target lives in a temp dir too, but lacks the marker once resolved.
	if got := send(3, link); strings.Contains(got, "OK") {
		t.Errorf("symlink to an unmarked file was read: %q", got)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("symlink target was deleted")
	}
}

// refs reports which app ids cells still show, standing in for the emulator.
type refs map[uint32]bool

func (r refs) hook(owned bool) (map[uint32]struct{}, bool) {
	set := make(map[uint32]struct{})
	for id, shown := range r {
		if shown {
			set[id] = struct{}{}
		}
	}
	return set, true
}

func TestPaneCapEvictsUnreferencedLeastRecentlyUsedFirst(t *testing.T) {
	one := testPNG(t, 8, 8, color.NRGBA{R: 1, A: 255})
	limit := int64(len(one))*3 + int64(len(one))/2
	shown := refs{1: true}
	p := newPane(Limits{Pane: limit, Total: 1 << 30}, Hooks{Referenced: shown.hook})
	put := func(id int) {
		// Different pixels so the PNGs differ; sizes stay close enough.
		if got := p.Handle([]byte(fmt.Sprintf("a=t,f=100,i=%d,q=2;%s", id, b64(testPNG(t, 8, 8, color.NRGBA{R: uint8(id), A: 255}))))); got != "" {
			t.Fatal(got)
		}
	}
	put(1) // oldest, but shown
	put(2)
	put(3)
	mustInfo(t, p, 2) // 2 is now newer than 3
	put(4)            // over the cap: evicts 3, the oldest unreferenced
	for id, want := range map[uint32]bool{1: true, 2: true, 3: false, 4: true} {
		if _, ok := p.Image(id); ok != want {
			t.Errorf("image %d stored=%v, want %v", id, ok, want)
		}
	}
	if got := p.Bytes(); got > limit {
		t.Fatalf("pane holds %d bytes, cap %d", got, limit)
	}
}

func TestReferencedImagesAreEvictedWhenNothingElseFits(t *testing.T) {
	one := testPNG(t, 8, 8, color.NRGBA{R: 1, A: 255})
	limit := int64(len(one))*2 + int64(len(one))/2
	shown := refs{1: true, 2: true, 3: true}
	p := newPane(Limits{Pane: limit, Total: 1 << 30}, Hooks{Referenced: shown.hook})
	for id := 1; id <= 3; id++ {
		p.Handle([]byte(fmt.Sprintf("a=t,f=100,i=%d,q=2;%s", id, b64(testPNG(t, 8, 8, color.NRGBA{R: uint8(id), A: 255})))))
	}
	if _, ok := p.Image(1); ok {
		t.Error("oldest shown image survived a full pane")
	}
	if _, ok := p.Image(3); !ok {
		t.Error("the new image was refused instead of evicting an old one")
	}
}

func TestImageLargerThanPaneCapIsRefused(t *testing.T) {
	big := testPNG(t, 64, 64, color.NRGBA{R: 9, A: 255})
	p := newPane(Limits{Pane: 100, Total: 1 << 30}, Hooks{})
	got := p.Handle([]byte("a=t,f=100,i=1;" + b64(big)))
	if !strings.Contains(got, "EFBIG") && !strings.Contains(got, "ENOSPC") {
		t.Fatalf("reply %q", got)
	}
	if p.Bytes() != 0 {
		t.Fatal("refused image counted against the cap")
	}
}

func TestServerCapEvictsAcrossPanes(t *testing.T) {
	one := testPNG(t, 8, 8, color.NRGBA{R: 1, A: 255})
	size := int64(len(one))
	s := NewStore(Limits{Pane: size * 4, Total: size*2 + size/2})
	idle := refs{}
	a := s.NewPane(Hooks{Referenced: idle.hook})
	shown := refs{1: true}
	b := s.NewPane(Hooks{Referenced: shown.hook})
	c := s.NewPane(Hooks{})
	put := func(p *Pane, id int) {
		p.Handle([]byte(fmt.Sprintf("a=t,f=100,i=%d,q=2;%s", id, b64(testPNG(t, 8, 8, color.NRGBA{R: uint8(id), A: 255})))))
	}
	put(b, 1) // oldest, shown by its pane
	put(a, 1) // unreferenced
	put(c, 1) // forces one eviction: a's, not b's older shown image
	if _, ok := b.Image(1); !ok {
		t.Error("an image still shown was evicted while an unshown one existed")
	}
	if _, ok := a.Image(1); ok {
		t.Error("the unshown image survived")
	}
	if s.Bytes() > s.limits.Total {
		t.Fatalf("server holds %d bytes, cap %d", s.Bytes(), s.limits.Total)
	}
}

func TestClosingAPaneFreesItsImages(t *testing.T) {
	s := NewStore(DefaultLimits)
	p := s.NewPane(Hooks{})
	p.Handle([]byte("a=t,f=100,i=1,q=2;" + b64(testPNG(t, 4, 4, color.NRGBA{A: 255}))))
	info := mustInfo(t, p, 1)
	p.Close()
	if s.Bytes() != 0 {
		t.Fatalf("server keeps %d bytes of a closed pane", s.Bytes())
	}
	if _, ok := s.Data(info.ID); ok {
		t.Fatal("closed pane's image still served")
	}
	p.Handle([]byte("a=t,f=100,i=2,q=2;" + b64(testPNG(t, 4, 4, color.NRGBA{A: 255}))))
	if s.Bytes() != 0 {
		t.Fatal("closed pane stored a new image")
	}
}
