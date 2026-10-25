<p align="center">
  <strong>Fenfa</strong> &nbsp;·&nbsp; Self-hosted app distribution platform
</p>

<p align="center">
  <a href="https://github.com/openprx/fenfa/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License"></a>
  <a href="https://github.com/openprx"><img src="https://img.shields.io/badge/org-OpenPRX-8b5cf6" alt="OpenPRX"></a>
</p>

<p align="center">
  <a href="README.zh-CN.md">中文文档</a>
</p>

---

Fenfa (分发, "distribute" in Chinese) is a self-hosted distribution platform for iOS, Android, macOS, Windows, and Linux apps. Upload builds, get install pages with QR codes, and manage releases through a clean admin panel.

Part of the [OpenPRX](https://github.com/openprx) ecosystem — open-source infrastructure for development teams.

## Features

- **Multi-platform** — iOS (IPA), Android (APK), macOS, Windows, Linux
- **Smart upload** — auto-detect app metadata (bundle ID, version, icon) from packages
- **iOS UDID binding** — device registration flow for ad-hoc distribution
- **Product pages** — public download pages with QR codes and platform detection
- **Admin panel** — manage products, variants, releases, devices, and settings
- **S3/R2 storage** — optional S3-compatible object storage (Cloudflare R2, AWS S3, MinIO)
- **Build retention** — keep the latest N builds per target, with automatic cleanup after uploads (default: 50)
- **Apple Developer API** — auto-register devices to your Apple Developer account
- **Single binary** — Go binary with embedded frontend, just run it
- **SQLite** — zero external dependencies, data in a single file
- **i18n** — Chinese and English UI

## Quick Start

### Docker (recommended)

```bash
docker run -d --name fenfa -p 8000:8000 fenfa/fenfa:latest
```

Visit `http://localhost:8000/admin`, login with token `dev-admin-token`.

**Production** — use custom tokens and persistent storage:

```bash
docker run -d \
  --name fenfa \
  --restart=unless-stopped \
  -p 127.0.0.1:8000:8000 \
  -e FENFA_ADMIN_TOKEN=your-secret-token \
  -e FENFA_UPLOAD_TOKEN=your-upload-token \
  -e FENFA_PRIMARY_DOMAIN=https://your-domain.com \
  -v ./data:/data \
  -v ./uploads:/app/uploads \
  fenfa/fenfa:latest
```

Or mount a [`config.json`](docs/config.example.json) for full control:

```bash
docker run -d --name fenfa -p 8000:8000 \
  -v ./data:/data -v ./uploads:/app/uploads \
  -v ./config.json:/app/config.json:ro \
  fenfa/fenfa:latest
```

### Build from source

Requirements: Go 1.25+, Node.js 20+

```bash
make build   # builds frontend + backend
make run     # starts the server
```

Or manually:

```bash
cd web/front && npm ci && npm run build && cd ../..
cd web/admin && npm ci && npm run build && cd ../..
go build -o fenfa ./cmd/server
./fenfa
```

## Configuration

See [`docs/config.example.json`](docs/config.example.json) for all options. Full API spec in [`docs/api-spec.md`](docs/api-spec.md).

| Key | Description | Default |
|-----|-------------|---------|
| `server.port` | HTTP port | `8000` |
| `server.primary_domain` | Public URL for manifests and callbacks | `http://localhost:8000` |
| `server.organization` | Organization name in iOS profiles | `Fenfa Distribution` |
| `server.data_dir` | Database directory | `data` |
| `auth.admin_tokens` | Admin API tokens | `["dev-admin-token"]` |
| `auth.upload_tokens` | Upload API tokens | `["dev-upload-token"]` |

### Environment Variables

Override config values without a file:

| Variable | Description |
|----------|-------------|
| `FENFA_PORT` | HTTP port |
| `FENFA_DATA_DIR` | Database directory |
| `FENFA_PRIMARY_DOMAIN` | Public domain URL |
| `FENFA_ADMIN_TOKEN` | Admin token |
| `FENFA_UPLOAD_TOKEN` | Upload token |

### Storage

Files are stored locally by default. S3-compatible storage (R2, AWS S3, MinIO) can be configured in admin panel > Settings > Storage.

The same settings section includes **Builds to keep per target**, a positive integer that defaults to **50**, including on existing installations. The limit applies separately to each target (variant), across all versions and channels. After a successful `/upload` or `/smart-upload`, Fenfa keeps the new build and the latest N−1 previous uploads by upload time, using release ID to break timestamp ties. Build numbers do not affect retention.

Older builds are permanently deleted together with their files, local `meta.json`, provisioning profiles, release events, and download grants. S3/R2 cleanup deletes the object and any local copy left by older uploads; new S3 uploads discard their local staging files after metadata extraction. The storage credentials must allow object deletion (`s3:DeleteObject`). With bucket versioning enabled, deleting an object may retain older object versions according to the bucket policy.

Saving a different limit does not immediately delete existing builds. Each target is cleaned up on its next successful upload. Failed uploads do not trigger retention. Cleanup failures are logged without failing the new upload; affected build records remain for retry on the next upload, so storage errors can temporarily leave more than N builds. Files and objects already orphaned by earlier versions are not discovered by this policy.

## Architecture

```
Request → Gin Router → Auth Middleware → Handler → GORM → SQLite
```

```
cmd/server/          Entry point
internal/server/     HTTP handlers & routing
internal/store/      Database models (GORM + SQLite)
internal/web/        Embedded frontend (go:embed)
web/front/           Public download page (Vue 3 + Vite)
web/admin/           Admin panel (Vue 3 + Vite)
```

## API

Auth via `X-Auth-Token` header. See [`docs/api-spec.md`](docs/api-spec.md) for details.

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| GET | `/products/:slug` | — | Product download page |
| GET | `/d/:releaseID` | — | Direct file download |
| POST | `/upload` | upload | Upload a build |
| * | `/admin/api/*` | admin | Admin API |

## Links

- [Documentation](https://docs.openprx.dev/en/fenfa/) — Full documentation (10 languages)
- [Community](https://community.openprx.dev) — OpenPRX community forum

## Contributing

Fenfa is part of [OpenPRX](https://github.com/openprx). Issues and pull requests welcome.

## License

[MIT](LICENSE)
