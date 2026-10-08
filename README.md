# Universal Media Service — Backend (Go)

Upload, process, store, and share images, video, and audio through a Gin API with Clerk auth, Cloudflare R2 storage, Neon Postgres metadata, Redis caching/events, and a background worker.

| | |
|---|---|
| **Production** | https://media-server-qbo2eammia-uc.a.run.app (Cloud Run, `us-central1`, service `media-server`) |
| **UI** | https://ums-media-forge-ui.vercel.app |
| **Stack** | Go 1.25 · Gin · pgx/Neon · R2 (S3) · Redis (Upstash) · Clerk · ffmpeg |
| **Last updated** | 2026-10-08 — see [`PROGRESS_REPORT.md`](PROGRESS_REPORT.md) for the full breakdown |
| **Size** | ~5.8k LOC Go, 35 commits, CI green on HEAD `e73cd74` |

## Status at a glance

| Area | Complete | Notes |
|---|---|---|
| Architecture & routing | 100% | Layered `api` / `adapters` / `core` / `internal` |
| Upload & CRUD | 100% | Multipart, streaming, replace-in-place, trash, batch |
| Image processing | 95% | Resize/crop/blur/grayscale/quality/thumbnail, URL-driven |
| Video & audio | 80% | Metadata + thumbnail + copy on ingest; transcode not exposed |
| Real-time status (SSE) | 100% | Redis pub/sub + EventSource, poll fallback |
| Auth & security | 90% | Clerk JWT, HMAC shares, rate limits; secret hygiene outstanding |
| Storage (R2) | 90% | No lifecycle policies yet |
| Database (Postgres) | 95% | No versioning/audit trail |
| Observability | 45% | Structured logs only — no metrics, tracing, or alerting |
| Testing & QA | 70% | Unit tests + `-race`, 32-check QA script, manual E2E |
| Deployment & CI | 100% | Docker → Artifact Registry → Cloud Run; CI on every push |

**Overall ≈ 90%.** Remaining work is prioritized in [`PROGRESS_REPORT.md`](PROGRESS_REPORT.md).

## Architecture

```
cmd/server          entrypoint, wiring, graceful shutdown
api/                route table (api/routes.go)
adapters/http       Gin handlers: upload, list/process, share, SSE, health
adapters/r2         Cloudflare R2 (S3 API)
adapters/cache      Redis processed-image cache (content-versioned keys)
adapters/events     Redis pub/sub event bus + short-lived SSE tokens
adapters/lease      Postgres advisory lease for the worker
core/auth           Clerk JWT middleware, token-bucket rate limiters
core/upload         upload service: validation, sync vs deferred processing
core/image          image processor, URL param parser, crop/resize/blur
core/video          ffmpeg/ffprobe: metadata, thumbnail, copy/transcode
core/audio          ffmpeg/ffprobe: duration, format conversion
core/worker         background worker: lease, retries, panic recovery, SSE publish
internal/config     env loading with fail-fast secret validation
```

## Feature status

### Authentication & security
- [x] Clerk JWT middleware (issuer pinned via `CLERK_ISSUER`)
- [x] User-scoped authorization; trashed/foreign items never leak
- [x] HMAC-signed share links (7-day expiry)
- [x] Rate limiting: 100 req/min per verified user + 300 req/min per-IP flood guard
- [x] CORS allow-list with preview-deployment support
- [x] Fail-fast validation of required secrets at startup
- [ ] Move `DATABASE_URL` / `SHARE_SECRET` into Secret Manager (currently plaintext env)

