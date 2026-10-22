package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/config"
	"github.com/josephferrero/bmux/internal/protocol"
)

// runTheme switches the chrome theme: it saves the name to the config file,
// then reloads the server so attached clients follow, like bmux reload. It
// is meant for hooks, such as a Neovim ColorScheme autocmd.
func runTheme(socket string, args []string) error {
	return themeCommand(socket, args, os.Stdout, os.Stderr, os.Getenv)
}

func themeCommand(socket string, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	path, err := config.Path()
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0:
		cfg, _ := config.Load(path, getenv)
		fmt.Fprintln(stdout, cfg.Theme)
		return nil
	case len(args) == 1 && args[0] == "--list":
		fmt.Fprintln(stdout, strings.Join(config.ThemeNames, "\n"))
		return nil
	case len(args) != 1 || strings.HasPrefix(args[0], "-"):
		return errors.New("usage: bmux theme [NAME | --list]")
	}
	theme, err := config.SaveTheme(path, args[0])
	if err != nil {
		return err
	}
	if v := getenv("BMUX_THEME"); v != "" {
		fmt.Fprintf(stderr, "bmux: BMUX_THEME=%s overrides the config file; unset it for %s to show\n", v, theme)
	}
	if _, err := client.Request(socket, protocol.Request{Op: "reload-config"}); err != nil {
		var unreachable *net.OpError
		if errors.As(err, &unreachable) {
			fmt.Fprintf(stdout, "Saved theme %s to %s; no server is running, so it applies on the next start\n", theme, path)
			return nil
		}
		return fmt.Errorf("saved theme %s to %s, but the server reported: %w", theme, path, err)
	}
	fmt.Fprintf(stdout, "Theme %s saved to %s\n", theme, path)
	return nil
}
