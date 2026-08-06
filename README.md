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
- Authenticated folders, trash, local file storage, streaming upload/download, broad file-type support with MIME detection, 5 GB per-file size checks, checksums, search, dashboard, and a minimal browser workspace

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
| `STORAGE_ROOT` | `data/objects` |
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

Uploads accept all detected file types by default. Set `AllowedMIMEs` when constructing a store if a deployment needs a narrower allowlist. Downloads remain forced to attachment disposition.

## Planned increments

1. ~~Authentication~~: password hashing, JWT access tokens, rotating refresh tokens, sessions, rate limiting, cookie/bearer transport, and CSRF behavior.
2. ~~Persistence~~: SQLite development database, migrations, repositories, and indexes. Ent/PostgreSQL remain future adapters.
3. ~~Folders and trash~~: authenticated CRUD, pagination, soft deletion, restore, and audit events.
4. ~~Storage~~: secure paths, upload/download streaming, size/MIME/filename validation, SHA-256 checksums, and local file operations.
5. ~~Search/dashboard and frontend web UI~~: initial API endpoints and embedded dashboard shell are included.
6. PostgreSQL/S3 adapters, sharing/versioning/RBAC/2FA/background jobs, and deployment packaging.
