# Progress Report — Universal Media Service Backend (Go)

**Report date:** 2026-10-07
**Scope:** `universal-media-service` (Go API), including verification against the live production deployment on Cloud Run.
**Repo state:** HEAD `e73cd74`, 35 commits, CI green (`gofmt`, `go vet`, `go build`, `go test -race`).
**Companion:** frontend report in `universal-media-ui/PROGRESS_REPORT.md`.

---

## 1. Headline

- **Overall completion: ≈ 90%** (weighted; weights in §2)
- Feature-complete for images, video, and audio ingest + processing, with real-time status delivery, sharing, trash/restore, and production deployment.
- The gap to "production-grade" is **not features** — it is **observability, automated integration testing, secret hygiene, and one hard platform limit on upload size**.

### Verification evidence (all run against production on 2026-10-07)

| Check | Result |
|---|---|
| CI (vet / build / `-race`) | green on HEAD |
| `scripts/phase0-qa.sh` | **18 passed, 0 failed**, 1 skipped (2-user IDOR, optional) |
| API E2E (curl) | upload → X-Cache MISS→HIT → rename → share 302 → R2 200 → reprocess → worker → SSE `event: status` → delete; list ends at 0 |
| Browser E2E (real UI) | Clerk sign-in → dashboard → upload through the drag & drop UI → `/info` 200 → `/process` 200 `image/jpeg` |
| CORS | preflight 204 from Vercel origin, 403 for a foreign origin |
| Health | `/health` 200, `/readyz` 200 (DB ping) |
| Logs | no app ERROR/panic since rollout; only INFO from deliberate negative tests |
| Deploy | image `sha256:3c0fbd90034e…`, revision `media-server-00010-lbt` @ 100% traffic, env preserved |

---

## 2. Completion by area

| # | Area | Weight | Complete | Evidence / gap |
|---|---|---|---|---|
| 1 | Architecture & routing | 10 | 100% | Layered `api`/`adapters`/`core`/`internal`; explicit route table with middleware ordering |
| 2 | Auth & security | 10 | 90% | Clerk JWT, per-user 100 req/min + IP flood 300 req/min, HMAC share links, CORS allow-list, fail-fast secrets. Gap: `DATABASE_URL` & `SHARE_SECRET` in plaintext env |
| 3 | Upload & CRUD | 12 | 100% | Multipart + streaming, MIME allow-list, replace-in-place, trash/restore/permanent, batch delete, pagination/search/sort |
| 4 | Image processing | 15 | 95% | Resize/crop(9 gravities)/blur/grayscale/quality/thumbnail, URL-driven params, content-versioned Redis cache, `X-Cache`. Gap: watermark, auto-enhance |
| 5 | Video & audio | 10 | 80% | ffprobe metadata, video thumbnail + stream copy, audio conversion. Gap: transcode options never exposed (dead code), no A/V dynamic processing |
| 6 | Storage (R2) | 8 | 90% | Raw/processed/thumbnail objects, derived-asset cleanup. Gap: lifecycle policies |
| 7 | Database (Postgres) | 8 | 95% | Status machine, soft delete, search/sort/paginate. Gap: versioning/audit trail |
| 8 | Async worker | 8 | 100% | Lease claiming, retry backoff, panic recovery, reprocess endpoint |
| 9 | Real-time (SSE) | 6 | 100% | Redis pub/sub, token exchange endpoint, `event: status` frames, poll fallback when Redis is absent |
| 10 | Observability | 5 | 45% | Structured logs + health/readiness only. Gap: metrics, tracing, alerting |
| 11 | Testing & QA | 5 | 65% | 8 unit test files run with `-race`; 18-check QA script; E2E run manually. Gap: no automated integration/E2E in CI |
| 12 | Deployment & CI | 3 | 100% | Multi-stage Dockerfile (ffmpeg), Artifact Registry, Cloud Run, GitHub Actions on push/PR |
| | **Weighted total** | **100** | **≈ 90%** | |

---

## 3. What is remaining (prioritized)

