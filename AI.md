# Flixer — Complete Plugin Ecosystem Implementation

## Project

GitHub Organisation: https://github.com/flixermedia

I want you to design and implement a **complete, production-ready, extensible plugin ecosystem for Flixer**.

This is a major architectural update that will affect multiple repositories across the Flixer ecosystem.

The goal is to make Flixer highly modular, allowing first-party and third-party developers to extend, replace, or introduce functionality without directly modifying Flixer's core source code.

**This is an actual implementation request, not a request for a proof of concept, architectural proposal, or example implementation.**

I expect working code integrated across all affected repositories, comprehensive testing, documentation, and migration of existing functionality into plugins.

However, architectural decisions must be based on the existing codebase rather than assumptions.

---

# 1. Mandatory Preparation — Before Changing Any Code

**DO NOT BEGIN MODIFYING SOURCE CODE UNTIL THESE STEPS ARE COMPLETED.**

### 1.1 Analyse All Existing Repositories

Start by examining the entire Flixer GitHub organisation:

https://github.com/flixermedia

Pay particular attention to:

- `flixer` — Main backend and API.
- `flixer-web` — Web frontend and admin dashboard.
- `flixer-indexer` — Indexing and provider functionality.
- `flixer-streamer` — Streaming and media delivery.
- `flixer-docs` — Project documentation.

Also inspect any additional repositories that are relevant.

Understand:

- How the applications communicate.
- Their existing architectures and frameworks.
- How APIs, authentication, and permissions work.
- How settings and database connections are managed.
- How the frontend is built and distributed.
- How the applications start and shut down.
- How Docker deployment and persistent storage work.
- Whether existing functionality is already modular.
- How media providers, playback functionality, and frontend features are currently registered.
- Where SkipMe.db, media requests, and theme song functionality are currently implemented.

Do not assume any functionality exists until verified in the source code.

### 1.2 Back Up Everything to GitHub

Before modifying anything:

1. Identify all existing local changes across all repositories.
2. Preserve both committed and uncommitted changes.
3. Create a timestamped backup branch or equivalent recoverable checkpoint for every affected repository.
4. Push the backups to GitHub.
5. Confirm the backups exist remotely.
6. Record the original commit hashes for rollback.

Never overwrite uncommitted work or force-push over existing changes.

Check for credentials, private keys, `.env` files, and other sensitive information before pushing any backup.

If a backup cannot be completed, stop and explain the problem before making destructive changes.

### 1.3 Establish Baseline Behaviour

Before development, run the existing tests and establish the current functionality of each service.

Record any pre-existing failures so they can be distinguished from regressions introduced by this update.

### 1.4 Architecture Planning

After inspecting the repositories, design the plugin architecture.

Present a concise architecture summary explaining:

- How plugins will be discovered and installed.
- How plugin execution will work across different languages and services.
- How services will communicate with plugins.
- How frontend plugins will integrate with Vue.
- How backend and Go-service extensions will work.
- How capabilities, dependencies, permissions, and versions will be handled.
- How plugin updates and migrations will work.
- How failures will be isolated.

Ask targeted questions where genuinely necessary, particularly before making difficult-to-reverse architectural decisions.

Once those decisions are resolved, continue implementing the complete solution.

---

# 2. Core Plugin System

Create a unified plugin architecture supporting the following applications:

| Application | Plugin responsibilities |
|---|---|
| `flixer` | Backend functionality, APIs, database integration, background jobs, events, authentication-aware features |
| `flixer-indexer` | New indexer providers, metadata sources, search providers, discovery and indexing behaviour |
| `flixer-streamer` | Streaming extensions, playback features, media processing, segment providers and streaming integrations |
| `flixer-web` | Frontend components, pages, routes, navigation items, styles, settings panels and UI integrations |

A plugin must be able to target one application or multiple applications simultaneously.

For example, a plugin might introduce a backend API endpoint, add a frontend settings page, and integrate with the streamer.

