---
title: "Runtime files"
description: "Where bmux keeps its socket, logs and saved sessions, how to stop the server and how to uninstall."
sidebar:
  order: 2
---

The runtime directory is chosen in this order:

1. `BMUX_RUNTIME_DIR`, if set. Relative paths are resolved from the invoking directory.
2. `$XDG_RUNTIME_DIR/bmux`, if `XDG_RUNTIME_DIR` is set.
3. On macOS, `~/Library/Caches/bmux`.
4. On Linux, `$XDG_CACHE_HOME/bmux`, or `~/.cache/bmux` if unset.

Use an absolute path for `BMUX_RUNTIME_DIR` so clients find the same server.
The directory must belong to your user, have mode **0700**, and not be a symlink.
bmux creates it if missing. Changing the directory selects a separate server;
it does not move existing sessions.

| File | Purpose |
| --- | --- |
| `server.log` | Server output and startup errors |
| `server.pid` | Background server's process ID |
| `server.sock` | Local client/server connection |
| `server.lock` | Prevents duplicate servers in this directory |

## Stop the server

Use **Ctrl-a d** to detach and leave work running. To upgrade, use
`bmux restart` instead of stopping the server by hand; see
[Restarts and reboots](/bmux/guides/restarts/). A server stopped with SIGTERM
also saves its sessions first.

From a shell outside bmux, resolve the directory using the same environment
you used to launch bmux:

```sh
if [ -n "$BMUX_RUNTIME_DIR" ]; then
  runtime_dir="$BMUX_RUNTIME_DIR"
elif [ -n "$XDG_RUNTIME_DIR" ]; then
  runtime_dir="$XDG_RUNTIME_DIR/bmux"
elif [ "$(uname -s)" = Darwin ]; then
  runtime_dir="$HOME/Library/Caches/bmux"
else
  runtime_dir="${XDG_CACHE_HOME:-$HOME/.cache}/bmux"
fi

kill -TERM "$(cat "$runtime_dir/server.pid")"
```

Wait for `server.pid` to disappear before restarting. The next `bmux` command
starts a new server, which restores the saved sessions. The log remains
available for troubleshooting. To stop for good without restoring later, kill
the sessions first with `bmux kill NAME`.

## Uninstall

Stop the server first using the instructions above. Removing the executable
alone does not stop running sessions.

For a Go installation, remove the installed binary:

```sh
go_bin="$(go env GOBIN)"
if [ -z "$go_bin" ]; then
  go_bin="$(go env GOPATH)/bin"
fi
rm "$go_bin/bmux"
```

If you moved the binary or built from source, remove that executable instead;
`command -v bmux` locates the one on your `PATH`. After shutdown, you can remove
`server.log` and `server.lock` from the runtime directory. Remove any `BMUX_*`
settings you added to your shell configuration.
