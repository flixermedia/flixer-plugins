#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
release="$root/0.1.1"
staging=$(mktemp -d)
archive="$release/package.tar.gz"
trap 'rm -rf "$staging"' EXIT

mkdir -p "$release"
if [ -e "$archive" ]; then
	printf 'Release already exists: %s\n' "$archive" >&2
	exit 1
fi
GOOS=wasip1 GOARCH=wasm go -C "$root" build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o "$staging/oauth.wasm" .
cp "$root/plugin.json" "$root/settings.schema.json" "$staging/"
tar -C "$staging" --sort=name --mtime='UTC 2026-01-01' --owner=0 --group=0 --numeric-owner -cf - plugin.json settings.schema.json oauth.wasm | gzip -n > "$archive"
cp "$root/plugin.json" "$release/plugin.json"

printf 'Package: %s\nSHA-256: ' "$archive"
sha256sum "$archive" | cut -d ' ' -f 1