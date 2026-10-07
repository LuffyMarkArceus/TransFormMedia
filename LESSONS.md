# Lessons Learned — Universal Media Service

opencode -s ses_15b881786ffeeM4mTpcZiX6ov2

A living document of gotchas, trade-offs, and architectural decisions encountered while building a universal media processing service (Go + Next.js, deployed to Cloud Run + Vercel).

---

## 1. Deployment Infrastructure

### Stack
| Component | Choice | Why |
|-----------|--------|-----|
| Backend | Go + Gin | Fast startup, single binary deploy, great concurrency |
| Frontend | Next.js (React) | SSR, Vercel deployment, good DX |
| Auth | Clerk | Handles JWT, MFA, social login out of the box |
| Storage | Cloudflare R2 | S3-compatible, no egress fees to Cloudflare network |
| Database | NeonDB (Postgres) | Serverless, autoscaling, Postgres compatibility |
| Backend host | GCP Cloud Run | Serverless containers, pay-per-use, auto-scales to zero |
| Frontend host | Vercel | Native Next.js support, auto-deploys from GitHub |
| Image lib | `github.com/disintegration/imaging` | Pure Go, no CGO, simple API, Lanczos resampling |

### Cloud Run Gotchas

#### `--allow-unauthenticated` is required when using app-level auth
Cloud Run has two layers of auth:
1. **GCP IAM layer** — checks before your container even sees the request
2. **Application layer** — your code (Clerk JWT middleware)

If you deploy with `--no-allow-unauthenticated`, GCP IAM blocks all unauthenticated requests, including CORS `OPTIONS` preflight (which never carries auth headers). The browser sends preflight → GCP rejects it → CORS middleware never runs → browser sees opaque failure.

**Fix:** Deploy with `--allow-unauthenticated` and let your application (Clerk JWT) handle auth. This is the recommended pattern for Cloud Run + BYO auth.

```bash
gcloud run deploy media-server \
  --image us-central1-docker.pkg.dev/ums-media-forge/ums-backend/latest \
  --allow-unauthenticated \
  --region us-central1
```

#### Google's frontend intercepts the bare path `/healthz`
From outside Cloud Run, `GET /healthz` returns Google's own HTML 404 — the request never reaches your container, even though the gin route is registered (proof: `GET /healthz/` returns gin's `301 → /healthz`). Inside the VPC / from Cloud Run's own TCP startup probe this does not apply (our startup probe is TCP, so deploys are unaffected).

**Fix:** register both paths and probe `/health` from the outside:

```go
ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) }
r.GET("/health", ok)
r.GET("/healthz", ok)
```

`scripts/phase0-qa.sh` asserts `/health`, not `/healthz`.

#### Cloud Run + Artifact Registry Multi-Region
Artifact Registry repos are regional. If your Cloud Run is in `us-central1`, your Artifact Registry repo must also be in `us-central1` (or use cross-region replication). Pushing to one region and deploying in another requires `locations` configuration.

#### Cloud Run + Docker
Cloud Run accepts containers from Artifact Registry or Container Registry. Use Artifact Registry (newer, better IAM).

```bash
# Build
docker build -t us-central1-docker.pkg.dev/ums-media-forge/ums-backend/latest .

# Push
docker push us-central1-docker.pkg.dev/ums-media-forge/ums-backend/latest

# Deploy
gcloud run deploy media-server \
  --image us-central1-docker.pkg.dev/ums-media-forge/ums-backend/latest \
  --allow-unauthenticated \
  --region us-central1 \
  --memory 512Mi \
  --cpu 1 \
  --min-instances 1 \
  --max-instances 3 \
  --timeout 300
```

The `--min-instances 1` prevents cold starts at the cost of one always-on instance (~$5-15/mo depending on CPU/memory).

---

## 2. Vercel + Cloud Run Proxy Pitfalls

### The 10MB Request Body Limit
Next.js rewrites/reverse proxy has a **hard default of 10MB** for request bodies. If a user uploads a video > 10MB, the request is silently truncated at the Vercel edge — no error, just a partial file.