All of those components must belong to the same installed plugin rather than requiring three unrelated installations.

## 2.1 Central Plugin Manager

Implement a central plugin manager within the Flixer backend.

It should be responsible for:

- Discovering plugins.
- Installing plugins.
- Enabling and disabling plugins.
- Uninstalling plugins.
- Updating plugins.
- Managing repositories.
- Validating plugin manifests.
- Resolving dependencies.
- Checking compatibility.
- Managing plugin settings.
- Tracking installation status.
- Tracking errors and health.
- Registering capabilities.
- Coordinating plugin activation across services.
- Cleaning up resources when plugins are disabled or removed.

Use the existing Flixer settings database where appropriate.

Plugin metadata, repository configuration, enabled states, version information, and settings must persist across restarts.

## 2.2 Plugin Lifecycle

Provide a clearly defined plugin lifecycle:

1. Discovered
2. Downloaded
3. Validated
4. Installed
5. Initialised
6. Enabled
7. Running
8. Disabled
9. Updated
10. Uninstalled

Support appropriate lifecycle hooks such as:

- `onInstall`
- `onInit`
- `onEnable`
- `onDisable`
- `onUpdate`
- `onUninstall`
- `onShutdown`

These are conceptual hook names; adapt their exact interfaces to the architecture.

Ensure plugins release resources, subscriptions, routes, and background tasks when disabled.

Changes should take effect without a full application restart where technically feasible. Where restart-free changes are unsafe or unsupported, clearly communicate and handle the restart requirement.

---

# 3. Plugin Manifest Specification

Every plugin must include a manifest defining its identity, compatibility, capabilities, dependencies, permissions, and entry points.

Design and document a stable, versioned manifest format.

For example, a hypothetical `plugin.json` could look like:

```json
{
  "manifestVersion": 1,
  "id": "flixer.skipmedb",
  "name": "SkipMe.db",
  "version": "1.0.0",
  "description": "SkipMe.db segment integration for Flixer",
  "author": "Flixer",
  "license": "MIT",
  "homepage": "https://github.com/flixermedia",
  "compatibility": {
    "flixer": ">=1.0.0",
    "pluginApi": "^1.0.0"
  },
  "targets": {
    "flixer": {
      "entry": "backend/index.js"
    },
    "flixer-streamer": {
      "entry": "streamer/"
    },
    "flixer-web": {
      "entry": "web/"
    }
  },
  "capabilities": [
    "media.segments",
    "api.routes",
    "web.settings"
  ],
  "permissions": [
    "media.read",
    "network.external",
    "settings.read",
    "settings.write"
  ],
  "dependencies": {},
  "settings": {
    "schema": "settings.schema.json"
  }
}
```

This is an illustrative structure, not a mandatory implementation.

Design the final schema according to the actual project architecture.

### Manifest Requirements

Each plugin must declare:

- Globally unique plugin identifier.
- Display name.
- Description.
- Author.
- Version.
- Manifest specification version.
- Supported Flixer/plugin API versions.
- Target applications.
- Application-specific entry points.
- Supported capabilities.
- Required permissions.
- Plugin dependencies.
- Optional dependencies.
- Settings schema, if applicable.
- Plugin icon and relevant metadata.

Validate manifests against a formal schema.

Reject invalid, incompatible, or unsupported plugins with useful error messages.

Use semantic versioning where appropriate.

---

# 4. Cross-Service Plugin Architecture

Flixer is a multi-service application, so the plugin framework must properly support multiple execution environments.

Do not try to directly execute TypeScript or JavaScript inside Go services.

Instead, determine a suitable architecture based on the existing source code.

Possible approaches include:

- Typed extension interfaces.
- Service-to-service RPC.
- HTTP-based plugin interfaces.
- A central plugin host.
- Process-isolated plugin workers.
- Event-based integration.
- WebAssembly for compatible extension workloads.

