package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestSyncOutputReleasesWholeUpdates(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, tc := range []struct {
		name   string
		chunks []string
		want   []string // ready output after each chunk
	}{
		{"plain output passes through", []string{"hello", " world"}, []string{"hello", " world"}},
		{"update in one read", []string{"a\x1b[?2026hdraw\x1b[?2026lb"}, []string{"a\x1b[?2026hdraw\x1b[?2026lb"}},
		{"update across reads", []string{"\x1b[?2026hcle", "ar", "draw\x1b[?2026l"}, []string{"\x1b[?2026h", "", "clear" + "draw\x1b[?2026l"}},
		{"split begin marker", []string{"x\x1b[?20", "26hdraw", "\x1b[?2026l"}, []string{"x", "\x1b[?2026h", "draw\x1b[?2026l"}},
		{"split end marker", []string{"\x1b[?2026hdraw\x1b[?", "2026ly"}, []string{"\x1b[?2026h", "draw\x1b[?2026ly"}},
		{"back-to-back updates", []string{"\x1b[?2026h1\x1b[?2026l\x1b[?2026h2"}, []string{"\x1b[?2026h1\x1b[?2026l\x1b[?2026h"}},
		{"lone escape is held", []string{"text\x1b"}, []string{"text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o syncOutput
			for i, chunk := range tc.chunks {
				if got := string(o.feed([]byte(chunk), now)); got != tc.want[i] {
					t.Fatalf("chunk %d %q: ready %q, want %q", i, chunk, got, tc.want[i])
				}
			}
		})
	}
}

func TestSyncOutputDeadlines(t *testing.T) {
	now := time.Unix(1000, 0)
	var o syncOutput
	if !o.deadline().IsZero() {
		t.Fatal("deadline set with nothing held")
	}
	o.feed([]byte("a\x1b["), now)
	if got := o.deadline(); !got.Equal(now.Add(syncPartialTimeout)) {
		t.Fatalf("partial marker deadline %v", got)
	}
	if got := string(o.flush()); got != "\x1b[" {
		t.Fatalf("flushed partial marker %q", got)
	}
	o.feed([]byte("\x1b[?2026hhalf"), now)
	if got := o.deadline(); !got.Equal(now.Add(syncTimeout)) {
		t.Fatalf("update deadline %v", got)
	}
	// An update that never ends is released whole, and tracking starts over.
	if got := string(o.flush()); got != "half" {
		t.Fatalf("flushed update %q", got)
	}
	if got := string(o.feed([]byte("next\x1b[?2026l"), now)); got != "next\x1b[?2026l" {
		t.Fatalf("output after timeout %q", got)
	}
	big := strings.Repeat("x", syncMaxBytes+1)
	if got := o.feed([]byte("\x1b[?2026h"+big), now); len(got) != len(syncBegin)+len(big) || o.syncing {
		t.Fatalf("oversized update held: released %d bytes, syncing %v", len(got), o.syncing)
	}
}

// A real pane never shows a synchronized update half drawn, and releases one
// that never ends after the timeout.
func TestPaneHoldsSynchronizedUpdates(t *testing.T) {
	p, dir := scriptedPane(t, `stty raw -echo
mkfifo go go2
printf 'OLD\r\n'
echo ready > ready
read -r go < go
printf '\033[?2026h\033[2J\033[HHALF'
echo half > half
read -r go < go2
printf 'DONE\033[?2026l'
printf '\033[?2026hSTUCK'
exec sleep 60`)
	awaitFile(t, filepath.Join(dir, "ready"))
	awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "OLD") })
	release := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	release("go")
	awaitFile(t, filepath.Join(dir, "half"))
	time.Sleep(100 * time.Millisecond)
	if text := frameText(mustFrame(p)); !strings.Contains(text, "OLD") || strings.Contains(text, "HALF") {
		t.Fatalf("frame shows a half-drawn update: %q", text)
	}
	release("go2")
	awaitFrame(t, p, func(f *protocol.TerminalFrame) bool {
		text := frameText(f)
		return strings.Contains(text, "HALFDONE") && !strings.Contains(text, "OLD")
	})
	if strings.Contains(frameText(mustFrame(p)), "STUCK") {
		t.Fatal("unfinished update shown before the timeout")
	}
	awaitFrame(t, p, func(f *protocol.TerminalFrame) bool { return strings.Contains(frameText(f), "STUCK") })
}