**Fix:** Set `experimental.proxyClientMaxBodySize` in `next.config.ts`:
```ts
const nextConfig = {
  experimental: {
    proxyClientMaxBodySize: "500mb",
  },
};
```

**Lesson:** When using a proxy layer, always check if it has body size limits. Vercel's is undocumented enough that you'll find it via trial and error.

### Vercel Edge DNS Cannot Resolve Cloud Run `run.app` URLs
When using Next.js rewrites to proxy requests to Cloud Run, Vercel's edge network throws `DNS_HOSTNAME_RESOLVED_PRIVATE` error. Cloud Run `run.app` domains resolve to private IPs in GCP's network, and Vercel edge nodes can't reach them.

**Fix:** Skip the proxy layer entirely. Use `NEXT_PUBLIC_BACKEND_URL` pointing directly to the Cloud Run URL and make API calls from the browser:

```ts
// lib/api.ts
const base = (typeof process !== "undefined" && process.env.NEXT_PUBLIC_BACKEND_URL) || ""
export const API_V1 = `${base}/api/v1`
```

**Trade-off:** Direct browser→Cloud Run calls expose your backend URL. Since auth is handled by Clerk JWT (not GCP IAM), this is safe — unauthenticated requests are rejected at the application layer. You also lose Vercel edge caching of API responses.

### CORS with Direct Browser Calls
Since the browser calls Cloud Run directly, you need CORS middleware that accepts your Vercel frontend origin:

```go
func corsConfig() cors.Config {
    return cors.Config{
        AllowOriginFunc: func(origin string) bool {
            if origin == "https://ums-media-forge-ui.vercel.app" {
                return true
            }
            if strings.HasSuffix(origin, ".vercel.app") {
                return true
            }
            return false
        },
        AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
        AllowHeaders:     []string{"Authorization", "Content-Type"},
        AllowCredentials: true,
        MaxAge:           300,
    }
}
```

**Note:** `AllowOriginFunc` is more flexible than the static `AllowOrigins` list — use it whenever origins are dynamic but follow a pattern.

---

## 3. Cloudflare R2 + AWS SDK v2

### R2 Needs an Explicit Region
Cloudflare R2 is S3-compatible but uses `Region: "auto"` instead of a real AWS region. Without setting the region, the AWS SDK returns opaque errors:

```
endpoint rule error, A region must be set
```

**Fix:**
```go
r2Client, err := s3.New(ctx, func(o *s3.Options) {
    o.Region = "auto"
    o.BaseEndpoint = aws.String("https://<account>.r2.cloudflarestorage.com")
    o.Credentials = aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
        os.Getenv("R2_ACCESS_KEY"),
        os.Getenv("R2_SECRET_KEY"),
        "",
    ))
})
```

**Lesson:** "S3-compatible" is 99% compatible — the 1% differences (region handling, endpoint format) will bite you. Always test basic CRUD operations against the actual service early.

---

## 4. Frontend Blob URL Management

### The Blob URL Revoke Race
When receiving a processed image as a blob and creating an object URL, you must revoke old URLs to prevent memory leaks. But there's a subtle race:

**The wrong approach:**
```tsx
// ❌ Broke: cleanup revokes before React re-renders
useEffect(() => {
  const url = URL.createObjectURL(blob);
  setObjectUrl(url);
  return () => URL.revokeObjectURL(url); // ❌ revokes current URL on cleanup!
}, [blob]);
```

The cleanup runs when deps change (before the new effect runs), but React hasn't re-rendered the `<img>` tag yet. The old URL is revoked while the `<img>` still references it → `ERR_FILE_NOT_FOUND` in console.

**The fix — useRef tracking:**
```tsx
const objectUrlRef = useRef<string | null>(null);

useEffect(() => {
  const url = URL.createObjectURL(blob);
  if (objectUrlRef.current) {
    URL.revokeObjectURL(objectUrlRef.current); // revoke old, not current
  }
  objectUrlRef.current = url;
  setObjectUrl(url);
}, [blob]);

// Only revoke on unmount
useEffect(() => {
  return () => {
    if (objectUrlRef.current) {
      URL.revokeObjectURL(objectUrlRef.current);
    }
  };
}, []);
```

