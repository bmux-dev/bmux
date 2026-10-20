# bmux

bmux is a Go terminal multiplexer focused on persistent sessions, a fast session
picker, and modeless terminal selection. Keep terminal state on the server and
clipboard access in the attached client. Never claim that saved workspaces can
restore arbitrary running processes after a reboot.

Use `go test ./...`, `go test -race ./...`, and `go vet ./...` to verify changes.
Do not introduce mandatory copy mode. Keep protocol and session lifecycle changes
covered by tests. Do not spawn subagents unless requested by the user.

File a GitHub issue for every bug or feature before fixing it, labeled `bug` or
`enhancement`, with Problem, Cause, Expected and Done when sections. Never
commit to `main`: work on a `feat/`, `fix/` or `docs/` branch and open a pull
request that closes its issues with `Fixes #N`.
