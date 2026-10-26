# Flixer Example Plugin

`flixer.example` is an installable example of the currently supported Flixer backend plugin contract.

## What it demonstrates

- An authenticated `POST /api/plugins/flixer.example/hello` route.
- An operation-specific `api.routes.hello` permission.
- A `fetch` operation (1.1.0) that uses the host-call protocol to perform an HTTPS GET of `https://example.com/`, gated by `api.routes.fetch` and `network.http.example.com`.
- Plugin-wide persistent storage using the `storage.plugin.*` permissions.
- Per-user isolated persistent storage using the `storage.user.*` permissions.
- A WASI module compiled for Go's `wasip1/wasm` target.

The route returns a greeting and independent invocation counts. The plugin-wide count is shared by all users; each authenticated user gets a separate user-scoped count.

## Install and try it

1. In **Admin → Plugins**, discover **Flixer Example Plugin**, install version `1.0.0`, and approve all five listed permissions.
2. Enable the plugin.
3. While authenticated to Flixer, send:

   ```http
   POST /api/plugins/flixer.example/hello
   Content-Type: application/json

   { "name": "Ada" }
   ```

4. Repeat the request. The response includes the incremented plugin-wide and user-specific counts. Calling as another user increments the shared count but starts that user's count independently.

Example response:

```json
{
  "message": "Hello, Ada!",
  "operation": "hello",
  "authenticated": true,
  "pluginInvocationCount": 1,
  "userInvocationCount": 1
}
```

The counters demonstrate persisted state for sequential calls. Concurrent calls may read the same previous counter before either writes its patch; the storage contract currently guarantees atomic patches, not atomic read-modify-write counters.

The route requires a valid Flixer bearer-authenticated session. Do not call the internal indexer invocation endpoint directly; it is a signed service-to-service endpoint.

## Build and test

From this directory, run `go test ./...`. From the repository root, run `./plugins/example/build.sh` to compile the WASI module and refresh the archive digest and size. The built artifact targets the WASI Preview 1 runner used by Flixer's indexer.

## Protocol

The module reads one JSON request from standard input and writes one JSON response to standard output. It consumes the version 1 backend request envelope. It returns a `result` and storage `set` patches; the host only supplies requested storage scopes and only persists scopes for which the installation has approved write permissions. The module has no direct filesystem, database, network, environment, or application-secret access.

The module is intentionally small. It is a contract example, not a feature plugin or a replacement for the pending Requests, SkipMe.db, or Theme Songs migrations.
