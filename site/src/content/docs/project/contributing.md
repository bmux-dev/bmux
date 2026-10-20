---
title: "Contributing"
description: "Report bugs, build from a checkout, run the checks and follow the issue, branch and pull request workflow."
sidebar:
  order: 3
---

Issues and pull requests are welcome. For bugs, include your OS, terminal
emulator, `bmux version`, and steps to reproduce.

Build from a checkout and run the checks:

```sh
go build -o bin/bmux ./cmd/bmux
./bin/bmux
go test ./...
go test -race ./...
go vet ./...
```

Read [AGENTS.md](https://github.com/bmux-dev/bmux/blob/main/AGENTS.md) and the [architecture guide](/bmux/project/architecture/)
before changing terminal or session behavior.

## Workflow

File a GitHub issue for every bug or feature before fixing it, labeled `bug` or
`enhancement`, with Problem, Cause, Expected and Done when sections. Never
commit to `main`: work on a `feat/`, `fix/` or `docs/` branch and open a pull
request that closes its issues with `Fixes #N`.
