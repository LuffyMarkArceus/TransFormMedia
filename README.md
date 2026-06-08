# Backend Status – Universal Media Service (Go)

## Architecture
- [x] Layered architecture (adapters / core / api)
- [x] Clear separation of concerns
- [x] Background worker subsystem (in-process goroutine pool)

## Authentication & Security
- [x] Clerk JWT middleware
- [x] User-scoped authorization
- [x] Public vs private image access (status-based filtering)
- [x] Signed URLs for sharing (HMAC-based, 7-day expiry)
- [x] Rate limiting (in-memory sliding window, 100 req/min)

## Image Upload
- [x] Multipart upload handling
- [x] In-memory buffering
- [x] Image validation & decoding
- [x] EXIF auto-orientation
- [x] Metadata extraction (width, height, size)
- [x] Streaming uploads (ProcessStream)
- [x] Replace media endpoint
- [x] Async processing for large files (>50MB)

## Image Processing
- [x] Centralized image processor
- [x] Resizing with Lanczos
- [x] JPEG, PNG & WebP support
- [x] Quality control
- [x] Thumbnail generation
- [x] Blur effect (Gaussian, sigma 0–20)
- [x] Grayscale conversion
- [x] Crop with gravity (center, top, bottom, left, right, corners)
- [ ] Additional effects (watermark, auto-enhance)

## Dynamic Image Processing API
- [x] URL-based processing parameters
- [x] Width & height via query params
- [x] Crop width/height + gravity via query params
- [x] Format selection via query params (jpeg/png/webp)
- [x] Quality control via query params
- [x] Blur & grayscale via query params
- [x] Processed image caching (Redis)
- [x] CDN cache headers
- [x] Cache hit/miss headers (X-Cache)

## Storage (Cloudflare R2)
- [x] Raw image storage
- [x] Processed image storage
- [x] Thumbnail storage
- [x] Delete raw + derived assets
- [x] Cache processed variants (Redis TTL)
- [ ] Lifecycle policies

## Database (Neon / Postgres)
- [x] Image metadata model
- [x] Create image record
- [x] List images by user with pagination
- [x] Search by name (ILIKE)
- [x] Sort by date/name/size
- [x] Rename image (DB-only)
- [x] Soft delete (trash) & restore
- [x] Permanent delete
- [x] Delete image record
- [x] Status tracking (uploaded → processing → ready/failed)
- [ ] Versioning

## Observability & Reliability
- [x] Structured logging (slog)
- [x] Rate limiting middleware
- [x] Graceful shutdown
- [ ] Metrics (Prometheus)
- [ ] Tracing
- [ ] Integration tests

## API Endpoints
- `POST   /api/v1/media`              — Upload media
- `PUT    /api/v1/media/:id`           — Replace media
- `GET    /api/v1/media`               — List media (paginated, searchable, sortable)
- `DELETE /api/v1/media/:id`           — Soft delete (trash)
- `DELETE /api/v1/media/:id/permanent` — Permanent delete
- `POST   /api/v1/media/batch-delete`  — Batch delete
- `PATCH  /api/v1/media/:id/rename`    — Rename
- `PATCH  /api/v1/media/:id/restore`   — Restore from trash
- `GET    /api/v1/media/:id/process`   — Dynamic image processing (w/h/q/format/blur/grayscale/cw/ch/gravity)
- `GET    /api/v1/media/:id/status`    — Lightweight status check (id + status)
- `GET    /api/v1/media/:id/info`      — Full metadata
- `POST   /api/v1/media/:id/share`     — Generate signed share URL
- `GET    /api/v1/share/:token`        — Public share redirect
- `POST   /api/v1/media/:id/reprocess` — Reset failed item to "uploaded" for retry
- `GET    /healthz`                    — Health check
- `GET    /readyz`                     — Readiness check

## Quick Start
```bash
cp .env.example .env    # fill in your env vars
go mod tidy
go run cmd/server/main.go
```
