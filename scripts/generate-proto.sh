#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"
protoc -I proto -I third_party/proto \
  --go_out=. --go_opt=module=github.com/good-fish-man/agent-runtime \
  --go-grpc_out=. --go-grpc_opt=module=github.com/good-fish-man/agent-runtime \
  proto/agent/runtime/v1/runtime.proto
