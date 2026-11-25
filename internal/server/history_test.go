package server

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bmux-dev/bmux/internal/config"
)

func TestRetainedViewportAnchorsAfterHistoryRingFills(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("history", t.TempDir(), "/bin/sh", 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	p.mu.Lock()
	var output strings.Builder
	for i := range currentSettings().scrollback + 20 {
		fmt.Fprintf(&output, "ROW%05d\r\n", i)
	}
	p.trackHistory(func() { _, _ = p.term.WriteString(output.String()) })
	p.revision++
	p.mu.Unlock()
	offsets, histories := map[int]int{}, map[int]int{}
	_, revision := s.snapshot(offsets, histories, 0, true)
	offsets[1] = 5
	before, revision := s.snapshot(offsets, histories, revision, true)
	old := before.Panes[0].Terminal
	if old.History != currentSettings().scrollback {
		t.Fatal("history ring was not filled")
	}
	p.mu.Lock()
	p.trackHistory(func() { _, _ = p.term.WriteString("NEW1\r\nNEW2\r\nNEW3\r\n") })
	p.revision++
	p.mu.Unlock()
	after, _ := s.snapshot(offsets, histories, revision, false)
	now := after.Panes[0].Terminal
	if now.Base != old.Base || frameText(now) != frameText(old) {
		t.Fatalf("retained viewport drifted: base %d->%d", old.Base, now.Base)
	}
	if now.HistoryStart <= old.HistoryStart || now.Offset <= old.Offset {
		t.Fatal("eviction and cumulative line movement were not recorded")
	}
	p.mu.Lock()
	p.trackHistory(func() { _, _ = p.term.WriteString("\x1b[3J") })
	p.revision++
	p.mu.Unlock()
	reset, _ := s.snapshot(offsets, histories, 0, true)
	if reset.Panes[0].Terminal.Epoch == now.Epoch {
		t.Fatal("history reset reused screen identity")
	}
}

// scrollback.lines reaches running panes at their next output: a larger
// limit keeps more, a smaller one trims the oldest lines and starts a new
// coordinate epoch, since those rows are gone.
func TestScrollbackSettingAppliesToRunningPanes(t *testing.T) {
	setScrollback := func(lines int) {
		cfg := config.Default()
		cfg.Scrollback.Lines = lines
		activeSettings.Store(settingsFrom(cfg))
	}
	t.Cleanup(func() { activeSettings.Store(settingsFrom(config.Default())) })
	setScrollback(100)
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("history", t.TempDir(), "/bin/sh", 20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	write := func(rows int) (history int, epoch uint64) {
		var output strings.Builder
		for i := range rows {
			fmt.Fprintf(&output, "ROW%05d\r\n", i)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.trackHistory(func() { _, _ = p.term.WriteString(output.String()) })
		return p.term.ScrollbackLen(), p.epoch
	}
	if history, _ := write(150); history != 100 {
		t.Fatalf("history %d, want the 100-line limit", history)
	}
	setScrollback(300)
	history, epoch := write(150)
	if history != 250 {
		t.Fatalf("after raising the limit: history %d, want 250", history)
	}
	setScrollback(100)
	history, trimmed := write(1)
	if history != 100 || trimmed == epoch {
		t.Fatalf("after lowering the limit: history %d, epoch %d -> %d", history, epoch, trimmed)
	}
}

func TestScrollbackByteCapAppliesToRunningPanes(t *testing.T) {
	setLimits := func(lines, maxMB int) {
		cfg := config.Default()
		cfg.Scrollback.Lines = lines
		cfg.Scrollback.MaxMB = maxMB
		activeSettings.Store(settingsFrom(cfg))
	}
	t.Cleanup(func() { activeSettings.Store(settingsFrom(config.Default())) })
	setLimits(10000, 1)
	t.Setenv("HOME", t.TempDir())
	s, err := newSession("history-bytes", t.TempDir(), "/bin/sh", 200, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.stop()
	p := s.panes[1]
	writeWide := func(rows int) (history, bytes int, epoch uint64) {
		line := strings.Repeat("W", 190) + "\r\n"
		var output strings.Builder
		for range rows {
			output.WriteString(line)
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.trackHistory(func() { _, _ = p.term.WriteString(output.String()) })
		return p.term.ScrollbackLen(), p.term.ScrollbackBytes(), p.epoch
	}
	// ~2320 bytes/row -> 1 MiB holds ~451 rows; writing 1000 must stay <= 1 MiB.
	history, bytes, epoch := writeWide(1000)
	if bytes == 0 || bytes > 1<<20 {
		t.Fatalf("ScrollbackBytes = %d, want in (0, %d]", bytes, 1<<20)
	}
	if history >= 1000 {
		t.Fatalf("history = %d, expected oldest lines dropped at 1 MiB cap", history)
	}

	// Lower byte cap below current usage via activeSettings; next write trims and bumps epoch.
	p.mu.Lock()
	p.term.SetScrollbackMaxBytes(4 << 10)
	p.mu.Unlock()
	activeSettings.Store(func() *serverSettings {
		cfg := config.Default()
		cfg.Scrollback.MaxMB = 1
		s := settingsFrom(cfg)
		s.scrollbackMaxBytes = 2 << 10
		return s
	}())
	h2, b2, e2 := writeWide(1)
	if b2 > 2<<10 || h2 >= history || e2 == epoch {
		t.Fatalf("after shrinking byte cap: history %d->%d, bytes=%d, epoch %d->%d", history, h2, b2, epoch, e2)
	}
}
