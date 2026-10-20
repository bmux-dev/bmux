#!/bin/sh
# Install bmux from a GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/bmux-dev/bmux/main/install.sh | sh
#
# Environment:
#   BMUX_VERSION=v0.6.0   install this release instead of the newest one
#   BMUX_INSTALL_DIR=DIR  install into DIR instead of ~/.local/bin
#   BMUX_RELEASE_URL=URL  download URL/TAG/ASSET from a mirror (needs BMUX_VERSION)
#
# The archive is checked against the release's checksums.txt before anything
# is installed. Nothing is written outside the install directory, and sudo is
# never used.
#
# Everything runs inside main, called on the last line, so a download cut
# short runs nothing.

set -eu

repo="bmux-dev/bmux"

say() { printf '%s\n' "$*"; }
fail() {
	printf 'bmux install: %s\n' "$*" >&2
	exit 1
}

# fetch URL FILE downloads over HTTPS (any scheme for a BMUX_RELEASE_URL mirror).
fetch() {
	if command -v curl >/dev/null 2>&1; then
		if [ -n "${BMUX_RELEASE_URL:-}" ]; then
			curl -fsSL -o "$2" "$1"
		else
			curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
		fi
	elif command -v wget >/dev/null 2>&1; then
		if [ -n "${BMUX_RELEASE_URL:-}" ]; then
			wget -q -O "$2" "$1"
		else
			wget -q --https-only -O "$2" "$1"
		fi
	else
		fail "needs curl or wget"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		fail "needs sha256sum or shasum to verify the download"
	fi
}

platform() {
	case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "no prebuilt bmux for $(uname -s); install with Go instead: go install github.com/josephferrero/bmux/cmd/bmux@latest" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "no prebuilt bmux for $(uname -m); install with Go instead: go install github.com/josephferrero/bmux/cmd/bmux@latest" ;;
	esac
}

# latest_tag prints the newest release with an archive for this platform,
# pre-releases included: every 0.x release is one, so GitHub's
# /releases/latest has nothing to return.
latest_tag() {
	fetch "https://api.github.com/repos/$repo/releases?per_page=30" "$tmp/releases.json" ||
		fail "couldn't list releases from GitHub; set BMUX_VERSION to pick one"
	url=$(grep -o "\"browser_download_url\": *\"[^\"]*/$asset\"" "$tmp/releases.json" | head -n 1 | sed 's/.*"\(https[^"]*\)"$/\1/')
	[ -n "$url" ] || fail "no release has $asset yet; install with Go instead: go install github.com/josephferrero/bmux/cmd/bmux@latest"
	tag=${url%/*}
	printf '%s\n' "${tag##*/}"
}

path_hint() {
	case "$(basename "${SHELL:-sh}")" in
	zsh) say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.zshrc" ;;
	bash) say "  echo 'export PATH=\"$1:\$PATH\"' >> ~/.bashrc" ;;
	fish) say "  fish_add_path $1" ;;
	*) say "  export PATH=\"$1:\$PATH\"  (add it to your shell's startup file)" ;;
	esac
}

main() {
	platform
	asset="bmux_${os}_${arch}.tar.gz"
	dir=${BMUX_INSTALL_DIR:-"$HOME/.local/bin"}

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t bmux)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' INT TERM

	if [ -n "${BMUX_VERSION:-}" ]; then
		tag=$BMUX_VERSION
		case "$tag" in v*) ;; *) tag="v$tag" ;; esac
	elif [ -n "${BMUX_RELEASE_URL:-}" ]; then
		fail "BMUX_RELEASE_URL needs BMUX_VERSION"
	else
		tag=$(latest_tag)
	fi
	base=${BMUX_RELEASE_URL:-"https://github.com/$repo/releases/download"}
	base="${base%/}/$tag"

	say "Installing bmux $tag for $os/$arch"
	fetch "$base/$asset" "$tmp/$asset" || fail "couldn't download $base/$asset"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "couldn't download $base/checksums.txt"

	want=$(awk -v name="$asset" '$2 == name { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || fail "checksums.txt has no entry for $asset"
	got=$(sha256 "$tmp/$asset")
	[ "$got" = "$want" ] || fail "checksum mismatch for $asset (expected $want, got $got); nothing was installed"

	tar -xzf "$tmp/$asset" -C "$tmp" bmux || fail "couldn't unpack $asset"

	mkdir -p "$dir" || fail "couldn't create $dir; set BMUX_INSTALL_DIR to a directory you can write"
	[ -w "$dir" ] || fail "can't write to $dir; set BMUX_INSTALL_DIR to a directory you can write"
	# Same directory, so the rename is atomic and a running bmux keeps its file.
	cp "$tmp/bmux" "$dir/.bmux.install.$$"
	chmod 755 "$dir/.bmux.install.$$"
	mv -f "$dir/.bmux.install.$$" "$dir/bmux"

	say "Installed $("$dir/bmux" --version) to $dir/bmux"

	case ":$PATH:" in
	*":$dir:"*)
		found=$(command -v bmux 2>/dev/null || true)
		if [ -n "$found" ] && [ "$found" != "$dir/bmux" ]; then
			say "Note: $found comes first on your PATH, so that one runs. Remove it or put $dir first."
		fi
		;;
	*)
		say "$dir isn't on your PATH. Add it with:"
		path_hint "$dir"
		;;
	esac

	if command -v pgrep >/dev/null 2>&1 && pgrep -u "$(id -u)" -f 'bmux --serve' >/dev/null 2>&1; then
		say "A bmux server is running. Run bmux restart to switch it to this version; programs running in panes end, sessions come back."
	else
		say "Run bmux to start."
	fi
}

main "$@"
