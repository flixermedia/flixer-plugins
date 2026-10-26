# Official Flixer Plugins

This repository is the catalog for official Flixer plugins. Flixer reads `repository.json` to discover versioned releases.

The formal schemas are [`plugin-manifest-v1.schema.json`](schemas/plugin-manifest-v1.schema.json) and [`repository-manifest-v1.schema.json`](schemas/repository-manifest-v1.schema.json).

## Catalog format

The root `repository.json` uses manifest version 1. Each plugin has a globally unique ID and one or more releases. A release points to a plugin manifest and a compressed package and includes the package SHA-256 digest and optional size. Package paths are relative to this repository and must not escape its root.

Release packages are immutable. Publish a new version rather than replacing an existing artifact. Flixer validates the plugin manifest, compatibility, package digest, and archive paths before installation.

## Official plugins

Official plugins are added to the catalog only after their implementations and release artifacts are available. The planned migrations are `flixer.skipmedb`, `flixer.requests`, and `flixer.theme-songs`.