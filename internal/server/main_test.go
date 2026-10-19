package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Tests type shell commands with the paste operation. Readline (bash 5.1+,
// /bin/sh on Fedora) enables bracketed paste by default and then holds a
// pasted newline in the line buffer instead of running the command, so the
// test shells read an inputrc that turns it off.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "bmux-server-test")
	if err != nil {
		panic(err)
	}
	inputrc := filepath.Join(dir, "inputrc")
	if err := os.WriteFile(inputrc, []byte("set enable-bracketed-paste off\n"), 0o600); err != nil {
		panic(err)
	}
	os.Setenv("INPUTRC", inputrc)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
