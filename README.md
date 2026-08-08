# File Service

This repository starts with the first incremental feature: a production-minded Go HTTP scaffold with a public health endpoint.

## Current scope

- Go 1.24 standard-library HTTP server
- Environment-based server configuration
- Graceful shutdown on SIGINT/SIGTERM
- Request IDs for correlation
- JSON request logging
- Panic recovery
- Conservative security headers
- Clean boundaries for handlers, middleware, repositories, services, storage, sessions, validation, and Ent/database integration
- SQLite-backed authentication with bcrypt passwords, JWT access tokens, rotating refresh sessions, bearer/cookie transport, CSRF checks for cookie mutations, and login rate limiting
- Authenticated folders, trash, local file storage, streaming upload/download, broad file-type support with MIME detection, 5 GB per-file size checks, xxHash checksums (duplicate detection and corruption checks), search, dashboard, and a minimal browser workspace

Future adapters and collaboration features remain extension points: Ent/PostgreSQL, S3, WebDAV, sharing/versioning, organizations/RBAC, 2FA, workers, Redis, Docker, and Kubernetes.

## Run

```sh
go test ./...
go run ./cmd/api
```

Then request `http://localhost:8080/health`.

## Configuration

| Variable | Default |
| --- | --- |
| `APP_ENV` | `development` |
| `HTTP_ADDRESS` | `:8080` |
| `HTTP_READ_HEADER_TIMEOUT` | `5s` |
| `HTTP_WRITE_TIMEOUT` | `30s` |
| `HTTP_IDLE_TIMEOUT` | `60s` |
| `HTTP_SHUTDOWN_TIMEOUT` | `10s` |
| `HTTP_MAX_HEADER_BYTES` | `1048576` |
| `HTTP_READ_TIMEOUT` | `30m` |
| `UPLOAD_MAX_BYTES` | `5368709120` (5 GiB) |
| `STORAGE_ROOT` | `data/objects` (single default disk when `STORAGE_MOUNTS` is unset) |
| `STORAGE_MOUNTS` | e.g. `Main=/mnt/main;Media=/mnt/media` — named disks listed in the sidebar |
| `CORS_ALLOWED_ORIGINS` | comma-separated origins (e.g. `https://files.example.com`) allowed to call the API from another host |
| `UI_API_BASE` | e.g. `https://api.example.com` — point the built-in UI at a remote API (frontend/backend split) |
| `JWT_SECRET` | dev-only placeholder (must be changed, ≥ 32 bytes) |
| `DATABASE_PATH` | `data/fileservice.db` |
| `N_DRIVE_USERNAME` | `Naman` |
| `N_DRIVE_PASSWORD` | `7564` (dev-only; rejected in production) |

### Account seeding

The single user account is read from `N_DRIVE_USERNAME` / `N_DRIVE_PASSWORD` and
seeded **only when the `users` table is empty**. Existing accounts are never
deleted or overwritten at boot. The built-in defaults are for local development
only: in `production`, a non-default password of at least 8 characters is
required, just like `JWT_SECRET`.

To change the account of an existing database, set the variables, stop the
server, delete (or back up) the database file, and start again — the new
credentials are then seeded fresh.

### Multiple disks

Set `STORAGE_MOUNTS` to a semicolon-separated list of `Name=/absolute/path`
entries to attach more than one disk (local drives, attached disks, NFS/SMB
mounts). Each disk appears in the sidebar with its own live usage meter and
its own file tree; clicking a disk switches the workspace to it. Names must
be short identifiers (`A-Za-z0-9._-`). The first listed disk is the default
disk and keeps the internal id `default`, so files stored before multi-disk
support stay visible on it. When `STORAGE_MOUNTS` is unset, a single default
disk named `Main` is derived from `STORAGE_ROOT`, so existing setups keep
working unchanged.

Files always live on the disk they were uploaded to; a file uploaded into a
folder is stored on that folder's disk. Files cannot be moved or copied
between disks. Existing databases are migrated in place: files and folders
gain a `mount` column defaulting to `default`, which is the id of the
single-disk mount.

### Frontend/backend split

N-Drive normally serves both the UI and the API from one process, which is
all that most deployments need. To host the UI somewhere else and call a
remote API instead:

1. On the backend server, allow the UI's origin: set `CORS_ALLOWED_ORIGINS`
   to the exact origin where the UI is hosted (e.g. `https://files.example.com`).
   Only explicitly listed origins are allowed; credentials are always sent
   with an exact reflected origin, never a wildcard.
2. Make the UI target the backend. Either:
   - serve the built-in page from the backend pointed elsewhere:
     set `UI_API_BASE=https://api.example.com` (the page then injects the
     target and widens its CSP), or
   - host the workspace HTML anywhere and set `window.NDRIVE_API_BASE` in a
     script tag before the app script: `window.NDRIVE_API_BASE="https://api.example.com";`.

When the UI targets a different origin it switches to bearer-token auth: the
login response already carries `access_token` and `refresh_token`, which the
UI stores in `localStorage` (keyed by API origin) and sends as
`Authorization: Bearer` headers, rotating via `/api/auth/refresh`. No cookies
or CSRF tokens cross origins, so this works between any two origins. Token
storage in `localStorage` is XSS-visible, as with any bearer-based web app.

Same-origin deployments are completely unchanged: no `CORS_ALLOWED_ORIGINS`,
no `UI_API_BASE`, cookie auth with CSRF as before.

Uploads accept all detected file types by default. Set `AllowedMIMEs` when constructing a store if a deployment needs a narrower allowlist. Downloads remain forced to attachment disposition.

Checksums are xxHash64 (non-cryptographic) for speed on CPUs without SHA
hardware acceleration; they power duplicate detection and corruption checks.
Objects stored before this change keep their old SHA-256 checksums, so
re-uploading an already-stored file only de-duplicates against new checksums.

## Planned increments

1. ~~Authentication~~: password hashing, JWT access tokens, rotating refresh tokens, sessions, rate limiting, cookie/bearer transport, and CSRF behavior.
2. ~~Persistence~~: SQLite development database, migrations, repositories, and indexes. Ent/PostgreSQL remain future adapters.
3. ~~Folders and trash~~: authenticated CRUD, pagination, soft deletion, restore, and audit events.
4. ~~Storage~~: secure paths, upload/download streaming, size/MIME/filename validation, xxHash checksums, and local file operations.
5. ~~Search/dashboard and frontend web UI~~: initial API endpoints and embedded dashboard shell are included.
6. PostgreSQL/S3 adapters, sharing/versioning/RBAC/2FA/background jobs, and deployment packaging.
