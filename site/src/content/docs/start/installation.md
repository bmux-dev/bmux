---
title: "Install and start"
description: "Install bmux with Go 1.26+ on macOS or Linux, start your first session, and detach and return."
sidebar:
  order: 1
---

Requires Go 1.26+ and an interactive terminal on macOS or Linux.

```sh
go install github.com/josephferrero/bmux/cmd/bmux@latest
bmux
```

Go's binary directory must be on `PATH`. Check `go env GOBIN`; when empty, use
`$(go env GOPATH)/bin`, usually `~/go/bin`.

Releases after v0.5.0 also carry prebuilt binaries for Linux and macOS on
amd64 and arm64, so Go isn't needed: download `bmux_<os>_<arch>.tar.gz` and
`checksums.txt` from the [releases page](https://github.com/bmux-dev/bmux/releases),
check that `sha256sum bmux_<os>_<arch>.tar.gz` (`shasum -a 256` on macOS)
matches its line in `checksums.txt`, and put the `bmux` inside on your `PATH`.
The macOS binaries aren't signed: one downloaded with a browser is blocked
until you run `xattr -d com.apple.quarantine bmux`; `curl` downloads aren't
affected.

The server starts automatically. Sessions survive detach and client exit. When
the server stops or the machine reboots, the next server restores them: see
[Restarts and reboots](/bmux/guides/restarts/).

The first time a client attaches, a welcome screen over the session covers the
prefix, Ctrl-h/j/k/l and the sidebar. **Enter**, **Right** or **l** continues
to the [agent integrations](/bmux/agents/integrations/) panel; **Esc** skips it.
Either way bmux adds `onboarding = false` to the top of the
[config file](/bmux/configuration/config-file/), creating it if needed, so it isn't shown
again; set it to `true` to see it on the next attach.

## Detach and come back

Press **Ctrl-a d** to detach. Return with:

```sh
bmux attach
```

This attaches to the most recently used session, including after
`bmux restart`. Running bare `bmux` again always creates another session. Use
`bmux list` to see sessions, or `bmux attach NAME` to choose one.
`bmux kill NAME` terminates a session and all its panes.
