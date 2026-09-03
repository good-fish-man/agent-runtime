#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
CACHE_ROOT="${ATHENA_GATE_CACHE_ROOT:-${TMPDIR:-/tmp}/athena-v1.0-gates}"

run() {
  local name="$1"
  shift
  printf '\n[v1.0] %s\n' "$name"
  "$@"
}

mkdir -p "$CACHE_ROOT"

run "frozen protocol, Golden Journey catalog, E2E validator, and compatibility matrix" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" test ./protocol/ga/v1 ./cmd/validate-ga-evidence
run "protocol freeze and generated artifact checks" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off make -C "$WORKSPACE_ROOT/athena-protocol" check
run "protocol static analysis" \
  env GOCACHE="$CACHE_ROOT/protocol" GOWORK=off go -C "$WORKSPACE_ROOT/athena-protocol" vet ./...

run "runtime GA readiness and production execution invariants" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test \
    ./internal/readiness ./internal/operations ./internal/provider ./internal/server
run "runtime regression tests" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" test ./...
run "runtime static analysis" \
  env GOCACHE="$CACHE_ROOT/runtime" go -C "$WORKSPACE_ROOT/agent-runtime" vet ./...

run "owner-scoped Golden evidence, trusted runner boundary, continuous provenance, recovery, and migrations" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test \
    ./application/service/operations ./api/http/handler/public/operations \
    ./api/http/router/public ./infra/repository/repo/operations ./infra/repository/migration
run "GA evidence and readiness race checks" \
  env GOCACHE="$CACHE_ROOT/client-race" go -C "$WORKSPACE_ROOT/agent-runtime-client" test -race ./application/service/operations
run "runtime-client regression tests" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" test ./...
run "runtime-client static analysis" \
  env GOCACHE="$CACHE_ROOT/client" go -C "$WORKSPACE_ROOT/agent-runtime-client" vet ./...

run "Launcher GA compatibility, readiness, signed update, and rollback invariants" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./internal/release ./internal/launcher/deployment
run "Launcher regression tests" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" test ./...
run "Launcher static analysis" \
  env GOCACHE="$CACHE_ROOT/launcher" go -C "$WORKSPACE_ROOT/athena-launcher" vet ./...

run "frontend frozen protocol synchronization" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run protocol:check
run "frontend type check" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run lint
run "frontend production build" npm --prefix "$WORKSPACE_ROOT/frontend/agent-ui" run build
run "GA architecture document" test -f "$WORKSPACE_ROOT/agent-runtime/doc/personal-agent-os-ga-v1.0.zh-CN.md"
run "GA engineering acceptance document" test -f "$WORKSPACE_ROOT/agent-runtime/doc/v1.0-engineering-acceptance.zh-CN.md"

printf '\n[v1.0] ENGINEERING_VERIFIED\n'
printf '[v1.0] GA_EXTERNAL_REQUIRED: engineering contracts pass, but a PASS E2E suite, signed platform release, security/privacy review, recovery drill, and SLO window are still mandatory.\n'