| Priority | Item | Why it matters | Size |
|---|---|---|---|
| **P0** | **Uploads larger than ~32 MB fail in production** — Cloud Run's front end returns an HTML `413` before the app sees the request (verified today with a 35 MB body; small upload succeeded). The configured 500 MB app limit and the >50 MB deferred-processing path are therefore unreachable in prod. **Full analysis, options, and phased plan: §4 below.** | The 50 MB async/SSE path, worker UX, and the documented size limits are misleading; large video uploads are impossible. Direction of travel: presigned direct-to-R2 uploads. | **L** |
| **P1** | **Secret hygiene** — move `DATABASE_URL` (Neon password in plaintext) and `SHARE_SECRET` (currently the placeholder `pick-a-random-string`) to Secret Manager and rotate both. | Credential exposure via Cloud Run console/IAM readers; weak HMAC key for share links. | **S** |
| **P1** | **Clerk production instance** — the deployed UI uses a `pk_test_`/`sk_test_` development instance (`learning-dingo-96.clerk.accounts.dev`). | Dev-instance users/sessions are not production-grade; instance migration is a coordinated UI + backend env change. | **S** |
| **P1** | **Observability** — export request/processing metrics (upload count, latency, worker queue depth, SSE connections, cache hit rate) + uptime checks and error alerting. | Today a silent failure only shows up when someone runs the QA script. | **M** |
| **P1** | **Automated integration tests in CI** — run `phase0-qa.sh` (and the E2E scripts) against a staging service on push. | All E2E proof is currently manual; regressions in auth/CORS/SSE would ship unnoticed. | **M** |
| **P2** | **Expose or delete video transcode options** — `core/video.transcode()` is unreachable; either surface quality/scale/codec params for A/V or remove it. | Dead code implies a capability the API does not have. | **S** |
| **P2** | **R2 lifecycle policies** — expire abandoned raw/processed objects, monitor storage cost. | Unbounded storage growth. | **S** |
| **P2** | **Media versioning / audit trail** — history of replaces, who changed what. | Currently replace rewrites the row in place with no history. | **M** |
| **P3** | Watermark, auto-enhance, EXIF stripping policy | Nice-to-have effects. | **M** |
| **P3** | Text/OCR processing (design-doc future) | Out of current product scope. | **L** |
| **P3** | Queue (Kafka) / Kubernetes migration from the design doc | Deliberately replaced by in-process worker + Redis pub/sub; revisit only if scale demands it. | **L** |

---

## 4. Deep dive: lifting the upload size limit (P0)

### What we verified (2026-10-07)

- `POST /api/v1/media` with a 35 MB body → **HTML `413 Request Entity Too Large` from Google's front end** (never reaches Gin); a 1 MB upload with the same token → `200`.
- The 32 MB figure is the **HTTP/1 request limit between Cloud Run's load balancer and the container**. It is not a quota that can be raised.
- Everything downstream of that cap is currently decorative: app `maxUploadBody` = 500 MB, deferred-processing threshold `MaxSyncProcessingSize` = 50 MB, UI client cap = 500 MB. None of them can trigger in production.

### Options

