package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/josephferrero/bmux/internal/protocol"
)

func TestCreateBuildsMissingDirectoryOnlyWhenAsked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		dir     string
		makeDir bool
		wantErr bool
		want    bool // the directory exists afterwards
	}{
		{"missing and asked", filepath.Join(root, "a", "b", "c"), true, false, true},
		{"missing and not asked", filepath.Join(root, "nope", "x"), false, true, false},
		{"existing and asked", root, true, false, true},
		{"a file is never replaced", file, true, true, false},
		{"under a file", filepath.Join(file, "sub"), true, true, false},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New("/bin/sh")
			defer s.Close()
			r := s.request(protocol.Request{Op: "create", Name: "s" + string(rune('a'+i)), Dir: tt.dir, MakeDir: tt.makeDir})
			if (r.Error != "") != tt.wantErr {
				t.Fatalf("error %q, want error %v", r.Error, tt.wantErr)
			}
			info, err := os.Stat(tt.dir)
			if got := err == nil && info.IsDir(); got != tt.want {
				t.Fatalf("directory exists = %v, want %v", got, tt.want)
			}
			if !tt.wantErr {
				if got := s.request(protocol.Request{Op: "list"}).Sessions; len(got) != 1 || got[0].Dir != tt.dir {
					t.Fatalf("sessions %+v", got)
				}
			}
		})
	}
}
