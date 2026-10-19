package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const onboardedLine = "onboarding = false          # welcome screen seen; true shows it again\n"

// MarkOnboarded records that the welcome screen was seen by setting the
// top-level onboarding = false in the config file at path, creating the file
// if needed. An existing top-level onboarding line is replaced; otherwise
// the line goes first, which is always top-level in TOML. Everything else is
// kept as written, and nothing is written unless the result still parses.
func MarkOnboarded(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			break // tables start; later keys aren't top-level
		}
		if key, _, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(key) == "onboarding" {
			lines[i] = onboardedLine
			replaced = true
			break
		}
	}
	updated := strings.Join(lines, "")
	if !replaced {
		updated = onboardedLine + updated
	}
	var check Config
	if _, err := toml.Decode(updated, &check); err != nil || check.Onboarding {
		return fmt.Errorf("%s: can't record that the welcome screen was seen: %v", path, err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config.toml.bmux-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(updated); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
