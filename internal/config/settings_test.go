package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSetLineEditsInPlace(t *testing.T) {
	for _, tc := range []struct {
		name, text, table, key, literal, want string
	}{
		{"empty file", "", "", "theme", `"light"`, "theme = \"light\"\n"},
		{"active line keeps its comment column", "theme = \"dark\"   # dark or light\n", "", "theme", `"light"`,
			"theme = \"light\"  # dark or light\n"},
		{"commented default is uncommented", "# theme = \"dark\"   # dark or light\n[ui]\n", "", "theme", `"light"`,
			"theme = \"light\"  # dark or light\n[ui]\n"},
		{"hash in a color isn't a comment", "[ui]\naccent = \"#aabbcc\"  # custom\n", "ui", "accent", `"amber"`,
			"[ui]\naccent = \"amber\"    # custom\n"},
		{"other tables with the same key are left alone", "[ui]\nwidth = 1\n[sidebar]\nwidth = 24\n", "sidebar", "width", "30",
			"[ui]\nwidth = 1\n[sidebar]\nwidth = 30\n"},
		{"new key goes under its table", "[sidebar]\nshow = true\n\n[sound]\n", "sidebar", "width", "30",
			"[sidebar]\nwidth = 30\nshow = true\n\n[sound]\n"},
		{"new top-level key stays above the first table", "# my config\n\n[ui]\n", "", "theme", `"light"`,
			"# my config\ntheme = \"light\"\n\n[ui]\n"},
		{"missing table is appended", "theme = \"dark\"\n", "update", "check", "false",
			"theme = \"dark\"\n\n[update]\ncheck = false\n"},
		{"file without a final newline", "[update]\ncheck = true", "update", "check", "false", "[update]\ncheck = false\n"},
		{"array spanning lines is replaced whole", "[projects]\nroots = [\n  \"~/a\",\n  \"~/b\",\n]\ndepth = 2\n", "projects", "roots", `["~/c"]`,
			"[projects]\nroots = [\"~/c\"]\ndepth = 2\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := setLine(tc.text, tc.table, tc.key, tc.literal); got != tc.want {
				t.Fatalf("got:\n%q\nwant:\n%q", got, tc.want)
			}
		})
	}
}

func settingByKey(t *testing.T, key string) Setting {
	t.Helper()
	for _, tab := range SettingTabs {
		for _, s := range tab.Settings {
			if s.Key == key {
				return s
			}
		}
	}
	t.Fatalf("no setting %q", key)
	return Setting{}
}

func TestSettingSaveWritesValidValuesAndKeepsTheRestOfTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := "# my notes\ntheme = \"dark\"  # keep this\n\n[sidebar]\nshow = true\n\n[projects]\nroots = [\"~/a\"]\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for key, input := range map[string]string{
		"theme": "light", "sidebar.width": "30", "projects.roots": "~/x, /srv/y", "agents.working-after": "3s", "sound.enabled": "false",
	} {
		if err := settingByKey(t, key).Save(path, input); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
	cfg, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "light" || cfg.Sidebar.Width != 30 || cfg.Sound.Enabled || cfg.Agents.WorkingAfter.Seconds() != 3 ||
		!reflect.DeepEqual(cfg.Projects.Roots, []string{"~/x", "/srv/y"}) {
		t.Fatalf("saved values didn't load: %+v", cfg)
	}
	data, _ := os.ReadFile(path)
	for _, kept := range []string{"# my notes\n", "# keep this", "[sidebar]", "show = true"} {
		if !strings.Contains(string(data), kept) {
			t.Fatalf("lost %q:\n%s", kept, data)
		}
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
}

func TestSettingSaveRejectsInvalidValuesWithoutWriting(t *testing.T) {
	for _, tc := range []struct{ key, input string }{
		{"sidebar.width", "99"},
		{"sidebar.width", "wide"},
		{"prefix", "C-h"},
		{"theme", "sepia"},
		{"ui.accent", "pink"},
		{"sound.ready", "chime.wav"},
		{"agents.working-after", "5ms"},
		{"agents.working-after", "soon"},
		{"scrollback.lines", "5"},
		{"projects.roots", "relative/dir"},
		{"sound.ready", "a\nb"},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		original := "[sidebar]\nwidth = 24\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := settingByKey(t, tc.key).Save(path, tc.input); err == nil {
			t.Errorf("%s = %q accepted", tc.key, tc.input)
		}
		if data, _ := os.ReadFile(path); string(data) != original {
			t.Errorf("%s = %q changed the file:\n%s", tc.key, tc.input, data)
		}
	}
}

func TestSettingSaveIgnoresProblemsTheFileAlreadyHad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[sidebar]\nwidth = 99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := settingByKey(t, "theme").Save(path, "light"); err != nil {
		t.Fatalf("an unrelated bad setting blocked the save: %v", err)
	}
	if err := settingByKey(t, "sidebar.width").Save(path, "30"); err != nil {
		t.Fatalf("fixing the bad setting was refused: %v", err)
	}
}

func TestSettingSaveRefusesFilesItCannotEditInPlace(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"does not parse": "theme = \n",
		"inline table":   "sidebar = { width = 24 }\n",
		"dotted key":     "sidebar.width = 24\n",
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".toml")
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := settingByKey(t, "sidebar.width").Save(path, "30"); err == nil {
			t.Errorf("%s: saved", name)
		}
		if data, _ := os.ReadFile(path); string(data) != text {
			t.Errorf("%s: changed the file:\n%s", name, data)
		}
	}
}

// Every setting must be a real key whose default round-trips, and every
// scalar in Config must either have a setting or be listed as file-only.
func TestSettingsMatchConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	defaults := Default()
	have := map[string]bool{}
	for _, tab := range SettingTabs {
		for _, s := range tab.Settings {
			have[s.Key] = true
			if err := s.Save(path, s.Get(defaults)); err != nil {
				t.Errorf("%s: saving its default %q: %v", s.Key, s.Get(defaults), err)
			}
		}
	}
	loaded, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tab := range SettingTabs {
		for _, s := range tab.Settings {
			if s.Get(loaded) != s.Get(defaults) {
				t.Errorf("%s: saved default loads as %q, want %q", s.Key, s.Get(loaded), s.Get(defaults))
			}
		}
	}

	fileOnly := map[string]bool{"onboarding": true, "keys": true, "restore.resume": true}
	var walk func(prefix string, t reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := range typ.NumField() {
			field := typ.Field(i)
			key := field.Tag.Get("toml")
			if prefix != "" {
				key = prefix + "." + key
			}
			switch {
			case fileOnly[key]:
			case field.Type.Kind() == reflect.Struct && field.Type != reflect.TypeFor[Duration]():
				walk(key, field.Type)
			case !have[key]:
				t.Errorf("config key %q has no setting; add one or list it as file-only", key)
			}
		}
	}
	walk("", reflect.TypeFor[Config]())
}
