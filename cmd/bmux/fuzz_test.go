package main

import "testing"

// FuzzParseTarget reads SESSION:WINDOW.PANE targets from the command line;
// it must never panic, whatever a script passes.
func FuzzParseTarget(f *testing.F) {
	for _, seed := range []string{"api", "api:2", "api:2.1", "api:2.", ":2.1", ":", "::", "a:b.c", "api:-1.-1", "api:99999999999999999999", ".", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = parseTarget(text)
	})
}