### Upload & media handling
- [x] Presigned direct-to-R2 uploads: `POST /media/uploads` → client PUTs straight to storage → `POST /media/:id/complete` verifies (size + family sniff) and enqueues processing — bypasses the ~32 MB request cap of Cloud Run's front end, up to 500 MB (`MAX_UPLOAD_BYTES`)
- [x] Multipart upload with `MaxBytesReader` guard for the legacy/replace path; practical cap ~30 MB in prod (Google's front end returns an HTML 413 above 32 MB before the app sees the request)
- [x] Per-user storage quota (`USER_STORAGE_QUOTA_BYTES`, default 10 GiB) enforced at presign time against all rows including pending/trashed
- [x] Family-level content sniffing at complete (`core/media/sniff.go`, ffmpeg-compatible container detection) — declared vs stored type mismatches are rejected and the object deleted
- [x] Abandoned `pending` uploads reaped by a worker sweeper (5 min tick, 2 h age)
- [x] Streaming worker path: download to temp file → process → stream outputs (never buffers whole files; RSS stays flat)
- [x] Streaming path for large bodies; MIME allow-list per type
- [x] EXIF auto-orientation, metadata extraction
- [x] Replace media in place (single row, fresh cache version)
- [x] Soft delete (trash) → restore → permanent delete; batch delete
- [x] Deferred processing via worker — threshold `MaxSyncProcessingSize` is 16 MiB (was 50 MB, unreachable behind the GFE cap)

### Image processing
- [x] Lanczos resize, crop with 9 gravities, quality, blur, grayscale
- [x] JPEG & PNG output, thumbnails
- [x] Dynamic URL-param processing (`/process?w=&h=&q=&format=&blur=&grayscale=&cw=&ch=&gravity=`)
- [x] Redis processed cache keyed by content version; `X-Cache: HIT/MISS`; immutable cache headers
- [x] `WebP` output intentionally removed (claims aligned with behavior)
- [ ] Watermark, auto-enhance

### Video & audio
- [x] Type allow-list (mp4/mov/avi/webm/mkv; mp3/wav/ogg/flac/aac/m4a)
- [x] ffprobe metadata (duration, dimensions, codec)
- [x] Video thumbnail + lossless stream copy on ingest
- [x] Audio format conversion on ingest
- [ ] Transcode options (scale/quality/codec) exposed through the API — `transcode()` exists but is unused
- [ ] Dynamic processing for A/V (`/process` correctly returns 400 for non-images)

### Real-time status (SSE)
- [x] `POST /api/v1/events/authorize` — Clerk session → short-lived stream token
- [x] `GET /api/v1/events/stream` — `event: status` frames, 25 s keepalive
- [x] Redis pub/sub bus (`events:<userID>`); registered only when Redis is configured
- [x] Client falls back to polling when the bus is absent

### Observability & reliability
- [x] Structured logging, graceful shutdown, request timeouts
- [x] Health endpoints: `/health` (liveness, externally reachable), `/healthz`, `/readyz` (DB ping)
- [x] Worker lease claiming, retry backoff, panic recovery
- [ ] Metrics (Prometheus / Cloud Monitoring), tracing, uptime alerting
- [ ] Automated integration tests in CI

## API endpoints

```
POST   /api/v1/media                    Upload media (multipart)
POST   /api/v1/media/uploads             Begin presigned direct upload → { id, uploadUrl, expiresIn }
POST   /api/v1/media/:id/complete        Verify presigned upload → enqueues worker
PUT    /api/v1/media/:id                Replace media in place
GET    /api/v1/media                    List (paginated, searchable, sortable)
DELETE /api/v1/media/:id                Soft delete (trash)
DELETE /api/v1/media/:id/permanent      Permanent delete (row + derived assets)
POST   /api/v1/media/batch-delete       Batch delete
PATCH  /api/v1/media/:id/rename         Rename
PATCH  /api/v1/media/:id/restore        Restore from trash
GET    /api/v1/media/:id/process        Dynamic image processing (see params above)
GET    /api/v1/media/:id/status         Lightweight status poll
GET    /api/v1/media/:id/info           Full metadata
POST   /api/v1/media/:id/share          Generate signed share URL
POST   /api/v1/media/:id/reprocess      Reset to "uploaded" for the worker to retry
GET    /api/v1/share/:token             Public share redirect (anonymous, 302)
POST   /api/v1/events/authorize         Exchange Clerk session for SSE token
GET    /api/v1/events/stream?token=…    SSE status stream (unauthenticated; token in query)
GET    /health                          Liveness (use this from outside Cloud Run)
GET    /healthz                         Liveness (conventional alias)
GET    /readyz                          Readiness (DB ping)
```

> `/healthz` is intercepted by Google's front end when called from outside Cloud Run (HTML 404); gin's `301` on `/healthz/` proves the route exists. Probe `/health`.

## Deployment

```bash
# Build & push (Artifact Registry repo must be in the same region as the service)
gcloud auth configure-docker us-central1-docker.pkg.dev
docker build -t us-central1-docker.pkg.dev/ums-media-forge/ums-backend/server:latest .
docker push us-central1-docker.pkg.dev/ums-media-forge/ums-backend/server:latest

# Deploy — env vars, scaling, and secrets are preserved unless overridden
gcloud run deploy media-server \
  --image us-central1-docker.pkg.dev/ums-media-forge/ums-backend/server:latest \
  --region us-central1 --project ums-media-forge --allow-unauthenticated
```

`--allow-unauthenticated` is required: IAM must not block CORS preflights (they carry no auth headers). App-level auth is enforced by the Clerk middleware.

## Tests & QA

```bash
go vet ./... && go build ./... && go test -race ./...   # matches CI (.github/workflows/ci.yml)

# 32-check smoke suite against a running instance
API_BASE=https://media-server-qbo2eammia-uc.a.run.app \
CLERK_TEST_TOKEN=<clerk session jwt> bash scripts/phase0-qa.sh
```

Latest run (2026-10-08, local): **31 passed, 0 failed, 1 skipped** (optional 2-user IDOR case). Includes the presigned-flow suite: begin rejections (type/size/zero), pending rows hidden, complete-before-PUT 409, presigned PUT, complete → `uploaded`, content-mismatch purge, size-mismatch rejection.

## Quick start

```bash
cp .env.example .env    # fill in Clerk, R2, Postgres, Redis, SHARE_SECRET
go mod tidy
go run cmd/server/main.go
```

## Remaining work

Prioritized, with effort estimates: [`PROGRESS_REPORT.md`](PROGRESS_REPORT.md).
Known gotchas and trade-offs: [`LESSONS.md`](LESSONS.md).
Design intent and deferred items: [`SystemDesignDoc.md`](SystemDesignDoc.md).
