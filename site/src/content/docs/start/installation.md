---
title: "Install and start"
description: "Install bmux with a checksum-verified script or Go, start your first session, and keep it updated."
sidebar:
  order: 1
---

Requires an interactive terminal on macOS or Linux. The install script
downloads the newest release for your platform (Linux or macOS, amd64 or
arm64), checks it against the release's `checksums.txt`, and puts `bmux` in
`~/.local/bin`:

```sh
curl -fsSL https://raw.githubusercontent.com/bmux-dev/bmux/main/install.sh | sh
bmux
```

| Variable | Effect |
| --- | --- |
| `BMUX_VERSION=v0.5.1` | Install that release instead of the newest |
| `BMUX_INSTALL_DIR=DIR` | Install into `DIR` instead of `~/.local/bin` |
| `BMUX_RELEASE_URL=URL` | Download `URL/TAG/ASSET` from a mirror; needs `BMUX_VERSION` |

Set them on the `sh` side: `curl -fsSL … | BMUX_VERSION=v0.5.1 sh`. The script
never uses `sudo` and writes nothing outside the install directory. It tells
you if that directory isn't on your `PATH`, or if another `bmux` comes first.
Run it again to upgrade, or use [`bmux update`](/bmux/start/installation/#updates); if a server is
running, `bmux restart` switches it to the new version.

**With Go 1.26+** instead:

```sh
go install github.com/josephferrero/bmux/cmd/bmux@latest
```

Go's binary directory must be on `PATH`. Check `go env GOBIN`; when empty, use
`$(go env GOPATH)/bin`, usually `~/go/bin`.

**By hand:** releases from v0.5.1 on carry `bmux_<os>_<arch>.tar.gz` and
`checksums.txt` on the [releases page](https://github.com/bmux-dev/bmux/releases).
Check that `sha256sum bmux_<os>_<arch>.tar.gz` (`shasum -a 256` on macOS)
matches its line in `checksums.txt`, and put the `bmux` inside on your `PATH`.
The macOS binaries aren't signed: one downloaded with a browser is blocked
until you run `xattr -d com.apple.quarantine bmux`; `curl` downloads, including
the script's, aren't affected.

The server starts automatically. Sessions survive detach and client exit. When
the server stops or the machine reboots, the next server restores them: see
[Restarts and reboots](/bmux/guides/restarts/).

The first time a client attaches, a welcome screen over the session covers the
prefix, Ctrl-h/j/k/l and the sidebar. **Enter**, **Right** or **l** continues
to the [agent integrations](/bmux/agents/integrations/) panel; **Esc** skips it.
Either way bmux adds `onboarding = false` to the top of the
[config file](/bmux/configuration/config-file/), creating it if needed, so it isn't shown
again; set it to `true` to see it on the next attach.

## Updates

A release build looks for newer releases once a day. The server asks GitHub's
releases API (`api.github.com/repos/bmux-dev/bmux/releases`), unauthenticated
and with nothing about you, and caches the answer in `update-check.json` next
to the saved sessions, so restarts don't ask again. Only releases with a binary
for your platform count; while bmux is 0.x, pre-releases do too. A failed check
is logged in `server.log` and retried the next day.

When one is newer than the server, the status bar shows **↑** at its right end,
or `↑ 0.7.0` when there's room, in space the bar leaves free. Clicking it, or
the top of the key list (Ctrl-a ?), says what to do:

| Hint | Meaning |
| --- | --- |
| ↑ | A newer release is available: run `bmux update` |
| ↻ | `bmux update` installed it; the server still runs the old binary until `bmux restart` |

`bmux update` downloads the release's archive for your platform and its
`checksums.txt`, checks the SHA-256, and only then replaces the binary in
place (one rename, so it's never half-written). It doesn't restart the server,
since that ends the programs running in panes; run `bmux restart` when you're
ready. `bmux update --check` only reports whether a release is available.

It refuses, and says what to do instead, for a build from a checkout, a binary
in a directory you can't write (re-run the install script with
`BMUX_INSTALL_DIR`, or `sudo bmux update`), and a Homebrew install
(`brew upgrade`). A `go install` build is replaced with the release binary,
which works the same.

Turn the daily check off with `[update] check = false` in the
[config file](/bmux/configuration/config-file/), or `BMUX_NO_UPDATE_CHECK=1`; builds from a
checkout never check. `bmux update` still works either way.

## Detach and come back

Press **Ctrl-a d** to detach. Return with:

```sh
bmux attach
```

This attaches to the most recently used session, including after
`bmux restart`. Running bare `bmux` again always creates another session. Use
`bmux list` to see sessions, or `bmux attach NAME` to choose one.
`bmux kill NAME` terminates a session and all its panes.
