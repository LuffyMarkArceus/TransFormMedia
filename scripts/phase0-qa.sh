#!/usr/bin/env bash
# Phase 0 QA — Universal Media Service
#
# Usage:
#   ./scripts/phase0-qa.sh                    # health + unauthenticated checks only
#   CLERK_TEST_TOKEN="eyJ..." ./scripts/phase0-qa.sh   # full API checks
#   CLERK_TEST_TOKEN_B="eyJ..." MEDIA_ID_OTHER_USER="uuid" ./scripts/phase0-qa.sh  # IDOR checks
#
# Get a Clerk session token (browser, while signed in on dashboard):
#   await window.Clerk.session.getToken()
#
set -euo pipefail

API_BASE="${API_BASE:-http://localhost:8080}"
CLERK_TEST_TOKEN="${CLERK_TEST_TOKEN:-}"
CLERK_TEST_TOKEN_B="${CLERK_TEST_TOKEN_B:-}"
MEDIA_ID_OTHER_USER="${MEDIA_ID_OTHER_USER:-}"

PASS=0
FAIL=0
SKIP=0

green() { printf '\033[0;32m%s\033[0m\n' "$*"; }
red() { printf '\033[0;31m%s\033[0m\n' "$*"; }
yellow() { printf '\033[0;33m%s\033[0m\n' "$*"; }

pass() { PASS=$((PASS + 1)); green "  PASS: $1"; }
fail() { FAIL=$((FAIL + 1)); red "  FAIL: $1"; }
skip() { SKIP=$((SKIP + 1)); yellow "  SKIP: $1"; }

# $1=name $2=method $3=path $4=expected_status [$5=extra curl args]
assert_status() {
  local name="$1" method="$2" path="$3" expected="$4"
  shift 4
  local url="${API_BASE}${path}"
  local status
  status=$(curl -s -o /dev/null -w "%{http_code}" -X "$method" "$@" "$url")
  if [[ "$status" == "$expected" ]]; then
    pass "$name (HTTP $status)"
  else
    fail "$name — expected HTTP $expected, got HTTP $status ($method $path)"
  fi
}

