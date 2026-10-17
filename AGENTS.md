# bmux

bmux is a Go terminal multiplexer focused on persistent sessions, a fast session
picker, and modeless terminal selection. Keep terminal state on the server and
clipboard access in the attached client. Never claim that saved workspaces can
restore arbitrary running processes after a reboot.

Use `go test ./...`, `go test -race ./...`, and `go vet ./...` to verify changes.
Do not introduce mandatory copy mode. Keep protocol and session lifecycle changes
covered by tests. Do not spawn subagents unless requested by the user.
