package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// removedSettings were dropped on purpose, with the reason. A config file
// that still sets one keeps loading, with the setting reported as unknown.
// Every other setting a release documented must still load cleanly.
var removedSettings = map[string]string{
	"projects.zoxide": "v0.6.0 removed zoxide support from the new-session popup (#80)",
}

// removedActions are key actions dropped on purpose, with the reason. A key
// bound to one is reported and left unbound; the file keeps loading.
var removedActions = map[string]string{
	"projects": "v0.5.2 replaced the project picker with the new-session popup; bind new-session instead",
}

// documentedSetting is a commented-out "key = value" line of default-config;
// prose comments never start that way.
var documentedSetting = regexp.MustCompile(`^# ((?:"[^"]+"|[A-Za-z0-9_.-]+) *= .*)$`)

func TestEveryReleasedConfigStillLoads(t *testing.T) {
	configs, err := filepath.Glob("../../testdata/releases/*/default-config.toml")
	if err != nil || len(configs) == 0 {
		t.Fatalf("no release fixtures: %v", err)
	}
	for _, path := range configs {
		release := filepath.Base(filepath.Dir(path))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Every setting that release documented, set to its default then.
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if m := documentedSetting.FindStringSubmatch(line); m != nil {
				lines[i] = m[1]
			}
		}
		full := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(full, []byte(strings.Join(lines, "\n")), 0600); err != nil {
			t.Fatal(err)
		}
		_, err = Load(full, noEnv)
		if err == nil {
			continue
		}
		_, problems, _ := strings.Cut(err.Error(), full+": ")
		for _, problem := range strings.Split(problems, "; ") {
			allowed := false
			for key := range removedSettings {
				allowed = allowed || problem == `unknown setting "`+key+`"`
			}
			for action := range removedActions {
				allowed = allowed || strings.Contains(problem, `: unknown action "`+action+`"`)
			}
			if !allowed {
				t.Errorf("%s's full default config no longer loads cleanly: %s", release, problem)
			}
		}
	}
}
