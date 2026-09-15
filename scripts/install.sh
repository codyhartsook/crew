#!/bin/sh
# Install the crew CLI from a GitHub Release.
set -eu

repo="codyhartsook/multiplayer"
version="${CREW_VERSION:-latest}"
destination="${CREW_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*) echo "crew: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) echo "crew: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="multiplayer_${os}_${arch}.tar.gz"
release="https://github.com/${repo}/releases"
if [ "$version" = latest ]; then
	base="${release}/latest/download"
else
	base="${release}/download/${version}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

curl --fail --location --silent --show-error --output "$tmp/$asset" "$base/$asset"
curl --fail --location --silent --show-error --output "$tmp/checksums.txt" "$base/checksums.txt"

expected="$(awk -v asset="$asset" '$2 == asset { print $1 }' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then
	echo "crew: checksum missing for $asset" >&2
	exit 1
fi
if command -v shasum >/dev/null 2>&1; then
	actual="$(shasum -a 256 "$tmp/$asset" | awk '{ print $1 }')"
elif command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$tmp/$asset" | awk '{ print $1 }')"
else
	echo "crew: need shasum or sha256sum to verify the download" >&2
	exit 1
fi
if [ "$actual" != "$expected" ]; then
	echo "crew: checksum verification failed" >&2
	exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp" crew
mkdir -p "$destination"
install -m 0755 "$tmp/crew" "$destination/crew"

echo "crew installed to $destination/crew"
case ":$PATH:" in
*":$destination:"*) ;;
*) echo "add $destination to PATH, then run: crew init" ;;
esac
