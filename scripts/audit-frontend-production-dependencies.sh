#!/usr/bin/env bash
set -euo pipefail

exec "$(dirname "${BASH_SOURCE[0]}")/audit-npm-production-dependencies.sh" "frontend"
