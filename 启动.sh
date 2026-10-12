#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
if [[ ! -x ./bin/weavegrid ]]; then
  make build
fi
exec ./bin/weavegrid --addr 127.0.0.1:8731 --data data "$@"
