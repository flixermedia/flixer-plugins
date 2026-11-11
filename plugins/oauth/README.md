# OAuth 2.0 provider

Configure multiple external sign-in providers for existing Flixer accounts. This plugin supports OAuth 2.0 authorization-code flows with PKCE S256 and JSON user-info responses. Provider-specific protocols and OpenID Connect ID-token validation are not supported.

## Configure

1. Install and enable **OAuth 2.0** in **Admin → Plugins**.
2. Open its **Settings**, select **Add provider**, and enter the provider's authorization, token and user-info URLs, client ID, scopes, issuer and stable user ID field.
3. Select the token authentication method and enter a client secret if required. Enable the provider and save it.
4. Configure the OAuth application with the callback URL shown in the plugin settings.
5. In Flixer profile settings, link the external account before using its sign-in button.

Client-secret POST, client-secret Basic and public PKCE clients are supported. The user ID field must be a stable top-level field in the user-info JSON response; do not use an email address unless the provider guarantees it never changes. Changing identity-related settings or removing a provider requires unlinking its accounts first.

For GitHub, use `https://github.com/login/oauth/authorize` as the authorization URL, `https://github.com/login/oauth/access_token` as the token URL, `https://api.github.com/user` as the user-info URL, `https://github.com` as the issuer, `id` as the user ID field and `read:user` as the scope. Existing GitHub OAuth plugin links remain separate and must be linked again to use this plugin.

## Catalogue artwork

The repository entry uses `plugins/oauth/icon.png` and `plugins/oauth/banner.png` for its catalogue artwork. Paths in `repository.json` are relative to that file and must point to PNG, JPEG, WebP or GIF images no larger than 5 MiB.

## Build

Run `sh build.sh` from this directory. The script creates an immutable release in `plugins/oauth/0.1.1` and refuses to overwrite an existing package. For a new release, update the version in `plugin.json` and `build.sh`, build it, then add its digest and size to `repository.json`.
