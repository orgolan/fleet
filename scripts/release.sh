#!/bin/sh
# Build release binaries for linux and macOS (amd64, arm64) into dist/.
#   scripts/release.sh v0.1.0
# It only builds and checksums. Publishing is a separate step you do yourself:
#   gh release create v0.1.0 dist/*
set -eu

VERSION=${1:-}
[ -n "$VERSION" ] || { echo "usage: $0 <version, e.g. v0.1.0>" >&2; exit 2; }

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
GOBIN_PATH=${GO:-$(sh scripts/install.sh check-go | sed -n 's/.*(\(.*\))$/\1/p')}
rm -rf dist && mkdir dist

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
	os=${target%/*}
	arch=${target#*/}
	name=fleet-$VERSION-$os-$arch
	echo "building $name"
	CGO_ENABLED=0 GOOS=$os GOARCH=$arch "$GOBIN_PATH" build -trimpath \
		-ldflags "-s -w -X main.version=$VERSION" -o "dist/$name/fleet" ./cmd/fleet
	cp LICENSE README.md "dist/$name/"
	tar -C dist -czf "dist/$name.tar.gz" "$name"
	rm -rf "dist/$name"
done
(cd dist && sha256sum ./*.tar.gz > SHA256SUMS)
echo "done:"
ls -1 dist