section() {
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo " $1"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

section "1. Health & readiness (no auth)"
# /healthz is intercepted by Google's frontend (HTML 404) when reached from
# outside Cloud Run, so the reachable liveness alias is /health.
assert_status "GET /health" GET "/health" 200
assert_status "GET /readyz (DB ping)" GET "/readyz" 200

section "2. Protected routes reject missing auth"
assert_status "GET /api/v1/media without token" GET "/api/v1/media" 401
assert_status "POST /api/v1/media without token" POST "/api/v1/media" 401
assert_status "DELETE /api/v1/media/:id without token" DELETE "/api/v1/media/00000000-0000-0000-0000-000000000001" 401
assert_status "PATCH rename without token" PATCH "/api/v1/media/00000000-0000-0000-0000-000000000001/rename" 401 \
  -H "Content-Type: application/json" -d '{"name":"x"}'
assert_status "GET process without token" GET "/api/v1/media/00000000-0000-0000-0000-000000000001/process" 401

section "3. Invalid auth rejected"
assert_status "GET /api/v1/media with bogus token" GET "/api/v1/media" 401 \
  -H "Authorization: Bearer invalid-token"

section "4. Not found / bad input (with auth)"
if [[ -z "$CLERK_TEST_TOKEN" ]]; then
  skip "Authenticated tests — set CLERK_TEST_TOKEN to run"
else
  AUTH=(-H "Authorization: Bearer ${CLERK_TEST_TOKEN}")

  assert_status "GET list media" GET "/api/v1/media" 200 "${AUTH[@]}"
  assert_status "DELETE unknown media id" DELETE "/api/v1/media/00000000-0000-0000-0000-000000000099" 404 "${AUTH[@]}"
  assert_status "GET process unknown media id" GET "/api/v1/media/00000000-0000-0000-0000-000000000099/process?w=100" 404 "${AUTH[@]}"
  assert_status "PATCH rename unknown media id" PATCH "/api/v1/media/00000000-0000-0000-0000-000000000099/rename" 404 \
    "${AUTH[@]}" -H "Content-Type: application/json" -d '{"name":"renamed"}'

  section "5. Upload + process + delete (happy path)"
  FIXTURE="$(mktemp /tmp/phase0-XXXXXX.png)"
  # Valid 1x1 PNG (base64)
  python3 -c "import base64,sys; sys.stdout.buffer.write(base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='))" > "$FIXTURE"

  UPLOAD_BODY=$(mktemp)
  UPLOAD_STATUS=$(curl -s -o "$UPLOAD_BODY" -w "%{http_code}" -X POST "${API_BASE}/api/v1/media" \
    "${AUTH[@]}" -F "file=@${FIXTURE};type=image/png")

  if [[ "$UPLOAD_STATUS" != "200" ]]; then
    fail "POST upload — expected HTTP 200, got HTTP $UPLOAD_STATUS"
    cat "$UPLOAD_BODY" >&2 || true
  else
    pass "POST upload (HTTP 200)"
    MEDIA_ID=$(python3 -c "import json,sys; print(json.load(open('$UPLOAD_BODY'))['id'])" 2>/dev/null || true)
    if [[ -z "${MEDIA_ID:-}" ]]; then
      fail "Could not parse media id from upload response"
    else
      pass "Parsed media id: $MEDIA_ID"

      assert_status "GET process own image" GET "/api/v1/media/${MEDIA_ID}/process?w=64&h=64" 200 "${AUTH[@]}"

      assert_status "DELETE own media" DELETE "/api/v1/media/${MEDIA_ID}" 200 "${AUTH[@]}"
      assert_status "GET process after delete" GET "/api/v1/media/${MEDIA_ID}/process?w=64" 404 "${AUTH[@]}"
    fi
  fi
  rm -f "$FIXTURE" "$UPLOAD_BODY"

  section "6. Presigned direct upload (Phase 2)"
  PFIX="$(mktemp /tmp/phase0-presign-XXXXXX.png)"
  python3 -c "import base64,sys; sys.stdout.buffer.write(base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='))" > "$PFIX"
  PSIZE=$(wc -c < "$PFIX" | tr -d ' ')

  json_body() {
    python3 -c 'import json,sys; print(json.dumps({"name": sys.argv[1], "contentType": sys.argv[2], "size": int(sys.argv[3])}))' "$1" "$2" "$3"
  }

  BODY=$(json_body "qa-unsupported.pdf" "application/pdf" 10)
  assert_status "Begin rejects unsupported type" POST "/api/v1/media/uploads" 400 \
    "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY"

  BODY=$(json_body "qa-big.jpg" "image/jpeg" $((33 * 1024 * 1024)))
  assert_status "Begin rejects image over 32 MB" POST "/api/v1/media/uploads" 413 \
    "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY"

  BODY=$(json_body "qa-zero.png" "image/png" 0)
  assert_status "Begin rejects size 0" POST "/api/v1/media/uploads" 400 \
    "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY"

  BODY=$(json_body "qa-presign.png" "image/png" "$PSIZE")
  BEGIN_BODY=$(mktemp)
  BEGIN_STATUS=$(curl -s -o "$BEGIN_BODY" -w "%{http_code}" -X POST "${API_BASE}/api/v1/media/uploads" \
    "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY")
  if [[ "$BEGIN_STATUS" != "201" ]]; then
    fail "Begin happy path — expected HTTP 201, got HTTP $BEGIN_STATUS"
    cat "$BEGIN_BODY" >&2 || true
  else
    pass "Begin happy path (HTTP 201)"
    PID=$(python3 -c "import json,sys; print(json.load(open('$BEGIN_BODY'))['id'])" 2>/dev/null || true)
    PURL=$(python3 -c "import json,sys; print(json.load(open('$BEGIN_BODY'))['uploadUrl'])" 2>/dev/null || true)

    if [[ -z "${PID:-}" || -z "${PURL:-}" ]]; then
      fail "Could not parse id/uploadUrl from begin response"
    else
      pass "Parsed presign id: $PID"

      # Pending rows must stay invisible until complete.
      LIST=$(curl -s "${API_BASE}/api/v1/media?limit=100" "${AUTH[@]}")
      if python3 -c "import json,sys; sys.exit(0 if any(m['id']=='$PID' for m in json.load(sys.stdin)['data']) else 1)" <<<"$LIST" 2>/dev/null; then
        fail "Pending upload leaked into media list"
      else
        pass "Pending upload hidden from list"
      fi

      assert_status "Complete before PUT → 409" POST "/api/v1/media/${PID}/complete" 409 "${AUTH[@]}"

      PUT_STATUS=$(curl -s -o /dev/null -w "%{http_code}" -X PUT -H "Content-Type: image/png" \
        --data-binary "@$PFIX" "$PURL")
      if [[ "$PUT_STATUS" =~ ^2[0-9][0-9]$ ]]; then
        pass "Presigned PUT (HTTP $PUT_STATUS)"
      else
        fail "Presigned PUT — expected 2xx, got HTTP $PUT_STATUS"
      fi

      COMPLETE_BODY=$(mktemp)
      COMPLETE_STATUS=$(curl -s -o "$COMPLETE_BODY" -w "%{http_code}" -X POST \
        "${API_BASE}/api/v1/media/${PID}/complete" "${AUTH[@]}")
      COMPLETE_STATE=$(python3 -c "import json,sys; print(json.load(open('$COMPLETE_BODY')).get('status',''))" 2>/dev/null || true)
      if [[ "$COMPLETE_STATUS" == "200" && "$COMPLETE_STATE" == "uploaded" ]]; then
        pass "Complete → 200 status=uploaded"
      else
        fail "Complete — expected 200/uploaded, got HTTP $COMPLETE_STATUS status=$COMPLETE_STATE"
      fi
      rm -f "$COMPLETE_BODY"

      # Content sniff: declare video/mp4, PUT PNG bytes → 400, object purged.
      BODY=$(json_body "qa-mismatch.mp4" "video/mp4" "$PSIZE")
      MISMATCH_BODY=$(mktemp)
      MISMATCH_STATUS=$(curl -s -o "$MISMATCH_BODY" -w "%{http_code}" -X POST "${API_BASE}/api/v1/media/uploads" \
        "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY")
      if [[ "$MISMATCH_STATUS" == "201" ]]; then
        MID=$(python3 -c "import json,sys; print(json.load(open('$MISMATCH_BODY'))['id'])")
        MURL=$(python3 -c "import json,sys; print(json.load(open('$MISMATCH_BODY'))['uploadUrl'])")
        curl -s -o /dev/null -X PUT -H "Content-Type: video/mp4" --data-binary "@$PFIX" "$MURL"
        assert_status "Content mismatch (PNG as video/mp4) → 400" POST "/api/v1/media/${MID}/complete" 400 "${AUTH[@]}"
        assert_status "Mismatched object purged (re-complete → 409)" POST "/api/v1/media/${MID}/complete" 409 "${AUTH[@]}"
      else
        fail "Begin for content-mismatch test — expected HTTP 201, got HTTP $MISMATCH_STATUS"
      fi
      rm -f "$MISMATCH_BODY"

      # Size mismatch: PUT more bytes than declared (quota was checked
      # against the declaration) → 400 and the object is removed.
      BODY=$(json_body "qa-size.png" "image/png" "$((PSIZE - 10))")
      SIZE_BODY=$(mktemp)
      SIZE_STATUS=$(curl -s -o "$SIZE_BODY" -w "%{http_code}" -X POST "${API_BASE}/api/v1/media/uploads" \
        "${AUTH[@]}" -H "Content-Type: application/json" -d "$BODY")
      if [[ "$SIZE_STATUS" == "201" ]]; then
        SID=$(python3 -c "import json,sys; print(json.load(open('$SIZE_BODY'))['id'])")
        SURL=$(python3 -c "import json,sys; print(json.load(open('$SIZE_BODY'))['uploadUrl'])")
        curl -s -o /dev/null -X PUT -H "Content-Type: image/png" --data-binary "@$PFIX" "$SURL"
        assert_status "Size mismatch (uploaded > declared) → 400" POST "/api/v1/media/${SID}/complete" 400 "${AUTH[@]}"
      else
        fail "Begin for size-mismatch test — expected HTTP 201, got HTTP $SIZE_STATUS"
      fi
      rm -f "$SIZE_BODY"

      assert_status "DELETE presigned media" DELETE "/api/v1/media/${PID}" 200 "${AUTH[@]}"
    fi
  fi
  rm -f "$PFIX" "$BEGIN_BODY"
fi

section "7. Cross-user IDOR (optional, needs two Clerk accounts)"
if [[ -z "$CLERK_TEST_TOKEN_B" || -z "$MEDIA_ID_OTHER_USER" ]]; then
  skip "IDOR tests — set CLERK_TEST_TOKEN_B and MEDIA_ID_OTHER_USER (media owned by another user)"
else
  AUTH_B=(-H "Authorization: Bearer ${CLERK_TEST_TOKEN_B}")
  assert_status "User B cannot DELETE User A media" DELETE "/api/v1/media/${MEDIA_ID_OTHER_USER}" 404 "${AUTH_B[@]}"
  assert_status "User B cannot PROCESS User A media" GET "/api/v1/media/${MEDIA_ID_OTHER_USER}/process?w=100" 404 "${AUTH_B[@]}"
  assert_status "User B cannot RENAME User A media" PATCH "/api/v1/media/${MEDIA_ID_OTHER_USER}/rename" 404 \
    "${AUTH_B[@]}" -H "Content-Type: application/json" -d '{"name":"stolen"}'
fi

section "8. Legacy route must not exist"
assert_status "Old /api/v1/images/:id/process returns 404" GET "/api/v1/images/00000000-0000-0000-0000-000000000001/process" 404

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " Results: ${PASS} passed, ${FAIL} failed, ${SKIP} skipped"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [[ "$FAIL" -gt 0 ]]; then
  exit 1
fi
