//go:build linux

package server

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeFakeProc(t *testing.T, root string, pid, pgid int, state, comm string, children []int) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(filepath.Join(dir, "task", strconv.Itoa(pid)), 0o755); err != nil {
		t.Fatal(err)
	}
	// /proc/<pid>/stat: pid (comm) state ppid pgrp ...
	stat := strconv.Itoa(pid) + " (" + comm + ") " + state + " 1 " + strconv.Itoa(pgid) + " 1 0 0\n"
	if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o600); err != nil {
		t.Fatal(err)
	}
	var kids string
	for i, c := range children {
		if i > 0 {
			kids += " "
		}
		kids += strconv.Itoa(c)
	}
	if err := os.WriteFile(filepath.Join(dir, "task", strconv.Itoa(pid), "children"), []byte(kids), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestForegroundGroupCommandFastPathSkipsProcScan(t *testing.T) {
	root := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })

	// Direct leader match: PID == PGID is "claude".
	writeFakeProc(t, root, 4200, 4200, "S", "claude", nil)
	// Child-tree match: leader PID 4300 is "sh", child PID 4301 in same group is "opencode".
	writeFakeProc(t, root, 4300, 4300, "S", "sh", []int{4301})
	writeFakeProc(t, root, 4301, 4300, "S", "opencode", nil)
	// Non-agent foreground job ("vim") under shell 4000 must return ("", nil) without scanning /proc.
	writeFakeProc(t, root, 4000, 4000, "S", "bash", []int{4400})
	writeFakeProc(t, root, 4400, 4400, "S", "vim", nil)
	// Shell pipeline ("cat | claude") under shell 4001: both are direct children of shell in pgid 4500.
	writeFakeProc(t, root, 4001, 4001, "S", "bash", []int{4500, 4501})
	writeFakeProc(t, root, 4500, 4500, "S", "cat", nil)
	writeFakeProc(t, root, 4501, 4500, "S", "claude", nil)

	// Make root execute-only (0o111) so os.ReadDir(root) fails with EACCES,
	// proving all fast paths resolve in O(1) without enumerating /proc.
	if err := os.Chmod(root, 0o111); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(root); err == nil {
		t.Skip("running with privileges that bypass 0o111 directory read restriction")
	}

	got, err := foregroundGroupCommandIn(root, 4200, 0, agentMatch)
	if err != nil || got != "claude" {
		t.Fatalf("leader fast path: got (%q, %v), want (%q, nil)", got, err, "claude")
	}

	got, err = foregroundGroupCommandIn(root, 4300, 0, agentMatch)
	if err != nil || got != "opencode" {
		t.Fatalf("child fast path: got (%q, %v), want (%q, nil)", got, err, "opencode")
	}

	got, err = foregroundGroupCommandIn(root, 4400, 4000, agentMatch)
	if err != nil || got != "" {
		t.Fatalf("non-agent foreground job: got (%q, %v), want (\"\", nil) without /proc scan", got, err)
	}

	got, err = foregroundGroupCommandIn(root, 4500, 4001, agentMatch)
	if err != nil || got != "claude" {
		t.Fatalf("shell pipeline fast path: got (%q, %v), want (%q, nil)", got, err, "claude")
	}
}

func TestForegroundGroupCommandFallsBackToProcScanForOrphanGroup(t *testing.T) {
	root := t.TempDir()
	// Group leader 5000 has exited (no /proc/5000), but member 5005 is alive in pgid 5000.
	writeFakeProc(t, root, 5005, 5000, "S", "codex", nil)
	// Stopped or zombie members must still be ignored.
	writeFakeProc(t, root, 6000, 6000, "Z", "claude", nil)
	writeFakeProc(t, root, 6001, 6000, "T", "claude", nil)

	got, err := foregroundGroupCommandIn(root, 5000, 0, agentMatch)
	if err != nil || got != "codex" {
		t.Fatalf("orphan group fallback: got (%q, %v), want (%q, nil)", got, err, "codex")
	}
	got, err = foregroundGroupCommandIn(root, 6000, 0, agentMatch)
	if err != nil || got != "" {
		t.Fatalf("stopped/zombie group: got (%q, %v), want (\"\", nil)", got, err)
	}
}
