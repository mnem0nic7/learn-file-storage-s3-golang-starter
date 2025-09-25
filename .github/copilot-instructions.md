# Tubely AI implementation guide

## Architecture snapshot
- `main.go` builds an `apiConfig` with env-driven paths, JWT secrets, and database client, then registers HTTP routes using the Go 1.22 pattern syntax (`"POST /api/login"`).
- Each `handler_*.go` file is a thin HTTP handler that operates on `*apiConfig`; reuse `respondWithJSON` and `respondWithError` from `json.go` for all responses.
- `internal/database` owns all SQL against the SQLite DB. `autoMigrate` provisions `users`, `refresh_tokens`, and `videos` tables on startup; prefer calling its methods instead of inlining SQL in handlers.
- `internal/auth` centralizes Argon2 password hashing, JWT creation/validation, and bearer-token parsing; JWTs use issuer `tubely-access` and embed the user ID as subject.
- Static SPA assets in `app/` are served from `/app/` (via `FILEPATH_ROOT`), while user-uploaded files live under `ASSETS_ROOT` and are exposed at `/assets/` behind `cacheMiddleware`.

## Data & storage
- `DB_PATH` points to a local SQLite file (default `tubely.db`). CRUD helpers in `internal/database` return `(value, nil)` when found and `(zero, nil)` when missing—check for empty structs explicitly.
- Video metadata persists in the `videos` table; binary uploads are planned to land in S3 and mirrored under `assets/` during local dev.
- Thumbnails are temporarily cached in the in-memory `videoThumbnails` map (`map[uuid.UUID]thumbnail`) so any upload flow must update both the map and the DB URLs.
- `handler_reset.go` wipes all tables via `Client.Reset()` but only when `PLATFORM=dev`.

## Auth flow expectations
- `/api/users` hashes passwords with Argon2 (`auth.HashPassword`) before persistence; `/api/login` verifies credentials, issues a 30-day access JWT plus a long-lived refresh token stored via `CreateRefreshToken`.
- All protected routes start with `auth.GetBearerToken` and `auth.ValidateJWT`. A missing refresh token yields `(nil, nil)` from `GetUserByRefreshToken`, so guard for that before dereferencing.
- JWT and refresh endpoints must keep issuer/expiry aligned with `internal/auth` helpers; do not hand-roll token parsing.

## Environment & local workflow
- Load `.env` (see `.env.example`) with: `DB_PATH`, `JWT_SECRET`, `PLATFORM`, `FILEPATH_ROOT`, `ASSETS_ROOT`, `S3_BUCKET`, `S3_REGION`, `S3_CF_DISTRO`, `PORT`. Startup `log.Fatal`s if any are absent.
- Install external tools up front: `ffmpeg` + `ffprobe` for transcoding, SQLite CLI for inspection, and AWS CLI for S3/CloudFront tasks.
- Typical dev loop: `go mod download`, run `./samplesdownload.sh` for media fixtures, then `go run .` to launch the API and front-end (creates `tubely.db` and ensures `assets/`).

## Implementation conventions
- Register new endpoints in `main.go` using the `"METHOD /path"` pattern; keep handler definitions in their own `handler_*.go` file for clarity.
- Always emit JSON (even on errors) via the helpers; they log internal errors and enforce `Content-Type: application/json`.
- Update DB records through `database.Client` methods—if you need a new query, add it alongside existing ones rather than mixing raw SQL into handlers.
- When implementing uploads, write the file under `ASSETS_ROOT` (or S3), set the public URL on the `videos` row, and refresh any cached entries so `/api/thumbnails/{videoID}` and `/api/videos/{videoID}` stay consistent.
- Preserve the `cfg.platform` guard for destructive or admin-only operations and prefer the `cacheMiddleware` for any new static asset mounts.

## Troubleshooting tips
- Startup failures usually mean an env var is unset or the SQLite file path is invalid; verify `.env` and file permissions.
- HTTP 401s tend to be malformed headers—ensure clients send `Authorization: Bearer <token>` (refresh endpoints reuse Bearer tokens).
- If thumbnails or videos fail to render in the SPA, confirm the corresponding URLs in the `videos` table and that the handlers set `Content-Length`/`Content-Type` like `handlerThumbnailGet`.