**Key insight:** Never revoke a URL in an effect's cleanup if that effect might re-run. Always use a ref to store the previous URL and revoke it *before* assigning the new one. The cleanup only runs on unmount.

---

## 5. State Management Patterns

### URL as Single Source of Truth
The image editor uses a pattern where the URL query string is the canonical source of truth for processing params, not React state:

```
User drags slider → local uiParams state (instant) → debounce 400ms → URL.replace()
  ↓                                         ↓
CSS filter preview (instant, no network)   URL update triggers re-fetch from backend
  ↓                                         ↓
params prop (from URL) → ImagePreview       params prop changes → ImagePreview re-fetches
```

**Why:** This makes each editor state shareable/bookmarkable via URL. The URL is the "committed" state, while `uiParams` is the "draft" state for instant CSS previews.

### The State Cascade Trap
When implementing [compare mode](#7-compare-mode), holding the compare button triggered state in the parent (`page.tsx`) that caused a full re-render cascade, interfering with debounce/URL effects and causing spurious HTTP requests.

**Lesson:** Keep transient UI state (like "is the user holding the compare button?") as **local state** in the component that uses it, not in the parent. Parent state is for data that affects multiple children or persists across renders.

### Debounce Granularity
400ms debounce balances responsiveness and server load:
- Too fast (0-100ms): fires on every slider tick, floods backend
- Too slow (1000ms+): feels sluggish, user waits to see result
- 400ms: feels instant for most users, reduces requests by ~10x vs no debounce

---

## 6. Image Processing Pipeline

### Params Can Be Silently Dropped
The image processing pipeline has three places where params must be explicitly forwarded:
1. **URL serialization** (page.tsx → URLSearchParams)
2. **URL parsing** (page.tsx useMemo → ProcessParams)
3. **Backend request** (ImagePreview.tsx → fetch URL)

If any of these three paths misses a parameter (e.g., `blur`), it's silently dropped — no error, no warning. The processed image just doesn't have the effect.

**Lesson:** When adding a new param, add it to all three places simultaneously. Create a shared type (`ProcessParams`) and helper functions (`serializeProcessParams`, `parseProcessParams`) to reduce the chance of forgetting one.

### CSS Filter Instant Preview vs Backend Processing
Sliders use CSS filters for instant visual feedback while waiting for the debounced backend response:

```
Blur slider drag → CSS `filter: blur(10px)` applied immediately to <img>
                  → 400ms later → backend processes → new image replaces old
                  → CSS filter cleared (backend result already has blur baked in)
```

**Key:** The CSS filter is only applied when `previewParams` differs from `committedParams` (the params the current image was processed with). Once the backend returns a new image, the CSS filter is removed.

```tsx
const previewFilter = useMemo(() => {
  if (comparing || !previewParams) return "";
  const parts: string[] = [];
  if (previewParams.blur > 0 && previewParams.blur !== committedParams.blur) {
    parts.push(`blur(${previewParams.blur}px)`);
  }
  if (previewParams.grayscale && !committedParams.grayscale) {
    parts.push("grayscale(1)");
  }
  return parts.join(" ");
}, [previewParams, committedParams, comparing]);
```

### Undo Stack
Ctrl+Z pops from an undo stack that captures all slider changes:

```tsx
const onTransformChange = useCallback((next: ProcessParams) => {
  undoStackRef.current.push(uiParamsRef.current); // push current before changing
  setUiParams(next);
}, []);
```

**Gotcha:** Using `useRef` for the undo stack instead of `useState` avoids re-renders on push. Only `setUiParams(prev)` triggers a re-render on undo.

---

## 7. Compare Mode

### What It Does
Hold a button to see the original (unprocessed) image, release to see the processed version. Like Photoshop's before/after toggle.

### Architecture
```
Button press → comparing = true
             → If no cached clean image: fetch from backend (same params, no blur/grayscale)
             → Cache as blob URL in compareUrlRef + compareUrl state
             → Swap <img> src from objectUrl to compareUrl
Button release → comparing = false
               → Swap <img> src back to objectUrl
```

### Blob URL Lifecycle for Compare
```tsx
// Ref for cleanup
const compareUrlRef = useRef<string | null>(null);

// State for render
const [compareUrl, setCompareUrl] = useState<string | null>(null);

// Clear cache when params change
useEffect(() => {
  setCompareUrl(null);
  return () => {
    if (compareUrlRef.current) {
      URL.revokeObjectURL(compareUrlRef.current);
      compareUrlRef.current = null;
    }
  };
}, [imageId, params]);

// Fetch clean image on first compare press
useEffect(() => {
  if (!comparing || compareUrl !== null) return;
  // fetch clean version (all same params except blur/grayscale)
  // create blob URL, store in compareUrlRef + setCompareUrl
}, [comparing, compareUrl, ...]);

// Render
const displayUrl = (comparing && compareUrl) ? compareUrl : objectUrl ?? undefined;
```

### Cleanup Chain
1. On unmount: component cleanup revokes both `objectUrlRef.current` and `compareUrlRef.current`
2. On params change: `[imageId, params]` effect clears `compareUrl`, ref revoked
3. On compare re-fetch: old `compareUrlRef.current` is revoked before assigning new

---

## 8. CORS Deep Dive

### Preflight Requests Have No Auth Headers
Browser `OPTIONS` preflight requests do NOT include `Authorization` headers. This means:
- If Cloud Run uses `--no-allow-unauthenticated`, GCP IAM rejects the preflight → `403`
- If your app's CORS middleware rejects the origin → no `Access-Control-Allow-Origin` header → browser fails

The CORS middleware must run **before** auth middleware:

```go
r := gin.Default()
r.Use(cors.New(corsConfig())) // CORS first
r.Use(clerkAuthMiddleware())  // Auth second
```

### Credentials Mode
When using `Authorization` headers + cookies, CORS must include:
```
Access-Control-Allow-Credentials: true
```
And the client must include:
```ts
fetch(url, { credentials: "include" })
// or for Clerk:
fetch(url, { headers: { Authorization: `Bearer ${token}` } })
```

### Vercel .vercel.app Suffix Wildcard
If you have preview deployments on Vercel, each preview gets a unique subdomain like `project-name-abc123.vercel.app`. Use a suffix check instead of listing individual URLs:

```go
if strings.HasSuffix(origin, ".vercel.app") {
    return true
}
```

---

## 9. Docker Build for Go + FFmpeg

### Multi-Stage Build for Small Images
```dockerfile
# Build stage
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ffmpeg
WORKDIR /app
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
```

**Why:**
- **golang:1.25-alpine** for build (has compiler, ~350MB)
- **alpine:3.21** for runtime (no compiler, ~5MB + ffmpeg ~30MB)
- **CGO_ENABLED=0** produces a statically linked binary with no libc dependency
- Final image: ~35MB vs ~400MB single-stage

### FFmpeg in Alpine
Alpine uses `apk` (not `apt`). The package is just `ffmpeg`:
```dockerfile
RUN apk add --no-cache ffmpeg
```

Test that it works:
```dockerfile
RUN ffmpeg -version
```

---

## 10. General Go Patterns

### Graceful Shutdown
```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := server.Shutdown(ctx); err != nil {
    log.Fatalf("Server shutdown failed: %v", err)
}
```

Cloud Run sends `SIGTERM` when scaling down. Your server must handle it gracefully — finish in-flight requests within the grace period (default 10s, configurable via `--timeout`).

### Structured Logging (slog)
```go
log.Printf("Processing image: id=%s blur=%d grayscale=%v", id, blur, grayscale)
```

Go 1.21+ has `log/slog` built-in. For Cloud Run, structured logs to stdout are automatically picked up by Google Cloud Logging.

### Router Groups
```go
v1 := r.Group("/api/v1")
v1.Use(authMiddleware)
{
    v1.POST("/media", uploadHandler)
    v1.GET("/media", listHandler)
    v1.GET("/media/:id/process", processHandler)
    // ...
}
```

Grouping routes by version + auth status keeps the code organized and middleware clear.

---

## 11. Environment Variables & Configuration

### Backend (.env)
```
DATABASE_URL=postgres://...
R2_ACCESS_KEY=...
R2_SECRET_KEY=...
R2_BUCKET=ums-media-forge
R2_PUBLIC_BASE_URL=https://pub-<hash>.r2.dev
CLERK_SECRET_KEY=sk_test_...
CLERK_PUBLISHABLE_KEY=pk_test_...
```

### Frontend (.env.local)
```
NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY=pk_test_...
CLERK_SECRET_KEY=sk_test_...
NEXT_PUBLIC_BACKEND_URL=https://media-server-xxxxx-uc.a.run.app
NEXT_PUBLIC_R2_PUBLIC_BASE_URL=https://pub-<hash>.r2.dev
```

**Key:** `NEXT_PUBLIC_` prefix makes the variable available to browser code. Variables without the prefix are server-side only in Next.js.

**Verifying env vars in production:** the Vercel REST API (`GET /v9/projects/{id}/env`) requires an env-read scope. A token that can deploy but cannot read env vars returns `{"error":{"code":"forbidden","message":"Not authorized"}}` — don't mistake that for missing vars. Verify behaviorally instead: `BACKEND_URL` by hitting the Next rewrite (`/api/v1/media` → backend 401), and `NEXT_PUBLIC_BACKEND_URL` by grepping the built chunks for the inlined Cloud Run URL.

---

## 12. Redis Caching (the right way)

### Cache Key Design
```
processed:{imageID}:{hash(params)}
```

### When to Cache
- Cache processed images (expensive to recompute)
- Do NOT cache uploads (one-time operation)
- Do NOT cache metadata that changes frequently

---

## 13. Checklist for Adding New Features

1. **Add the param/field** to shared types (`ProcessParams`)
2. **Add serialization** to URLSearchParams (both `page.tsx` URL update and `ImagePreview.tsx` fetch)
3. **Add parsing** from URL in `page.tsx` useMemo
4. **Add backend handling** in the processor
5. **Add UI control** (slider, toggle, dropdown) in TransformPanel
6. **Update previewFilter** in ImagePreview if it supports instant CSS preview
7. **Add to all three** serialize/parse locations — forgetting any one means silent dropping

---

## 14. Common Mistakes (TL;DR Cheat Sheet)

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| CORS preflight fails with 403 | Cloud Run `--no-allow-unauthenticated` | Use `--allow-unauthenticated`, auth at app level |
| Vercel → Cloud Run fails with DNS error | Vercel edge can't resolve `run.app` private IPs | Use direct browser calls via `NEXT_PUBLIC_BACKEND_URL` |
| Uploads >10MB fail silently | Vercel proxy body limit | Set `proxyClientMaxBodySize` in next.config |
| R2 GetObject fails with region error | Missing `Region: "auto"` in S3 client options | Add `o.Region = "auto"` |
| Console `ERR_FILE_NOT_FOUND` on slider drag | Blob URL revoked before React re-renders | Use ref-based URL tracking, revoke old before assigning new |
| Backend processing param does nothing | Param not forwarded in one of 3 serialization paths | Add to page.tsx URL, ImagePreview fetch, and backend parser |
| Holding compare button triggers network request | `comparing` state in parent causes re-render cascade | Move transient UI state to local component state |
| Docker push fails with auth error | Not authenticated with Artifact Registry | Run `gcloud auth configure-docker us-central1-docker.pkg.dev` |
| Auth works locally but fails in production | CORS middleware runs after auth middleware | CORS middleware must be first in the chain |
