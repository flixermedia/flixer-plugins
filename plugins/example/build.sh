#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
plugin="$root/plugins/example"
release="$plugin/1.0.0"

mkdir -p "$release/backend"
cp "$plugin/LICENSE" "$release/LICENSE"
(cd "$plugin" && GOOS=wasip1 GOARCH=wasm go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o "$release/backend/plugin.wasm" ./cmd/example)

archive="$release/package.tar.gz"
tarFile="$release/package.tar"
trap 'rm -f "$tarFile"' EXIT
tar -C "$release" --sort=name --mtime='UTC 2026-01-01' --owner=0 --group=0 --numeric-owner -cf "$tarFile" LICENSE plugin.json backend/plugin.wasm
gzip -n -c "$tarFile" > "$archive"
rm -f "$tarFile"
trap - EXIT

digest=$(sha256sum "$archive" | cut -d ' ' -f 1)
size=$(wc -c < "$archive" | tr -d ' ')
REPOSITORY="$root/repository.json" DIGEST="$digest" SIZE="$size" node --input-type=module <<'NODE'
import { readFile, writeFile } from "node:fs/promises";
const path = process.env.REPOSITORY;
const repository = JSON.parse(await readFile(path, "utf8"));
const release = repository.plugins
  .find((plugin) => plugin.id === "flixer.example")
  ?.versions.find((version) => version.version === "1.0.0");
if (!release) throw new Error("flixer.example 1.0.0 is missing from repository.json");
if (/^[a-f0-9]{64}$/.test(release.sha256) && release.sha256 !== process.env.DIGEST) {
  throw new Error("The 1.0.0 artifact is immutable; bump the example plugin version before publishing changes");
}
release.sha256 = process.env.DIGEST;
release.size = Number(process.env.SIZE);
await writeFile(path, `${JSON.stringify(repository, null, 4)}\n`);
NODE
