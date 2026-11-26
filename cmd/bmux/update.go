package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bmux-dev/bmux/internal/client"
	"github.com/bmux-dev/bmux/internal/config"
	"github.com/bmux-dev/bmux/internal/protocol"
	"github.com/bmux-dev/bmux/internal/update"
	"github.com/bmux-dev/bmux/internal/version"
	"golang.org/x/sys/unix"
)

// runUpdate replaces this binary with the newest release after checking it
// against the release's checksums. It never restarts the server: that ends
// the programs running in panes, so the user decides when.
func runUpdate(socket string, args []string) error {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	check := flags.Bool("check", false, "only report whether a newer release exists")
	beta := flags.Bool("beta", false, "check or install from the beta channel (includes pre-releases)")
	stable := flags.Bool("stable", false, "check or install from the stable channel (can step back from a beta)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: bmux update [--check] [--beta|--stable]")
	}
	if *beta && *stable {
		return fmt.Errorf("cannot combine --beta and --stable")
	}
	current := version.String()
	if current == version.Dev {
		return fmt.Errorf("this is a development build, which bmux update doesn't replace; install a release with the install script or go install")
	}
	channel := resolveUpdateChannel(*beta, *stable)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	source := update.GitHub()
	source.Channel = channel
	release, found, err := source.Latest(ctx)
	if err != nil {
		return fmt.Errorf("couldn't look for releases: %w", err)
	}
	if !found {
		fmt.Printf("No release has a bmux for %s/%s yet; this is bmux %s\n", runtime.GOOS, runtime.GOARCH, current)
		return nil
	}
	newer := update.Newer(release.Version, current)
	stepBack := *stable && update.IsPrerelease(current) && !update.IsPrerelease(release.Version) && release.Version != current
	if !newer && !stepBack {
		if channel == update.ChannelStable && update.IsPrerelease(current) {
			fmt.Printf("bmux %s is ahead of the latest stable release (%s); run bmux update --stable to switch back to stable, or bmux update --beta to check for newer betas\n", current, release.Version)
			return nil
		}
		fmt.Printf("bmux %s is the latest\n", current)
		return nil
	}
	if *check {
		switch {
		case stepBack:
			fmt.Printf("bmux %s is the latest stable release (this is %s): run bmux update --stable to switch back\n", release.Version, current)
		case *beta:
			fmt.Printf("bmux %s is available (this is %s): run bmux update --beta\n", release.Version, current)
		default:
			fmt.Printf("bmux %s is available (this is %s): run bmux update\n", release.Version, current)
		}
		return nil
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return fmt.Errorf("can't find this binary: %w", err)
	}
	if err := replaceable(exe, writable); err != nil {
		return err
	}
	if !version.Stamped() {
		fmt.Println("Replacing this go install build with the release binary, which works the same.")
	}
	fmt.Printf("Downloading bmux %s…\n", release.Version)
	if err := source.Install(ctx, release, exe); err != nil {
		return err
	}
	fmt.Printf("Updated bmux %s → %s (%s)\n", current, release.Version, exe)
	// A running server shows the restart hint; with none running, the next
	// start runs the new binary anyway.
	if _, err := client.Request(socket, protocol.Request{Op: "update-installed", Text: release.Version}); err == nil {
		fmt.Println("Run bmux restart to switch the server; programs running in panes end, sessions come back.")
	}
	return nil
}

// replaceable refuses binaries bmux update shouldn't or can't replace. exe
// has its symlinks resolved, so a Homebrew install is under Cellar or Caskroom.
func replaceable(exe string, writable func(dir string) error) error {
	if strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/Caskroom/") {
		return fmt.Errorf("%s is managed by Homebrew; update it with brew upgrade", exe)
	}
	dir := filepath.Dir(exe)
	if err := writable(dir); err != nil {
		return fmt.Errorf("can't write to %s (%v); re-run the install script with BMUX_INSTALL_DIR set to a directory you own, or run sudo bmux update", dir, err)
	}
	return nil
}

func resolveUpdateChannel(beta, stable bool) string {
	if beta {
		return update.ChannelBeta
	}
	if stable {
		return update.ChannelStable
	}
	if path, err := config.Path(); err == nil {
		cfg, _ := config.Load(path, os.Getenv)
		return cfg.Update.Channel
	}
	return config.Default().Update.Channel
}

func writable(dir string) error {
	err := unix.Access(dir, unix.W_OK)
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EROFS) || errors.Is(err, unix.EPERM) {
		return errors.New("permission denied")
	}
	return err
}
