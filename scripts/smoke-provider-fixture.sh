#!/usr/bin/env bash
# Build and exercise the local-only provider compatibility fixture. This does
# not start HAI, download a model, or contact any provider.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
IMAGE="${HAI_PROVIDER_FIXTURE_IMAGE:-hai-provider-fixture-smoke}"
NAME="hai-provider-fixture-smoke-$$"

cleanup() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker build --target provider-fixture -t "$IMAGE" "$ROOT/backend"
# Docker does not publish container ports from an internal network on hosted
# runners. The fixture is a scratch image with no client tooling; its existing
# read-only filesystem, dropped capabilities, no-new-privileges, and resource
# limits remain the isolation boundary while the loopback-only port is tested.
docker run -d \
  --name "$NAME" \
  --read-only \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --pids-limit 32 \
  --memory 32m \
  --cpus 0.10 \
  -p 127.0.0.1:0:11434 \
  "$IMAGE" >/dev/null

# Git Bash otherwise rewrites the absolute in-container executable path into a
# Windows host path before Docker receives it. The environment flag is inert
# on Linux runners.
fixture_exec() {
  MSYS_NO_PATHCONV=1 docker exec "$NAME" "$@"
}

if python3 -c 'import sys' >/dev/null 2>&1; then
  python_bin=python3
elif python -c 'import sys' >/dev/null 2>&1; then
  python_bin=python
else
  echo "A working Python interpreter is required to validate fixture responses." >&2
  exit 1
fi

for _ in $(seq 1 20); do
  if fixture_exec /provider-fixture --healthcheck >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
fixture_exec /provider-fixture --healthcheck >/dev/null

binding="$(docker port "$NAME" 11434/tcp | head -n 1)"
port="${binding##*:}"
base="http://127.0.0.1:${port}"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"; cleanup' EXIT

curl --fail --silent --show-error --connect-timeout 2 --max-time 5 "$base/api/tags" >"$tmp_dir/ollama.json"
curl --fail --silent --show-error --connect-timeout 2 --max-time 5 "$base/v1/models" >"$tmp_dir/openai.json"

"$python_bin" - "$tmp_dir" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
ollama = json.loads((root / "ollama.json").read_text(encoding="utf-8"))
openai = json.loads((root / "openai.json").read_text(encoding="utf-8"))

assert ollama["models"][0]["name"] == "hai-fixture-ollama:latest"
assert openai["data"][0]["id"] == "hai-fixture-openai"
PY

echo "Provider fixture live HTTP contract passed."