Choose the approach that provides the best combination of performance, security, maintainability, and developer usability.

### Requirements

- One plugin can expose capabilities to multiple services.
- Each service must understand which plugins target it.
- Services must be able to discover and communicate with their supported extensions.
- Authentication and authorisation must be enforced between services.
- Plugin crashes should not bring down Flixer.
- Plugin calls must have appropriate timeouts and error handling.
- Service restarts should restore plugin state correctly.
- Plugins must not depend on undocumented internal implementations.
- Define clear interfaces for plugins to interact with Flixer.

Where practical, provide a shared SDK or specification so plugin developers do not have to manually implement internal communication protocols.

---

# 5. Plugin Storage

All installed plugins must be stored within:

`DATA_DIR/plugins`

A possible directory layout:

```text
DATA_DIR/
└── plugins/
    ├── installed/
    │   ├── flixer.skipmedb/
    │   ├── flixer.requests/
    │   └── flixer.theme-songs/
    ├── cache/
    ├── temp/
    └── data/
        ├── flixer.skipmedb/
        ├── flixer.requests/
        └── flixer.theme-songs/
```

The exact structure can be adjusted as needed.

### Storage Requirements

- Plugin source files and persistent plugin data should be separated.
- Updating a plugin must not destroy its settings or user data.
- Plugins must survive application and Docker container restarts.
- Downloads should use temporary storage and atomic installation where possible.
- Interrupted installations must not leave partially installed plugins.
- Protect against archive path traversal and arbitrary filesystem writes.
- Plugin directories should not be treated as trusted application source.
- Uninstalling a plugin must offer a clear data-retention policy.
- Plugin IDs and installation paths must be validated.

Make sure the storage system works correctly with Flixer's existing Docker setup.

---

# 6. Plugin Repository System

Create a new official repository:

`https://github.com/flixermedia/flixer-plugins`

This will be Flixer's default plugin repository.

It should contain official plugins maintained by the Flixer project.

## 6.1 Repository Manifest

Every plugin repository must contain a root-level manifest.

For example:

```text
flixer-plugins/
├── repository.json
├── README.md
└── plugins/
    ├── skipmedb/
    ├── requests/
    └── theme-songs/
```

The repository manifest should describe the plugins available through that repository.

For example:

```json
{
  "manifestVersion": 1,
  "id": "flixer.official",
  "name": "Official Flixer Plugins",
  "description": "Official plugin repository for Flixer",
  "plugins": [
    {
      "id": "flixer.skipmedb",
      "path": "plugins/skipmedb"
    },
    {
      "id": "flixer.requests",
      "path": "plugins/requests"
    },
    {
      "id": "flixer.theme-songs",
      "path": "plugins/theme-songs"
    }
  ]
}
```

Extend this specification as necessary to support versioning, release metadata, download locations, hashes, and compatibility.

A single repository must support multiple independently installable and independently versioned plugins.

## 6.2 Default Repository Behaviour

The official `flixer-plugins` repository must be registered automatically.

On first run:

1. Initialise the plugin system.
2. Register the official repository.
3. Retrieve its manifest.
4. Discover the available official plugins.
5. Download and install the designated default plugins.
6. Enable the required default plugins.
7. Preserve existing feature settings and data.
8. Record successful initialisation.

This should be idempotent.

Restarting Flixer must not reinstall plugins unnecessarily, overwrite user settings, or re-enable plugins the administrator deliberately disabled.

If GitHub is unavailable, Flixer must continue to operate using already installed plugins.

A failed repository download must not prevent the entire application from starting.

## 6.3 Custom Repositories

Administrators must be able to add additional plugin repositories.

Support repository URLs and implement validation before accepting them.

Custom repositories should support:

- Multiple plugins per repository.
- Plugin discovery.
- Version metadata.
- Compatibility checks.
- Repository refresh.
- Update discovery.
- Removal.

