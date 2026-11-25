# Contributing to bmux

Thank you for contributing to `bmux`!

## Getting started

**Prerequisites:** Go **1.27.3+** (and Node.js if previewing the docs site).

```sh
gofmt -l .
go build -o bin/bmux ./cmd/bmux
./bin/bmux
```

Verify changes before opening a pull request:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Preview documentation changes locally:

```sh
cd site && npm run dev
```

## Workflow

1. **File an issue first** labeled `bug` or `enhancement`, including **Problem**, **Cause** (for bugs), **Expected**, and **Done when** sections.
2. **Never commit to `main`**: branch off `main` using a `feat/`, `fix/`, or `docs/` prefix.
3. **Update docs for user-visible changes**: user docs live under `site/src/content/docs/`, while `ROADMAP.md` and `docs/agents.md` live at their paths and are synced into the site at build time.
4. **Open a pull request** targeting `main` and link the issue with `Fixes #N`.

## Full guide

For OpenCode integration validation, fuzzing, `esctest` conformance, tmux benchmarking, and the release runbook, see the full [Contributing guide](https://bmux-dev.github.io/bmux/project/contributing/) and [AGENTS.md](AGENTS.md).
