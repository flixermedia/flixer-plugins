# SkipMe.db

`flixer.skipmedb` supplies crowd-sourced **intro, recap, credits and preview** timestamps from a SkipMe.db compatible service. Flixer's player uses them for skip buttons, alongside chapter markers and administrator overrides (which still take priority).

## Behaviour

Ported from Flixer's built-in lookup:

- Movies are matched by TMDB, TVDB or IMDb id; episodes by their series' ids plus season and episode number.
- Among the service's entries, the one recorded against a file within 5 seconds of your file's length with the most submissions wins.
- A type with no usable entry clears any previously stored SkipMe.db segment; a failed or empty reply leaves stored segments unchanged.
- Lookups are cached by Flixer for 24 hours per item, whether they succeed or not.
- The service URL is the existing **skipme_api_url** admin setting (default `https://database.skipme.deno.net/v3`; empty turns lookups off).

## Permissions

| Permission | Why |
| --- | --- |
| `media.segments.lookup` | Lets Flixer ask this plugin for segments. |
| `network.http.database.skipme.deno.net` | Lets the plugin POST to the default service host. If you point `skipme_api_url` at another host, install a build that declares it; requests to unapproved hosts are denied. |

The module has no filesystem, environment or direct network access. It asks Flixer to perform the HTTPS request using the host-call protocol.

## While installed

When this plugin is enabled and its permissions are approved, Flixer delegates lookups to it. When it is disabled or absent, Flixer's built-in lookup is used, so existing installations keep working during the transition. Stored segments are never deleted by installing or disabling the plugin.

## Build

```sh
sh plugins/skipmedb/build.sh 1.0.0
```

Builds a reproducible `package.tar.gz` and updates its digest in `repository.json`. Published versions are immutable.
