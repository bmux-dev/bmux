package config

import (
	"strings"
	"testing"
)

func TestKeyNamesAreCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"C-h": "C-h", "c-H": "C-h", "C-S-h": "C-h", "A-x": "A-x", "C-A-x": "C-A-x", "A-C-x": "C-A-x",
		"S-pageup": "S-pageup", "S-PageUp": "S-pageup", "S-a": "A", "S-|": "|", "-": "minus", "minus": "minus",
		"C--": "C-minus", " ": "space", "space": "space", "enter": "ret", "Esc": "esc", "f5": "F5", "F12": "F12",
		"|": "|", "(": "(", "é": "é", "A-S-a": "A-A", "S-space": "space", "S-tab": "S-tab",
	} {
		if got, err := KeyName(in); err != nil || got != want {
			t.Errorf("KeyName(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "X-a", "ab", "F13", "F0", "F01", "FF1", "C-", "pgup", "\x07"} {
		if got, err := KeyName(in); err == nil {
			t.Errorf("KeyName(%q) accepted as %q", in, got)
		}
	}
}

func TestBindingsMergeChangesOverTheDefaults(t *testing.T) {
	cfg, err := Load(write(t, `prefix = "C-b"
[keys.root]
"C-l" = "none"
"A-h" = "focus-left"
[keys.prefix]
"-" = "none"
"v" = "split-right"
"s" = "detach"
`), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	root, prefix := cfg.Bindings()
	if _, ok := root["C-l"]; ok || root["C-h"] != "navigate-left" || root["A-h"] != "focus-left" {
		t.Fatalf("root: %v", root)
	}
	if _, ok := prefix["minus"]; ok || prefix["v"] != "split-right" || prefix["|"] != "split-right" || prefix["s"] != "detach" {
		t.Fatalf("prefix: %v", prefix)
	}
	if prefix["C-b"] != "send-prefix" || prefix["C-a"] != "" {
		t.Fatalf("the prefix doesn't send itself: %v", prefix)
	}
}

func TestInvalidBindingsAreReportedAndDropped(t *testing.T) {
	cfg, err := Load(write(t, `[keys.root]
"C-a" = "sessions"
"C-x" = "explode"
"Hyper-x" = "zoom"
[keys.prefix]
"S-a" = "zoom"
"A" = "detach"
"z" = "zoom"
`), noEnv)
	if err == nil {
		t.Fatal("invalid bindings accepted silently")
	}
	for _, want := range []string{"prefix can't also be a root key", `unknown action "explode"`, `"Hyper-x"`, "bound twice"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error doesn't mention %s: %v", want, err)
		}
	}
	root, prefix := cfg.Bindings()
	if root["C-a"] != "" || root["C-x"] != "" || prefix["z"] != "zoom" || prefix["C-a"] != "send-prefix" {
		t.Fatalf("invalid bindings kept: root %v prefix %v", root, prefix)
	}
}
