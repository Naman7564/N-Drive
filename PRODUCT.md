# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

A single person running N-Drive on their own machine or server. Their job: keep personal files private, organized, and reachable — store them, find them, and retrieve them without handing them to a cloud provider.

## Product Purpose

A self-hosted personal file service: upload, organize, search, trash, and download your own files from a browser workspace served by a server you control. Success means the user's files live only where they chose, and every routine file task happens quickly and without friction.

## Positioning

Private by design. Unlike cloud drives that hold your data in someone else's data center, N-Drive stores everything on hardware the user controls — no cloud middleman, no account to surrender, no quiet inspection of private files.

## Operating Context

- Runs as a local Go server (`go run ./cmd/api`, default `http://localhost:8080`), configured via environment variables (`APP_ENV`, `HTTP_ADDRESS`, HTTP timeouts, etc.).
- The entire workspace is a single embedded HTML page served with a CSP nonce: sign in, dashboard overview, file browsing with folder navigation and breadcrumbs, trash, search, and upload/download.
- Auth is email + password (bcrypt), JWT access tokens, rotating refresh sessions, bearer/cookie transport, CSRF checks on cookie mutations, and login rate limiting.
- Files live in local storage with SHA-256 checksums, MIME and size validation, and a 100 MB per-file upload cap.
- Works in both dark and light themes; a mobile menu and responsive layout are part of the workspace.

## Capabilities and Constraints

Confirmed:

- Authentication and account creation; logout and session refresh.
- Folders: create, rename, delete, navigate (breadcrumb trail).
- Files: upload (multiple, drag-and-drop), download, rename, copy, move, soft-delete to trash, restore, purge.
- Search across files; dashboard with storage and item stats.
- SQLite persistence; Go 1.24 standard-library HTTP server; graceful shutdown; JSON request logging; panic recovery; security headers.
- Extension points already named in the repository: Ent/PostgreSQL, S3, WebDAV, sharing, versioning, organizations/RBAC, 2FA, workers, Redis, Docker, Kubernetes.

Undecided:

- Deployment beyond the local machine (VPS, Docker, Raspberry Pi) and multi-user direction — not committed.

## Brand Commitments

- Product name is **N-Drive**; "File Service" in the UI is a placeholder to be replaced.
- Current UI voice — "A calm view of your workspace", "Private by design · Secure local workspace" — is evidence of tone, not a confirmed voice contract.

## Evidence on Hand

- README.md documents current scope, configuration, and planned increments.
- The embedded UI (internal/handler/httpapi/web.go) is the working interface.
- Absent and must not be fabricated: testimonials, customers, benchmarks, pricing, licensing, or deployment claims.

## Product Principles

1. Privacy is the product: data stays on hardware the user controls; no cloud middleman.
2. One person, zero ceremony: single-user, self-hosted flows stay simple; no enterprise friction.
3. Reliability over surface area: the proven core — files, folders, trash, search — is finished well before new features.
4. Real capability only: never fabricate claims, evidence, or benchmarks.
5. Small, secure defaults: conservative configuration, safe transport, and validated inputs are the baseline.