The official repository must be permanently registered and cannot be removed through the admin dashboard or API.

Administrators may install or uninstall optional official plugins, subject to dependencies and feature requirements.

## 6.4 Security and Repository Trust

Treat all third-party repository content as untrusted.

Implement suitable controls for:

- Secure downloads.
- Integrity verification.
- Trusted publisher identification where practical.
- Explicit permission approval.
- Plugin dependency validation.
- Prevention of malicious archive extraction.
- Protection against SSRF and unsafe download destinations.
- Restricting plugin access to application secrets.
- Preventing unauthorised filesystem access.
- Avoiding arbitrary code execution in the frontend.
- Safely handling plugin updates.

A permission declaration alone does not provide security isolation. Enforce permissions through supported APIs and execution boundaries.

Warn administrators when installing code from untrusted repositories.

Do not silently execute arbitrary remote code with unrestricted application privileges.

---

# 7. Admin Dashboard — Plugin Management

Modify `flixer-web` to introduce a fully integrated **Plugins** section within the existing administration dashboard.

Maintain the current design language, navigation patterns, and responsive behaviour.

The experience should feel like a native part of Flixer.

## 7.1 Installed Plugins

Provide a page listing installed plugins.

Display:

- Name.
- Icon.
- Description.
- Author.
- Version.
- Repository source.
- Supported Flixer components.
- Current status.
- Update availability.
- Error/health status.

Each plugin should provide appropriate actions:

- Enable.
- Disable.
- Update.
- Uninstall.
- Open settings.
- View details.
- View logs or errors.

Require confirmation for potentially destructive actions.

## 7.2 Discover Plugins

Provide a plugin browser supporting:

- Searching across registered repositories.
- Browsing available plugins.
- Filtering by target application.
- Filtering by capabilities or category.
- Viewing plugin details.
- Viewing required permissions.
- Viewing compatibility information.
- Installing plugins.
- Identifying already installed plugins.
- Identifying updates.

Searching should work across all registered repositories, not just the official repository.

## 7.3 Manage Repositories

Add a repository management interface.

Administrators must be able to:

- View configured repositories.
- Add repositories.
- Validate repository URLs.
- Refresh repository metadata.
- Remove custom repositories.
- View repository errors.
- Identify the official repository.

The official `flixer-plugins` repository must be visibly marked as an official, non-removable repository.

## 7.4 Plugin Settings

Plugins should be able to declare their configuration requirements.

Support automatically generated settings forms for common types:

- Text.
- Numbers.
- Booleans.
- Dropdowns.
- Passwords/API keys.
- URLs.
- Lists.
- Other schema-supported values.

Plugins may also provide custom frontend settings interfaces when necessary.

Settings must be persisted securely.

Secret values must not be exposed unnecessarily through API responses.

## 7.5 Access Control

Only authenticated users with appropriate administrative permissions may install, update, enable, disable, uninstall, or configure plugins.

The backend must enforce these restrictions independently of frontend visibility.

---

# 8. Frontend Plugin Extension System

Implement a proper extension API within `flixer-web`.

Plugins should be able to extend the frontend without directly editing core Vue source files.

Supported extension capabilities should include:

### 8.1 Pages and Routes

Plugins may register new pages.

Examples:

- `/plugins/requests`
- `/plugins/skipmedb`
- `/plugins/example`

Routes must be namespaced or otherwise protected from collisions.

### 8.2 Navigation

Plugins may register navigation items in approved locations, such as:

- Main navigation.
- Admin navigation.
- User settings.
- Media detail views.

### 8.3 Components and UI Slots

Create defined extension points that plugins can use to inject functionality.

Examples include:

- Movie details.
- TV show details.
- Episode details.
- Media player controls.
- Homepage sections.
- Admin dashboard.
- User profile.
- Settings pages.

Use explicit extension points instead of arbitrary DOM modifications.

### 8.4 CSS and Themes

