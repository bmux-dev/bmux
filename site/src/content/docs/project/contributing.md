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

Releases are built by [GoReleaser](https://github.com/bmux-dev/bmux/blob/main/.goreleaser.yaml)
from a tag; see [Releasing](#releasing). `goreleaser release --snapshot --clean`
builds the same archives locally into `dist/`.

## Workflow

File a GitHub issue for every bug or feature before fixing it, labeled `bug` or
`enhancement`, with Problem, Cause, Expected and Done when sections. Never
commit to `main`: work on a `feat/`, `fix/` or `docs/` branch and open a pull
request that closes its issues with `Fixes #N`.

## Comparing with tmux

`scripts/compare-tmux.py` measures bmux from your checkout against the installed
tmux and prints Markdown tables for three things:

- keystroke echo latency, through an attached client;
- how long `seq 1 1000000` takes to display;
- server memory, idle, with scrollback at 80 and 200 columns, and with 20
  sessions.

Each program gets a throwaway socket and config and a client in a pty, and
tmux's `history-limit` matches bmux's `scrollback.lines`. A full run takes a
few minutes; `--quick` takes about one. It needs Go, tmux and Python 3. Run it
before and after a change to the server, the client or the emulator to see
its effect.

## Releasing

A release is a tag on `main`. Pushing it runs CI and then GoReleaser
(`.github/workflows/release.yml`), which uploads `bmux_<os>_<arch>.tar.gz` for
Linux and macOS on amd64 and arm64, plus `checksums.txt`, to a **draft**
release. Nothing is public until you publish the draft. The version comes from
the tag: no file in the code holds it.

You need push access and the [GitHub CLI](https://cli.github.com/) (`gh`),
logged in. Set the version once; the commands below use it:

```sh
V=v0.5.3
```

### 1. Pick the version

bmux is 0.x, and every release is a pre-release. Bump the patch number for
anything within the current roadmap milestone, and the minor number when a
milestone ships (v0.6 is scripting). Check whether the protocol changed since
the last release; the release notes must then say to run `bmux restart`:

```sh
git fetch origin --tags
git diff "$(git describe --tags --abbrev=0 origin/main)" origin/main -- internal/protocol/protocol.go | grep 'const Version'
```

It prints the old and new `const Version` lines when the protocol changed, and
nothing when it didn't.

### 2. Release pull request

`main` only changes through pull requests, so the release commit is one too.
Check that `main` is green first (`gh run list --branch main --limit 3`).

```sh
git switch -c "docs/release-$V" origin/main
```

Update:

- `README.md`: the **vX.Y.Z pre-release for macOS and Linux** line near the top.
- `ROADMAP.md`: the **Shipped** row of the table at the top; `Shipped in vX.Y.Z`
  over the milestone's table when it's done; and a `vX.Y.Z added …` paragraph at
  the end of **Already built**, matching the earlier ones.
- Any docs example that names a version that doesn't exist yet, such as
  `BMUX_VERSION=` in `install.sh` and the installation page.

```sh
git commit -am "Release $V"
git push -u origin "docs/release-$V"
gh pr create --title "Release $V" --body "Marks $V shipped in the README and ROADMAP."
gh pr checks --watch && gh pr merge --squash --delete-branch
```

### 3. Tag and build

Tag the merge commit on `main` with an annotated tag:

```sh
git fetch origin
git tag -a "$V" -m "bmux $V" origin/main
git push origin "$V"
```

Wait a few seconds for the run to start, then watch it:

```sh
gh run watch "$(gh run list --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')" --exit-status
```

### 4. Check the draft

It should hold five files, and every archive should match `checksums.txt`:

```sh
gh release view "$V" --json isDraft,assets --jq '.isDraft, .assets[].name'
dir=$(mktemp -d) && gh release download "$V" -D "$dir" && cd "$dir"
sha256sum -c checksums.txt          # on macOS: shasum -a 256 -c checksums.txt
tar -xzf bmux_linux_amd64.tar.gz bmux && ./bmux --version   # your platform's archive
cd - && rm -rf "$dir"
```

`bmux --version` must print the version without the `v`.

### 5. Notes and publish

Write the notes in a file, following the earlier releases: a one-line summary;
the install commands (the `curl … install.sh | sh` line and
`go install github.com/josephferrero/bmux/cmd/bmux@vX.Y.Z`); then **New**,
**Changed** and **Fixed** lists as they apply; and an **Upgrading** line that
says whether the protocol changed and, if it did, to run `bmux restart`
(programs running in panes end; sessions, windows, layouts, directories and
agent conversations come back).

```sh
gh release edit "$V" --title "bmux $V" --notes-file notes.md --draft=false --prerelease --latest
```

### 6. Try it

```sh
curl -fsSL https://raw.githubusercontent.com/bmux-dev/bmux/main/install.sh | BMUX_INSTALL_DIR="$(mktemp -d)" sh
```

It should install the new version. From the previous release, `bmux update --check`
should offer it.

### 7. Record its fixtures

The [compatibility](/bmux/reference/compatibility/) tests keep every release's
save file, config and command list working. Record the new one's, in a pull
request like any other change (it needs `tmux`):

```sh
git switch -c "test/fixtures-$V" origin/main
scripts/compat-fixtures.sh "$V"
go test ./...
git add "testdata/releases/$V" && git commit -m "test: compatibility fixtures for $V"
```

### When something fails

- **Before publishing** (CI fails, GoReleaser fails, the draft looks wrong):
  nothing is public. Delete the draft and the tag, fix it in a pull request,
  and start again at step 3:

  ```sh
  gh release delete "$V" --cleanup-tag --yes   # when a draft exists
  git push origin ":refs/tags/$V"               # when only the tag exists
  git tag -d "$V"
  ```

- **After publishing:** don't move or reuse the tag. Go's module proxy keeps
  the first commit it saw for each version, so `go install …@vX.Y.Z` would no
  longer match the release. Fix it in a new patch release.
