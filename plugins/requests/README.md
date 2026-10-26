# Requests

`flixer.requests` is a port of Flixer's built-in Requests feature. It adds a **Requests** page to the main navigation where users discover movies, series and albums, search, request them and follow their requests. Administrators approve or decline requests, manage Sonarr, Radarr and Lidarr servers, and set per-user limits.

The page is server-driven: the plugin's `page` operation returns a UI tree that Flixer renders.

## Operations

| Operation | Who | What |
| --- | --- | --- |
| `page` | user, admin | Builds the page for the current query (`tab`, `q`, `type`, `status`, `page`, `view=details`, `edit`). |
| `request` | user, admin | Creates a request (quota, duplicate and library checks; sent straight to the server when auto-approved). |
| `cancel` | user (own pending), admin (any) | Removes a request. |
| `approve`, `retry`, `decline` | admin | Sends a request to its server, or declines it with an optional reason. |
| `service-save`, `service-delete`, `service-test` | admin | Manage and test Sonarr/Radarr/Lidarr servers. |
| `user-limit-save` | admin | Per-user can-request, auto-approve and quota overrides. |
| `sync` | scheduler (every 300 s) | Marks approved requests as available once they are in the library. |

## Data

Tables `requests`, `services` and `user_limits` are plugin-owned. Server API keys are stored with `secret.set` (`arr.<service id>`), never in tables or UI payloads. Server addresses must be public `https://` URLs.

TMDB (movies, series) and Discogs (albums) credentials and the global defaults (can request, auto-approve, quota, notifications) are plugin settings.

## Operation limits

Every operation fits four host-call rounds of at most eight calls. Multi-round operations use a continuation that re-runs the operation and replays stored results.

## Build

```sh
sh plugins/requests/build.sh 1.0.0
go test ./plugins/requests/...
```
