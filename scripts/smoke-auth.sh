#!/usr/bin/env bash

# The backend intentionally fails closed when controlled-automation approval
# capabilities cannot be signed. Smoke suites run in production mode, so give
# every suite a dedicated test-only key without weakening that startup gate.
: "${HAI_APPROVAL_PROOF_SIGNING_KEY:=ci-smoke-approval-proof-signing-key-v1-0123456789}"
export HAI_APPROVAL_PROOF_SIGNING_KEY

hai_smoke_mint_jwt() { # role, secret, optional subject
  local role="$1"
  local secret="$2"
  local subject="${3:-local-operator}"
  python3 - "${role}" "${secret}" "${subject}" <<'PY'
import base64
import hashlib
import hmac
import json
import sys
import time

role, secret, subject = sys.argv[1:4]

def encode(value):
    return base64.urlsafe_b64encode(value).rstrip(b"=").decode()

header = encode(json.dumps({"alg": "HS256", "typ": "JWT"}, separators=(",", ":")).encode())
payload = encode(
    json.dumps(
        {"sub": subject, "role": role, "exp": int(time.time()) + 3600},
        separators=(",", ":"),
    ).encode()
)
signature = encode(
    hmac.new(secret.encode(), f"{header}.{payload}".encode(), hashlib.sha256).digest()
)
print(f"{header}.{payload}.{signature}")
PY
}

# The production composition only enables the local safe worker after the
# authenticated owner has activated a durable Constitution.  Smoke suites use
# this helper to exercise that exact API flow; they must restart the backend
# afterwards so it rebuilds its authorization boundary from the durable record.
hai_smoke_activate_baseline_constitution() { # api base, backend key, owner jwt
  local base="$1"
  local api_key="$2"
  local owner_jwt="$3"
  local headers=(-H "X-HAI-Backend-Key: ${api_key}" -H "Content-Type: application/json" -H "Authorization: Bearer ${owner_jwt}")
  local draft draft_id activated

  draft="$(curl -fsS "${headers[@]}" -X POST "${base}/framework-registry/constitution/drafts" \
    -d '{"baseVersion":1,"changeSummary":"Smoke owner reviewed the local safe-worker baseline policy."}')"
  draft_id="$(printf '%s' "${draft}" | jq -r '.id // empty')"
  [ -n "${draft_id}" ] || {
    echo "unable to create durable Constitution draft: ${draft}" >&2
    return 1
  }

  activated="$(curl -fsS "${headers[@]}" -X POST "${base}/framework-registry/constitution/${draft_id}/activate" \
    -d '{"confirmation":"ACTIVATE CONSTITUTION","approvalNote":"Smoke owner approves the local safe-worker baseline policy."}')"
  [ "$(printf '%s' "${activated}" | jq -r '.status // empty')" = "active" ] || {
    echo "unable to activate durable Constitution: ${activated}" >&2
    return 1
  }
}
