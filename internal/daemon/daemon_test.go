package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateRuntimeDirectory(t *testing.T) {
	// A short existing path avoids sockaddr_un limits on macOS.
	dir, err := os.MkdirTemp("", "hv-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("BMUX_RUNTIME_DIR", dir)
	socket, err := Prepare()
	if err != nil {
		t.Fatal(err)
	}
	if socket != filepath.Join(dir, "server.sock") {
		t.Fatalf("socket: %s", socket)
	}
	if err = os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = Prepare(); err == nil {
		t.Fatal("public runtime directory accepted")
	}
}

func TestRuntimeSymlinkRejected(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BMUX_RUNTIME_DIR", link)
	if _, err := Prepare(); err == nil {
		t.Fatal("runtime symlink accepted")
	}
}
