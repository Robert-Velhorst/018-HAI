#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="$repository_root/scripts/generate-secrets.sh"
readme="$repository_root/README.md"
temporary_parent="$(cd "${TMPDIR:-/tmp}" && pwd -P)"
tmp="$(mktemp -d "$temporary_parent/hai-secret-generator-test.XXXXXXXX")"
cleanup() {
  local resolved
  [[ -d "$tmp" && ! -L "$tmp" ]] || return 1
  resolved="$(cd "$tmp" && pwd -P)" || return 1
  [[ "$resolved" == "$tmp" ]] || return 1
  case "$resolved" in
    "$temporary_parent"/hai-secret-generator-test.????????) rm -rf -- "$resolved" ;;
    *) echo 'Refusing cleanup outside the owned secret-generator fixture.' >&2; return 1 ;;
  esac
}
trap cleanup EXIT
mkdir -p "$tmp/bin"

cat >"$tmp/bin/openssl" <<'OPENSSL'
#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 3 && "$1" == rand && "$2" == -hex && "$3" == 32 ]]
printf '%064d\n' 1
OPENSSL
chmod +x "$tmp/bin/openssl"

valid_output="$(PATH="$tmp/bin:$PATH" FIRST_RUN_ADMIN_EMAIL='  Robert.Velhorst+HAI@Example.COM  ' bash "$generator" 2>"$tmp/valid.err")"
grep -Fx 'FIRST_RUN_ADMIN_EMAIL=robert.velhorst+hai@example.com' <<<"$valid_output" >/dev/null
grep -Eq '^FIRST_RUN_ADMIN_PASSWORD=[0-9]{64}$' <<<"$valid_output"
grep -Eq '^JWT_SECRET=[0-9]{64}$' <<<"$valid_output"

if PATH="$tmp/bin:$PATH" FIRST_RUN_ADMIN_EMAIL='not-an-email' bash "$generator" >"$tmp/invalid.out" 2>"$tmp/invalid.err"; then
  echo "Expected invalid owner email to fail." >&2
  exit 1
fi
[[ ! -s "$tmp/invalid.out" ]]
grep -F 'FIRST_RUN_ADMIN_EMAIL must be a valid email address' "$tmp/invalid.err" >/dev/null

if env -u FIRST_RUN_ADMIN_EMAIL PATH="$tmp/bin:$PATH" bash "$generator" >"$tmp/missing.out" 2>"$tmp/missing.err"; then
  echo "Expected missing non-interactive owner email to fail." >&2
  exit 1
fi
[[ ! -s "$tmp/missing.out" ]]
grep -F 'Set FIRST_RUN_ADMIN_EMAIL or run this script interactively' "$tmp/missing.err" >/dev/null

mkdir -p "$tmp/failing-bin"
cat >"$tmp/failing-bin/openssl" <<'OPENSSL'
#!/usr/bin/env bash
exit 1
OPENSSL
chmod +x "$tmp/failing-bin/openssl"
if PATH="$tmp/failing-bin:$PATH" FIRST_RUN_ADMIN_EMAIL='owner@example.com' bash "$generator" >"$tmp/entropy-failure.out" 2>"$tmp/entropy-failure.err"; then
  echo "Expected secret generation failure to fail." >&2
  exit 1
fi
[[ ! -s "$tmp/entropy-failure.out" ]]

mkdir -p "$tmp/malformed-bin"
cat >"$tmp/malformed-bin/openssl" <<'OPENSSL'
#!/usr/bin/env bash
printf '%s' "$HAI_TEST_RANDOM_OUTPUT"
OPENSSL
chmod +x "$tmp/malformed-bin/openssl"
accepted_malformed=0
for kind in empty short nonhex multiline; do
  case "$kind" in
    empty) random_output='' ;;
    short) random_output='abc' ;;
    nonhex) random_output="$(printf '%064d' 1)"; random_output="z${random_output:1}" ;;
    multiline) random_output="$(printf '%064d' 1)"$'\nextra' ;;
  esac
  if PATH="$tmp/malformed-bin:$PATH" FIRST_RUN_ADMIN_EMAIL='owner@example.com' HAI_TEST_RANDOM_OUTPUT="$random_output" bash "$generator" >"$tmp/malformed-$kind.out" 2>"$tmp/malformed-$kind.err"; then
    accepted_malformed=$((accepted_malformed + 1))
    continue
  fi
  [[ ! -s "$tmp/malformed-$kind.out" ]]
  grep -F 'Secret generation did not return exactly 32 bytes of hexadecimal entropy.' "$tmp/malformed-$kind.err" >/dev/null
done
if [[ "$accepted_malformed" -ne 0 ]]; then
  echo "Malformed entropy output was accepted in $accepted_malformed cases." >&2
  exit 1
fi

grep -F 'FIRST_RUN_ADMIN_EMAIL=you@example.com ./scripts/generate-secrets.sh' "$readme" >/dev/null
[[ -f "$repository_root/backend/migrations/pre/0109_trello_webhook_reconciliation_generations.up.sql" ]]
[[ -f "$repository_root/backend/migrations/pre/0109_trello_webhook_reconciliation_generations.down.sql" ]]
grep -F 'Versioned SQL migrations are the schema source of truth' "$readme" >/dev/null
grep -F 'Clean-clone Windows 11 migration apply/rollback remains unproven.' "$readme" >/dev/null

printf 'generate-secrets and README checks passed\n'