| Option | What it takes | Ceiling | Pros | Cons | Verdict |
|---|---|---|---|---|---|
| **A. HTTP/2 end-to-end (h2c)** — tick "Use HTTP/2 end-to-end" and serve h2c (wrap Gin with `h2c.NewHandler`) | ~½ day: one Cloud Run flag + one server wrapper | Undocumented; community reports the 413 disappears | Cheapest; no API change | Protocol workaround, not a documented guarantee; reports of 502s and killed long-running h2 requests; still leaves the app buffering whole files in RAM | **Experiment only** (afternoon spike, keep only if it proves stable) |
| **B. Chunked upload through our API** — parts ≤ 32 MB, reassembled server-side | Large: temp-disk spooling, chunk manifest in Postgres/Redis, TTL cleanup, resume protocol | Arbitrary | All bytes stay behind our auth; no R2 CORS change | Instances aren't sticky (no in-memory state), reassembly complexity, N round-trips, slowest path, most code to maintain | **Reject** — solves the wrong problem |
| **C. Presigned direct-to-R2** — backend issues a scoped PUT URL, client uploads to R2, backend verifies and enqueues processing | Medium: 2 new endpoints, status-machine change, R2 CORS, verification step | **5 GiB** single PUT; **5 TiB** via R2 multipart (parts 5 MiB–5 GiB, 10,000 parts) | Platform-independent; backend never sees the bytes (bandwidth + memory win); the industry-standard pattern (GCS/R2 signed URLs are Google's own recommended fix); unlocks resumable multipart later | More moving parts: bucket CORS, post-upload verification, orphan cleanup | **Recommended** |

### Recommended design (Option C)

```
1. POST /api/v1/media/uploads      auth → validate name/MIME/declared size/quota
                                   → insert row (status "pending", key reserved)
                                   → return { mediaID, uploadURL }   (presigned PUT, ~15 min)
2. PUT  <uploadURL>                browser → R2, directly (CORS: UI origin, method PUT,
                                   expose ETag). Cloud Run never sees these bytes.
3. POST /api/v1/media/:id/complete auth → HeadObject in R2 (exists? size == declared?)
                                   → status "uploaded" → process inline (small) or
                                   enqueue worker (large) → SSE "status" event
```

Details that decide whether this is safe:

- **Size enforcement:** presign-time we validate the declared size against a per-user quota and a per-file max; post-upload `HeadObject` re-checks actual size and deletes the object if it lies. (S3 `content-length-range` is only available on POST policies, not presigned PUT — accept post-hoc enforcement, or switch to a POST policy if hard pre-enforcement is required.)
- **Status machine:** add `pending` (URL issued, no object yet) → `uploaded` → `ready`/`failed`. Needs a sweeper that expires stale `pending` rows, plus an R2 lifecycle rule for orphaned `raw/` objects.
- **Orphan safety:** `complete` is idempotent; a lost `complete` leaves `pending` → swept, object GC'd by lifecycle rule.
- **Transport ceiling vs processing ceiling:** lifting the cap is useless unless processing stops buffering whole files — video/audio `Process(data []byte)` would OOM a 512 MB instance on a 300 MB file. Large-file work must go: download-to-temp-file in the worker, or streaming from the start.
- **Quota:** an uncapped upload path needs a per-user storage quota (e.g. 2 GB default) enforced at presign time, or it is an open-ended bill.

### Phases

| Phase | Scope | Size | Acceptance |
|---|---|---|---|
| **0. Honest limits** | UI guard ≤ 30 MB with a clear message; map HTML 413 → friendly text; document the real cap in README | **S** (½ day) | 35 MB upload in prod shows "file too large for this deployment (max ~30 MB)", no generic retry loop |
| **1. Make the async path real** | Lower `MaxSyncProcessingSize` to something under the transport cap (e.g. 15–20 MB) so videos process in the worker; confirm video/audio paths use temp files, never full buffering | **M** (1–2 days) | A 25 MB video: upload → `uploaded` → worker → `ready` + SSE event observed in the browser |
| **2. Direct-to-R2 presigned uploads** | Presign + complete endpoints, `pending` status, sweeper, R2 CORS, quota, UI 3-step flow with progress | **L** (3–5 days) | 500 MB mp4 uploads end-to-end and is processed by the worker; QA script gains a presign/complete suite; orphans cleaned |
| **3. Resumable multipart (optional)** | R2 multipart with presigned part URLs, parallel parts, resume after network drop | **M** | Kill the tab mid-500 MB upload, return, resume without restarting |

**Sequencing opinion:** Phase 0 is a day of honesty and should not wait. Phase 1 is worth doing regardless of Phase 2 because it fixes in-request CPU/time pressure for every video, not just big ones. Phase 2 is the actual product decision — do it before advertising large video uploads, not after someone's 400 MB file fails. Skip Option B entirely; treat Option A as a spike whose result (stable or not) gets written into `LESSONS.md`.

### Update — 2026-10-08: Phases 0, 1 and 2 implemented (backend + UI)

Code complete, committed locally, not yet pushed/deployed.

**Phase 1 — async path made real**
- `MaxSyncProcessingSize` lowered 50 MB → **16 MiB**; worker `process()` now streams: `storage.DownloadTo` → temp file → `ProcessFile(ctx, inputPath, …)` → outputs streamed via `storage.Upload(os.File)`. `Process([]byte)` kept as a thin wrapper. R2 uploader tuned to PartSize 16 MB / Concurrency 2.
- Acceptance run locally: 20.36 MB mp4 → multipart upload → `uploaded` → worker → `ready` in ~3 s, correct dimensions/duration, server RSS flat at 97 MB.

**Phase 2 — presigned direct-to-R2**
- New endpoints: `POST /api/v1/media/uploads` (Begin: validates family/declared size/quota, creates hidden `pending` row, returns `{id, uploadUrl, expiresIn}`) and `POST /api/v1/media/:id/complete` (Complete: `Head` size check → 512-byte family sniff → flips to `uploaded`, idempotent for completed rows).
- New `pending` status: invisible to list/default queries; abandoned rows reaped by a worker sweeper (5 min tick, 2 h age, batch 50).
- Quota: `USER_STORAGE_QUOTA_BYTES` (default 10 GiB) enforced at presign against `SUM(size_bytes)` over **all** the user's rows including trashed and pending (races accepted).
- Caps as env config: `MAX_UPLOAD_BYTES` (500 MiB), `MAX_IMAGE_BYTES` (32 MiB).
- Errors mapped: unsupported type/invalid request → 400, file too large → 413 (with cap in message), quota/not-started → 409, size/content mismatch → 400. Objects of mismatched uploads are deleted immediately.
- Sniffing: `http.DetectContentType` misses mp4/mov/flac/mp3/aac (returns generic `application/octet-stream`), so `core/media/sniff.go` implements ffmpeg-compatible container detection used by **both** the multipart path and presign complete.
- Acceptance: full local E2E against real Neon+R2 (happy path, content mismatch + purge, size mismatch, complete-before-PUT, pending hidden) plus a new QA suite — **31 passed / 0 failed / 1 skipped** locally.

**Phase 0 — honest limits (UI)**
- `lib/upload-limits.ts`: `MULTIPART_MAX_BYTES` 30 MB (GFE-safe), image 32 MB, video/audio 500 MB, per-file `precheckFile`.
- `lib/api-error.ts`: non-JSON/HTML 413 → clear GFE message; new `presignPutErrorMessage` (status 0 = network/CORS wording).
- Replace flow guarded at 30 MB and now surfaces real API errors instead of a generic toast.

**Documented deviations**
- **Presigned PUT does not bind Content-Type.** aws-sdk-go-v2 (s3 v1.95.1) signs only `host` for `PresignPutObject` even with `ContentType` set (`X-Amz-SignedHeaders=host`, verified: mismatched PUT returns 200). Enforcement therefore happens at Complete via the family-level sniff — a jpeg declared as `video/mp4` is rejected (400) and the object deleted (verified).
- **Size check is one-directional by design:** actual > declared → 400 + object deleted; actual < declared is accepted (quota over-reserved in the safe direction, comment in `CompleteDirectUpload`).

**Remaining before this can ship:** R2 bucket CORS for the browser PUT (token lacks `PutBucketCORS` — needs the dashboard or a wider token; curl QA unaffected), push + deploy (`--ephemeral-storage=2Gi`), post-deploy QA + SSE check, then a ≥500 MB prod E2E.


---

## 5. Known limitations & deliberate trade-offs

| Limitation | Status |
|---|---|
| Requests >32 MB → Cloud Run front end `413` (HTML) | Platform limit; workaround is P0 above |
| `/healthz` unreachable from outside Cloud Run (GFE answers instead of the app) | Mitigated: `/health` alias added 2026-10-07; QA script updated |
| SSE status events only fire for deferred (>50 MB) processing and `reprocess` | By design — small uploads are processed inline and go straight to `ready` |
| `/process` returns 400 for video/audio | By design — dynamic transforms are image-only |
| `X-Cache` not visible to cross-origin `fetch()` | CORS only exposes safelisted response headers; visible via curl |
| Vercel REST API cannot read env vars with the deploy token (`forbidden`) | Token scope; verify env vars behaviorally instead |
| Worker is in-process (scales with Cloud Run instances) | Simpler than Kafka; lease claiming prevents double work |

---

## 6. Housekeeping

- Uncommitted at report time: `adapters/http/health.go`, `scripts/phase0-qa.sh`, `LESSONS.md` (new), `README.md`, `PROGRESS_REPORT.md`.
- `SystemDesignDoc.md` describes the original design (Kafka, Kubernetes, Prometheus, Loki); it is kept as design intent and now carries a status banner pointing here.
