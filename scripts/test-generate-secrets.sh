#!/usr/bin/env bash
set -euo pipefail

repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
generator="$repository_root/scripts/generate-secrets.sh"
readme="$repository_root/README.md"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
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

grep -F 'FIRST_RUN_ADMIN_EMAIL=you@example.com ./scripts/generate-secrets.sh' "$readme" >/dev/null
grep -F 'pre/0109_trello_webhook_reconciliation_generations' "$readme" >/dev/null
grep -F 'migration runner' "$readme" >/dev/null
grep -F 'remains unproven release evidence' "$readme" >/dev/null

printf 'generate-secrets and README checks passed\n'
