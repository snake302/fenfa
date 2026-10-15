# Client catalog and password login

The homepage `/` lists all published products. Each card opens `/products/<slug>`; the All products link on the product page returns to the catalog. Unpublished products and platforms are excluded from the catalog.

## Enabling password login

Set a dedicated environment variable when starting the server or container:

```dotenv
FENFA_CLIENT_PASSWORD=your-long-random-password
```

The password is shared by visitors to the client site. It is independent of `FENFA_ADMIN_TOKEN` and `FENFA_UPLOAD_TOKEN` and does not grant administrator privileges. The server reads the variable at startup: changing the password requires restarting the server with the updated environment, but does not require rebuilding the frontend.

If the variable is absent, you can set `auth.client_password` in `config.json`. Access remains public if no password is configured or if an explicitly empty variable, `FENFA_CLIENT_PASSWORD=`, is supplied. The server does not read `.env` files directly: the launcher must supply the environment, for example through Docker Compose's `env_file` setting.

## Client behavior

Visitors who open a product page without signing in are redirected to `/`, which displays the password form. After a successful login, the catalog appears and visitors can open any published product. The catalog includes a Sign out button.

Sessions last seven days and use the `fenfa_client_session` cookie with `HttpOnly`, `SameSite=Lax`, and `Secure` over HTTPS. The database stores only a hash of the random token, tied to the current password. Sessions survive restarts as long as the database and password are preserved. Signing out revokes the current session. Changing the password invalidates all previously issued sessions and download grants.

Use HTTPS in production. If TLS terminates at a reverse proxy, forward `X-Forwarded-Proto: https`, as shown in the repository's nginx example.

## Protected routes

Protection covers `/products/*` and legacy `/apps/*` pages, client catalog and product APIs, icons, events, UDID profile and status requests, `/d/*` downloads, and iOS manifests at `/ios/*/manifest.plist`. Page requests without a session redirect to `/`; client APIs return HTTP 401. If a session expires while a product page is open, the client also redirects the visitor to `/`.

The homepage, static UI assets, and `/healthz` remain accessible without signing in. The admin interface retains its independent authentication.

When protection is enabled, iOS callbacks are accepted only with a nonce previously issued by an authenticated profile request. The system callback does not require the browser's cookie.

## Downloads and iOS installation

After login, the API issues a temporary grant for each build through the `access_token` parameter. It lasts one hour and permits access only to the corresponding release's manifest and file. This allows the iOS system installer to download the app without Safari cookies. The manifest carries the grant into the IPA URL. A different release, a modified token, or an expired token does not grant access.

A temporary link remains usable until its one-hour expiry, including after signing out of the client session. It does not grant access to the catalog or other products. Do not put the shared client password in a URL.

Protection applies to requests served by Fenfa. With S3/R2 storage, downloads redirect to the configured public object URL; access to a known storage URL is controlled by the storage configuration rather than the Fenfa client session.

## Login API

| Method and path | Purpose |
|---|---|
| `GET /api/client/session` | Returns `enabled` and `authenticated` |
| `POST /api/client/login` | Accepts JSON `{"password":"..."}` and sets the session cookie |
| `POST /api/client/logout` | Revokes the current session and clears the cookie |

An incorrect password returns HTTP 401. Login requires `Content-Type: application/json`; the request body is limited to 4 KiB. Login attempts are limited to 10 per minute per IP and 100 per minute across each server instance; exceeding either limit returns HTTP 429 with `Retry-After`. The password and session token are never returned in JSON or stored in `localStorage`.
