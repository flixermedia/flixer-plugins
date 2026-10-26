# Requests

`flixer.requests` is a port of Flixer's built-in Requests feature. It adds a **Requests** page to the main navigation where users discover movies, series and albums, search, request them and follow their requests. Administrators approve or decline requests, manage Sonarr, Radarr and Lidarr servers, and set per-user limits.

The page is server-driven: the plugin's `page` operation returns a UI tree that Flixer renders.

## Operations

| Operation | Who | What |
| --- | --- | --- |
| `page` | user, admin | Builds the page for the current query (`tab`, `q`, `type`, `status`, `page`, `view=details`, `edit`). |
| `request` | user, admin | Creates a request (quota, duplicate and library checks; auto-approve and admin requests are stored `approved` and sent right away or by `sync`; others stay `pending`). |
| `search` | user, admin | Capability `search.results`: `{q, type?}` returns a "Not in library" section of TMDB/Discogs results linking to the request details view (≤20 items, 3 rounds). |
| `cancel` | user (own pending), admin (any) | Removes a request. |
| `approve`, `retry`, `decline` | admin | Sends a request to its server, or declines it with an optional reason. |
| `service-save`, `service-delete`, `service-test` | admin | Manage and test Sonarr/Radarr/Lidarr servers. |
| `settings-save` | admin | Validates and saves the Settings tab (switches, quota, language, provider keys). |
| `user-limit-save` | admin | Per-user can-request, auto-approve and quota overrides. |
| `sync` | scheduler (every 300 s) | Sends queued approved requests, confirms sent ones, and marks requests available once they are in the library. |

## Data

Tables `requests` (with `tvdb_id`), `services`, `user_limits` and `settings` (key, json value) are plugin-owned. Server API keys are stored with `secret.set` (`arr.<service id>`), never in tables or UI payloads. Server addresses must be public `https://` URLs.

Configuration lives on the admin-only **Settings** tab; the package ships no settings schema. Non-secret values are rows in `settings`; the TMDB key (`tmdb-api-key`) and Discogs token (`discogs-token`) are secrets, never echoed (the form only shows "Saved"; an empty submit keeps them, a switch removes them). Values from 1.0.0 schema settings are read as fallbacks until a row or secret exists.

Admin tabs: Settings, Servers (empty state with Add Radarr/Sonarr/Lidarr cards, then add, test connection, edit), Users, Requests queue.

Auto-approved and admin requests are stored as `approved` and sent immediately when the four-round host limit allows. `requests.send_attempts` marks a sent-but-unconfirmed (1+) or queued (0) request; the page shows it as "Queued - will be sent shortly". `sync` then confirms it at the server, resends up to 3 times, or marks it `failed` (admin **Retry**). A missing `tvdb_id` (series from before 1.1.0) is filled in lazily, so approving works on the first click.

## Operation limits

Every operation fits four host-call rounds of at most eight calls. Multi-round operations use a continuation that re-runs the operation and replays stored results.

## Build

```sh
sh plugins/requests/build.sh 1.1.0
go test ./plugins/requests/...
```