Plugins should be able to:

- Register stylesheets.
- Add scoped component styles.
- Override documented CSS variables.
- Introduce themes or theme presets where appropriate.

Avoid global CSS conflicts and uncontrolled replacement of application markup.

### 8.5 JavaScript and Vue Components

Support custom frontend functionality through a documented plugin interface.

Investigate safe approaches for loading frontend extensions, including sandboxed execution for untrusted UI where feasible.

Frontend plugin code must not automatically inherit privileged administrator credentials or unrestricted access to internal application APIs.

### 8.6 Build and Deployment Compatibility

The solution must work in production builds and Docker deployments.

Do not design a plugin system that works only in Vue development mode.

Where plugin assets require building or bundling, define a reliable installation and deployment process.

---

# 9. Backend Plugin Extension System

Implement a backend plugin API within `flixer`.

Depending on declared permissions and supported capabilities, plugins should be able to:

- Register namespaced API endpoints.
- Register background jobs.
- Subscribe to application events.
- Access documented media metadata APIs.
- Read or write permitted settings.
- Register integrations.
- Introduce new backend features.
- Provide data to frontend extensions.
- Use supported data persistence interfaces.

### Requirements

- Authenticated API access.
- Input validation.
- Route conflict prevention.
- Plugin-scoped configuration.
- Structured logging.
- Proper cleanup on disable.
- Consistent error handling.
- Versioned extension interfaces.

Plugins must not be allowed to bypass Flixer's authentication system simply by registering their own endpoints.

Expose supported functionality through a stable SDK rather than requiring direct access to internal services.

---

# 10. Indexer Plugin Support

Extend `flixer-indexer` to support provider plugins.

Plugins should be able to register additional provider implementations.

Depending on the provider, extensions may support:

- Search providers.
- Metadata providers.
- External indexers.
- Content discovery.
- Provider-specific configuration.
- Authentication credentials.
- Provider health checks.
- Search result normalisation.

Develop a consistent provider interface.

New providers must integrate with the existing indexing pipeline rather than introducing a separate, incompatible workflow.

Maintain existing provider behaviour and compatibility.

Failures in one provider must not prevent other providers from operating.

---

# 11. Streaming Plugin Support

Extend `flixer-streamer` to support streaming-related extensions.

Possible capabilities include:

- Media segment providers.
- Intro and credits detection integrations.
- Subtitle-related functionality.
- Playback metadata.
- Media processing extensions.
- Streaming session hooks.
- Additional compatible streaming integrations.

Preserve the streamer's existing high-performance architecture.

### Critical Requirements

- Keep Direct Play and existing streaming functionality working.
- Do not introduce unnecessary overhead to streaming hot paths.
- Preserve seeking and HTTP range support.
- Maintain existing authentication.
- Preserve transcoding behaviour.
- Properly handle plugin timeouts and failures.
- Avoid coupling plugin availability to basic media playback.

A broken plugin should not prevent users from streaming unrelated content.

---

# 12. Move Existing Features Into Official Plugins

This is a critical requirement.

I want the following existing Flixer features migrated out of the core codebase and into independent official plugins.

These plugins should be real, fully functional implementations that also serve as examples for future third-party developers.

Do not simply wrap the existing functionality while keeping its implementation tightly coupled to Flixer's core.

## 12.1 SkipMe.db Plugin

Plugin ID:

`flixer.skipmedb`

Move the existing SkipMe.db functionality into its own official plugin.

Identify its current implementation and preserve existing behaviour, including:

- Integration with the SkipMe.db service.
- Media matching.
- Segment retrieval.
- Existing caching behaviour.
- Intro, credits, and other supported segment handling.
- Existing user or administrator configuration.
- Existing streaming/player integration.

Determine the exact supported capabilities by analysing the implementation.

The plugin must work independently through the new plugin APIs.

## 12.2 Media Requests Plugin

Plugin ID:

`flixer.requests`

