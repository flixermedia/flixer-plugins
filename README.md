<div align="center">

<img src="https://raw.githubusercontent.com/flixermedia/flixer/main/resources/logo/flixer-logo.png" alt="Flixer logo" width="112" />

# Flixer Plugins

**Extend Flixer, one plugin at a time.**

[![Stars](https://img.shields.io/github/stars/flixermedia/flixer-plugins?style=flat-square)](https://github.com/flixermedia/flixer-plugins/stargazers) [![Issues](https://img.shields.io/github/issues/flixermedia/flixer-plugins?style=flat-square)](https://github.com/flixermedia/flixer-plugins/issues)

[Flixer](https://github.com/flixermedia/flixer) · [Documentation](https://github.com/flixermedia/flixer-docs) · [Report an issue](https://github.com/flixermedia/flixer-plugins/issues)

</div>

---

> [!WARNING]
> **Under active development.** Flixer components and integration contracts may change. Check current source and release notes before upgrading production installations.

> [!NOTE]
> **Role in Flixer:** The official versioned catalogue of Flixer plugins and plugin schemas. For an end-to-end deployment, start with the [Flixer backend repository](https://github.com/flixermedia/flixer).

## ✨ Features

- **Official catalogue:** List plugins and immutable releases in `repository.json`.
- **Schema-defined packages:** Validate repository and plugin manifests against versioned JSON schemas.
- **Integrity checks:** SHA-256 digests and package path validation.
- **Permission-aware installation:** Flixer requires approval of declared plugin permissions.
- **OAuth 2.0:** Configure external sign-in providers for linked Flixer accounts.

## 🏗️ Architecture

Flixer separates application logic from its web UI, media indexing, media delivery, update management, plugins and documentation. **Flixer Plugins** owns the following responsibility: the official versioned catalogue of Flixer plugins and plugin schemas.

For the full platform, the API coordinates services; the browser interacts with the backend and the streamer receives authorised playback requests. Components should be operated as part of the supported Flixer stack unless you are developing or testing them directly.

## 🚀 Getting started

### Browse and develop

Start at [`repository.json`](repository.json) for the release catalogue, [`schemas/`](schemas/) for JSON schemas, and [`plugins/oauth/`](plugins/oauth/) for the OAuth plugin.

To build a new OAuth release from the repository root:

```bash
sh plugins/oauth/build.sh
```

This requires a Go toolchain supporting `GOOS=wasip1` and `GOARCH=wasm`. Set a new version in the plugin manifest and build script before building; the script refuses to overwrite an existing release. Add the new package's SHA-256 and size to the catalogue, then install through Flixer's administrator Plugins page.

## 🧱 Catalogue structure

```text
flixer-plugins/
├── repository.json              # Official plugin index and versions
├── schemas/                    # Repository and plugin JSON schemas
└── plugins/
    └── oauth/                  # Configurable OAuth sign-in plugin
```

Each release references a manifest, an archive and its SHA-256 digest (optionally the size). Paths must remain within the repository. The Flixer installation flow validates metadata, compatibility and archive integrity before use.

Catalogue entries may include `icon` and `banner` paths relative to `repository.json`, for example `"icon": "plugins/oauth/icon.png"`. Images must be PNG, JPEG, WebP or GIF, at most 5 MiB. Flixer displays them in the admin plugin catalogue and safely fetches them from the repository.

## 🧩 Plugin lifecycle

1. Define the plugin and its declared permissions in a manifest.
2. Build and package the plugin for a new version.
3. Add the immutable artifact and its digest to `repository.json`.
4. Validate against the schemas and install through the Flixer admin Plugins page.
5. Require explicit permission approval before enabling capabilities.

## 🧪 Current versus planned plugins

- **Available plugin:** `flixer.oauth`, a configurable OAuth 2.0 sign-in provider.
- **Planned migrations, not shipped in this catalogue as confirmed releases:** `flixer.requests`, `flixer.theme-songs`. SkipMe.db has been removed from the catalogue.

Read [`AI.md`](AI.md), [`repository.json`](repository.json) and both schema files before making catalogue changes.

## 🧩 Flixer ecosystem

| Repository                                                        | Purpose                                                                                   |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| [Flixer](https://github.com/flixermedia/flixer)                   | The self-hosted backend API, authentication, libraries, and administration service.       |
| [Flixer Web](https://github.com/flixermedia/flixer-web)           | The Vue 3 application that powers browsing, sign-in, discovery, and playback interfaces.  |
| [Flixer Streamer](https://github.com/flixermedia/flixer-streamer) | The Go service responsible for authorised media delivery and FFmpeg processing.           |
| [Flixer Indexer](https://github.com/flixermedia/flixer-indexer)   | The Go worker and API service for discovering, identifying, probing, and enriching media. |
| [Flixer Updater](https://github.com/flixermedia/flixer-updater)   | The Go sidecar that orchestrates Docker Compose updates and restores.                     |
| [Flixer Plugins](https://github.com/flixermedia/flixer-plugins)   | The official versioned catalogue of Flixer plugins and plugin schemas.                    |
| [Flixer Docs](https://github.com/flixermedia/flixer-docs)         | The Docusaurus source for Flixer platform documentation.                                  |

## 🤝 Contributing

Contributions, bug reports and documentation improvements are welcome. Open an [issue](https://github.com/flixermedia/flixer-plugins/issues) with reproduction steps or a focused proposal, then submit a pull request against the repository. Keep configuration, examples and documentation aligned with implemented code. For backend contributions, also see the [main contribution guidelines](https://github.com/flixermedia/flixer/blob/main/CONTRIBUTING.md).

## 📄 Licence

Check this repository's [`LICENSE`](LICENSE) file, if present, for the authoritative licensing terms. The [main Flixer backend](https://github.com/flixermedia/flixer/blob/main/LICENSE) uses MIT; licensing for separate components should be verified individually before redistribution.

---

<div align="center">

**Your media. Your server. Your rules.**

Made for the [Flixer](https://github.com/flixermedia) ecosystem.

</div>
