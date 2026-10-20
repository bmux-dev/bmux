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

	"github.com/josephferrero/bmux/internal/client"
	"github.com/josephferrero/bmux/internal/protocol"
	"github.com/josephferrero/bmux/internal/update"
	"github.com/josephferrero/bmux/internal/version"
	"golang.org/x/sys/unix"
)

// runUpdate replaces this binary with the newest release after checking it
// against the release's checksums. It never restarts the server: that ends
// the programs running in panes, so the user decides when.
func runUpdate(socket string, args []string) error {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	check := flags.Bool("check", false, "only report whether a newer release exists")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: bmux update [--check]")
	}
	current := version.String()
	if current == version.Dev {
		return fmt.Errorf("this is a development build, which bmux update doesn't replace; install a release with the install script or go install")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	source := update.GitHub()
	release, found, err := source.Latest(ctx)
	if err != nil {
		return fmt.Errorf("couldn't look for releases: %w", err)
	}
	if !found {
		fmt.Printf("No release has a bmux for %s/%s yet; this is bmux %s\n", runtime.GOOS, runtime.GOARCH, current)
		return nil
	}
	if !update.Newer(release.Version, current) {
		fmt.Printf("bmux %s is the latest\n", current)
		return nil
	}
	if *check {
		fmt.Printf("bmux %s is available (this is %s): run bmux update\n", release.Version, current)
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
// has its symlinks resolved, so a Homebrew install is under a Cellar.
func replaceable(exe string, writable func(dir string) error) error {
	if strings.Contains(exe, "/Cellar/") {
		return fmt.Errorf("%s is managed by Homebrew; update it with brew upgrade", exe)
	}
	dir := filepath.Dir(exe)
	if err := writable(dir); err != nil {
		return fmt.Errorf("can't write to %s (%v); re-run the install script with BMUX_INSTALL_DIR set to a directory you own, or run sudo bmux update", dir, err)
	}
	return nil
}

func writable(dir string) error {
	err := unix.Access(dir, unix.W_OK)
	if errors.Is(err, unix.EACCES) || errors.Is(err, unix.EROFS) || errors.Is(err, unix.EPERM) {
		return errors.New("permission denied")
	}
	return err
}