func TestPaneReportsSynchronizedUpdatesSupported(t *testing.T) {
	p, dir := scriptedPane(t, `stty raw -echo
printf '\033[?2026$p'
dd bs=1 count=11 of=reply.tmp 2>/dev/null
mv reply.tmp reply
exec sleep 60`)
	_ = p
	if got := awaitFile(t, filepath.Join(dir, "reply")); got != "\x1b[?2026;2$y" {
		t.Fatalf("DECRQM 2026 reply %q, want reset (supported)", got)
	}
}

func mustFrame(p *pane) *protocol.TerminalFrame {
	f, _ := p.frame(0)
	return f
}

func TestSyncOutputFastPathZeroCopyAndZeroAllocs(t *testing.T) {
	now := time.Unix(1000, 0)
	var o syncOutput
	input := []byte("ordinary PTY output line without synchronized markers\r\n")
	got := o.feed(input, now)
	if string(got) != string(input) {
		t.Fatalf("got %q, want %q", got, input)
	}
	if &got[0] != &input[0] {
		t.Fatal("fast path copied input instead of returning zero-copy slice")
	}
	if allocs := testing.AllocsPerRun(100, func() {
		out := o.feed(input, now)
		if len(out) != len(input) {
			t.Fatal("unexpected length")
		}
	}); allocs != 0 {
		t.Fatalf("fast path allocated %v times per run, want 0", allocs)
	}

	// Partial syncBegin suffix returns a zero-copy prefix and holds only the suffix.
	partial := []byte("prefix\x1b[?20")
	got = o.feed(partial, now)
	if string(got) != "prefix" || &got[0] != &partial[0] {
		t.Fatalf("partial fast path got %q (aliased=%v), want zero-copy prefix", got, len(got) > 0 && &got[0] == &partial[0])
	}
	if got := string(o.flush()); got != "\x1b[?20" {
		t.Fatalf("held suffix %q, want %q", got, "\x1b[?20")
	}
}

func TestSyncOutputMultiChunkLinearAndCapShrink(t *testing.T) {
	now := time.Unix(1000, 0)
	var o syncOutput
	chunk := []byte(strings.Repeat("A", 4096))
	if got := string(o.feed([]byte(syncBegin), now)); got != syncBegin {
		t.Fatalf("begin: %q", got)
	}
	// 256 chunks = 1 MiB inside a single synchronized update, with a split end marker.
	start := time.Now()
	for range 256 {
		if got := o.feed(chunk, now); len(got) != 0 {
			t.Fatalf("unexpected early release of %d bytes", len(got))
		}
	}
	if got := o.feed([]byte("\x1b[?20"), now); len(got) != 0 {
		t.Fatalf("unexpected release on partial end marker: %d", len(got))
	}
	released := o.feed([]byte("26lafter"), now)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("multi-chunk sync took %v, expected O(N)", elapsed)
	}
	wantLen := 256*len(chunk) + len(syncEnd) + len("after")
	if len(released) != wantLen || o.syncing {
		t.Fatalf("released %d bytes (want %d), syncing=%v", len(released), wantLen, o.syncing)
	}
	// Oversized capacity (>64 KiB) must not stay pinned after returning to normal output.
	if cap(o.held) > syncRetainCap {
		t.Fatalf("held capacity stayed pinned at %d (> %d)", cap(o.held), syncRetainCap)
	}
	_ = o.feed([]byte("normal"), now)
	if cap(o.ready) > syncRetainCap {
		t.Fatalf("ready capacity stayed pinned at %d (> %d)", cap(o.ready), syncRetainCap)
	}
}
