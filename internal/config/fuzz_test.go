package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzLoad reads any config file: Load must never panic, and what it returns
// is already repaired, so repairing it again changes nothing.
func FuzzLoad(f *testing.F) {
	for _, seed := range []string{
		DefaultFile,
		"theme = \"catppuccin\"\nprefix = \"C-b\"\n[keys.prefix]\n\"|\" = \"split-right\"\n",
		"[sidebar]\nwidth = 999999\n[scrollback]\nlines = -1\n",
		"[keys.root]\n\"C-a\" = \"run-shell \"\n\"A-S-F12\" = \"none\"\n",
		"[hooks]\nsession-created = [\"\", \"echo #{\"]\n",
		"[restore.resume]\nclaude = [\"/bin/sh\", \"\\u0007\"]\n",
		"prefix = \n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		cfg, _ := Load(path, func(string) string { return "" })
		again := cfg
		if problems := again.repair(); len(problems) != 0 || !reflect.DeepEqual(again, cfg) {
			t.Fatalf("Load returned a config that still needs repair: %v", problems)
		}
	})
}

// FuzzKeyName canonicalizes key names from config files. It must never panic,
// and a canonical name is its own canonical name.
func FuzzKeyName(f *testing.F) {
	for _, seed := range []string{"C-h", "c-S-pageup", "A-x", "S-tab", "minus", "-", "F12", "é", "C-", "S-a", " ", "A-C-S-del"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, key string) {
		name, err := KeyName(key)
		if err != nil {
			return
		}
		again, err := KeyName(name)
		if err != nil || again != name {
			t.Fatalf("KeyName(%q) = %q, but KeyName(%q) = %q, %v", key, name, name, again, err)
		}
		_, _ = PrefixKey(key)
	})
}
