# Official Flixer Plugins

This repository is the catalog for official Flixer plugins. Flixer reads `repository.json` to discover versioned releases.

The formal schemas are [`plugin-manifest-v1.schema.json`](schemas/plugin-manifest-v1.schema.json) and [`repository-manifest-v1.schema.json`](schemas/repository-manifest-v1.schema.json).

## Catalog format

The root `repository.json` uses manifest version 1. Each plugin has a globally unique ID and one or more releases. A release points to a plugin manifest and a compressed package and includes the package SHA-256 digest and optional size. Package paths are relative to this repository and must not escape its root.

Release packages are immutable. Publish a new version rather than replacing an existing artifact. Flixer validates the plugin manifest, compatibility, package digest, and archive paths before installation.

## Official plugins

## Example plugin

[`flixer.example`](plugins/example/) is a working WASI backend plugin that demonstrates an authenticated `hello` route and separately scoped plugin-wide and per-user persistent counters. Its package is available from the catalog and can be installed from the Flixer admin Plugins page. The plugin requires explicit approval for each declared permission.

To rebuild its package from the repository root, run:

```sh
./plugins/example/build.sh
```

This requires Go with the `wasip1/wasm` target. The script rebuilds the WASI module, packages the versioned artifact, and updates its SHA-256 and size in `repository.json`. Publish new versions as new immutable release paths; do not replace a published release.

The planned migrations are `flixer.skipmedb`, `flixer.requests`, and `flixer.theme-songs`.