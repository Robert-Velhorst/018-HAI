#!/usr/bin/env bash
# Two-account isolation acceptance test.
#
# Proves multi-user readiness against a RUNNING stack (real HTTP, real Postgres)
# rather than in-process fakes: two authenticated owners must not be able to
# see, retrieve, or act on each other's connected sources or extracted content.
#
# Usage:
#   HAI_BASE_URL=http://localhost:8080 HAI_JWT_SECRET=devsecret \
#     scripts/two-account-isolation-test.sh
#
# Exits non-zero on the first failed assertion.
set -uo pipefail

BASE="${HAI_BASE_URL:-http://localhost:8080}"
SECRET="${HAI_JWT_SECRET:-devsecret}"
SECRET_TEXT="45000 EUR"
fails=0

pass() { printf '  \033[32mPASS\033[0m %s\n' "$1"; }
fail() { printf '  \033[31mFAIL\033[0m %s\n' "$1"; fails=$((fails+1)); }
check() { [ "$2" = "$3" ] && pass "$1 ($2)" || fail "$1: got '$2', want '$3'"; }

mkjwt() {
  python3 -c "
import hmac,hashlib,base64,json,time,sys
def b64(b): return base64.urlsafe_b64encode(b).rstrip(b'=').decode()
h=b64(b'{\"alg\":\"HS256\",\"typ\":\"JWT\"}')
p=b64(json.dumps({'sub':sys.argv[1],'role':'owner','exp':int(time.time())+3600}).encode())
s=b64(hmac.new(sys.argv[2].encode(),f'{h}.{p}'.encode(),hashlib.sha256).digest())
print(f'{h}.{p}.{s}')" "$1" "$SECRET"
}

jlen() { python3 -c "
import sys,json
try:
    d=json.load(sys.stdin); r=d if isinstance(d,list) else (d.get('data') or [])
    print(len(r))
except Exception: print('ERR')"; }

# Unique owners per run so repeated runs stay independent.
STAMP="$(date +%s)"
ALICE="alice-${STAMP}@local"; BOB="bob-${STAMP}@local"
A="$(mkjwt "$ALICE")"; B="$(mkjwt "$BOB")"

echo "Two-account isolation test against $BASE"
echo "  owners: $ALICE / $BOB"
echo

mksrc() {
  curl -s -X POST "$BASE/api/v1/sources/" -H "Authorization: Bearer $1" -H 'Content-Type: application/json' \
    -d "{\"connectorKey\":\"local-folder\",\"name\":\"$2\",\"category\":\"local_folder\",\"enabled\":true,\"localOnly\":true,\"syncFrequency\":\"manual\",\"syncTarget\":\".\",\"defaultProjectKey\":\"$3\"}" \
    | python3 -c "import sys,json;d=json.load(sys.stdin);print((d.get('data') or d).get('id',''))"
}
AID="$(mksrc "$A" "Alice board $STAMP" "alice-$STAMP")"
BID="$(mksrc "$B" "Bob board $STAMP" "bob-$STAMP")"
[ -n "$AID" ] && [ -n "$BID" ] || { echo "could not create sources; is the stack running?"; exit 2; }

# Give Alice private content to protect. The result must prove that source
# extraction also reaches the governed pursuit/workflow intake boundary.
source_sync="$(curl -s -X POST "$BASE/api/v1/sources/$AID/sync" -H "Authorization: Bearer $A" -H 'Content-Type: application/json' \
  -d "{\"mode\":\"manual_import\",\"items\":[{\"externalId\":\"alice-secret-$STAMP\",\"title\":\"Alice confidential brief\",\"content\":\"Follow up: Alice client quote is $SECRET_TEXT, decision due Friday.\",\"sourceUri\":\"file://alice/brief.md\",\"itemType\":\"note\",\"projectKey\":\"alice-$STAMP\"}]}" )"
check "source sync persisted an extraction" \
  "$(printf '%s' "$source_sync" | python3 -c "import json,sys; print(len(json.load(sys.stdin).get('extractions') or []))")" "1"
check "source sync creates a governed pursuit/workflow outcome" \
  "$(printf '%s' "$source_sync" | python3 -c "import json,sys; outcomes=json.load(sys.stdin).get('pursuitOutcomes') or []; allowed={'candidate_pending','pursuit_routed','workflow_created','pursuit_linked'}; print(str(len(outcomes) == 1 and outcomes[0].get('status') in allowed).lower())")" "true"

echo "1. Source listing is owner-scoped"
check "alice sees exactly 1 source" "$(curl -s -H "Authorization: Bearer $A" "$BASE/api/v1/sources/" | jlen)" "1"
check "bob sees exactly 1 source"   "$(curl -s -H "Authorization: Bearer $B" "$BASE/api/v1/sources/" | jlen)" "1"

echo "2. Bob cannot act on Alice's source by id (no insecure direct object reference)"
for act in pause sync revoke; do
  code="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/sources/$AID/$act" \
    -H "Authorization: Bearer $B" -H 'Content-Type: application/json' -d '{}')"
  # 404 (not 403) is correct: it must not confirm the resource exists.
  check "bob -> alice/$act denied" "$code" "404"
done

echo "3. Extracted content is owner-scoped"
alice_sees="$(curl -s -H "Authorization: Bearer $A" "$BASE/api/v1/sources/extractions" | grep -c "$SECRET_TEXT")"
bob_sees="$(curl -s -H "Authorization: Bearer $B" "$BASE/api/v1/sources/extractions" | grep -c "$SECRET_TEXT")"
[ "$alice_sees" -gt 0 ] && pass "alice can read her own extraction" || fail "alice cannot read her own extraction"
check "bob cannot read alice's extraction" "$bob_sees" "0"

echo "4. Grounded search cannot retrieve another owner's content"
bob_hit="$(curl -s -X POST "$BASE/api/v1/sources/search" -H "Authorization: Bearer $B" -H 'Content-Type: application/json' \
  -d '{"query":"client quote decision Friday"}' | grep -c "$SECRET_TEXT")"
check "bob search returns nothing of alice's" "$bob_hit" "0"

echo "5. Operational history is owner-scoped"
check "bob sees no sync jobs" "$(curl -s -H "Authorization: Bearer $B" "$BASE/api/v1/sources/sync-jobs" | jlen)" "0"

echo
if [ "$fails" -eq 0 ]; then
  echo "RESULT: PASS — two authenticated owners are fully isolated."
  exit 0
fi
echo "RESULT: FAIL — $fails assertion(s) failed."
exit 1
