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