Move the **entire existing media request system** into an official plugin.

This includes all associated functionality found in the source code, potentially including:

- Frontend pages and UI components.
- Backend API endpoints.
- Request creation.
- Request status tracking.
- Administrator approval and rejection.
- User permissions.
- Database models.
- Notifications and events.
- Indexer/search integrations.
- Existing settings.

Do not remove or simplify features during migration.

The request system should no longer require dedicated feature-specific code in Flixer's core, beyond reusable plugin extension points.

The plugin should be independently enableable and disableable.

When disabled, its routes, navigation, endpoints, scheduled jobs, and other registered functionality should be properly deactivated without deleting user data.

## 12.3 Theme Songs Plugin

Plugin ID:

`flixer.theme-songs`

Move all current theme-song functionality into an official plugin.

Preserve existing features, including any currently implemented:

- Theme song discovery.
- Theme song retrieval.
- Playback integration.
- Caching.
- User configuration.
- Frontend controls.
- Backend integrations.

Inspect the codebase to identify its complete current feature set.

When disabled, the plugin should no longer expose its related UI or services.

## 12.4 Preserve Existing Data

Migration must be non-destructive.

Do not lose:

- Media requests.
- Request history.
- User settings.
- Existing cached data.
- Plugin-related configuration.
- Media library information.
- User permissions.
- Other existing application data.

Implement database migrations where necessary.

Migrations must be versioned, repeatable where appropriate, and safe to rerun.

Existing Flixer installations must upgrade without requiring a fresh installation or manual data recreation.

## 12.5 Compatibility

After migration, an existing Flixer user should still be able to use all three features normally.

The main difference should be that administrators can now manage them as plugins.

Remove obsolete implementation paths and redundant code only after confirming the replacement works.

---

# 13. Dependencies and Compatibility

Plugins may depend on:

- Other plugins.
- Specific versions of Flixer.
- Specific plugin API versions.
- Particular service capabilities.

Implement dependency resolution.

For example:

- Installing a plugin with missing dependencies should identify the required dependencies.
- Compatible dependencies should be installable together with administrator approval.
- Circular dependencies must be detected.
- Disabling a required dependency should warn about affected plugins.
- Incompatible plugin versions must not be activated.
- Updates should validate downstream compatibility.

Avoid silently breaking installed plugins.

Maintain clear compatibility rules for future Flixer releases.

---

# 14. Updates and Rollback

Implement plugin update management.

Requirements:

- Check registered repositories for updates.
- Compare installed and available versions.
- Display available updates in the dashboard.
- Allow manual updates.
- Support a configurable automatic-update policy.
- Validate compatibility before updating.
- Preserve plugin data and settings.
- Stage updates safely.
- Restore the previous working version when activation fails.
- Record update history and errors.

Automatic updates should be disabled by default for third-party executable plugins unless an administrator explicitly opts in.

Avoid unnecessarily redownloading unchanged plugins.

An interrupted update must not corrupt an existing working installation.

---

# 15. Plugin Logging, Diagnostics and Health

Integrate plugins with Flixer's existing logging infrastructure.

Every log event should identify its originating plugin.

Support:

- Structured plugin logs.
- Startup errors.
- Installation errors.
- Runtime errors.
- Dependency errors.
- Update failures.
- Plugin health information.
- Service communication failures.

Provide a way for administrators to inspect relevant plugin errors from the dashboard.

Logs should avoid exposing passwords, tokens, or other secrets.

A plugin failure must not silently crash the entire application.

---

# 16. Plugin Developer SDK and Documentation

Make the plugin ecosystem easy for external developers to use.

Provide documentation covering:

- Plugin architecture.
- Plugin manifests.
- Repository manifests.
- Available capabilities.
- Permission definitions.
- Lifecycle hooks.
- Backend extension APIs.
- Frontend extension APIs.
- Streamer extension APIs.
- Indexer provider APIs