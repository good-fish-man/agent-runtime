#!/usr/bin/env bash
set -euo pipefail

runtime_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace_root="$(cd "$runtime_root/.." && pwd)"
run_id="${ATHENA_W0_RUN_ID:-v03-w0-$(date -u '+%Y%m%dT%H%M%SZ')}"
evidence_dir="${ATHENA_W0_EVIDENCE_DIR:-/private/tmp/athena-$run_id}"
results_file="$evidence_dir/gates.ndjson"
repositories_file="$evidence_dir/repositories.ndjson"
summary_file="$evidence_dir/local-evidence.json"
cache_root="${ATHENA_W0_CACHE_ROOT:-/private/tmp/athena-v03-w0-go-cache}"

mkdir -p "$evidence_dir/logs" "$cache_root"
: >"$results_file"
: >"$repositories_file"

if ! command -v jq >/dev/null 2>&1; then
	echo "jq is required to produce V3-W0 evidence" >&2
	exit 2
fi

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

run_gate() {
	local gate_id=$1
	local workdir=$2
	shift 2
	local log_file="$evidence_dir/logs/${gate_id//\//-}.log"
	local started_at finished_at started_epoch finished_epoch exit_code status digest
	started_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	started_epoch="$(date '+%s')"
	printf '[V3-W0] %-34s' "$gate_id"
	if (cd "$workdir" && "$@") >"$log_file" 2>&1; then
		exit_code=0
		status="PASS"
		printf ' PASS\n'
	else
		exit_code=$?
		status="FAIL"
		printf ' FAIL (see %s)\n' "$log_file"
	fi
	finished_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	finished_epoch="$(date '+%s')"
	digest="$(sha256_file "$log_file")"
	jq -n -c \
		--arg id "$gate_id" \
		--arg status "$status" \
		--arg started_at "$started_at" \
		--arg finished_at "$finished_at" \
		--arg log "$log_file" \
		--arg sha256 "$digest" \
		--argjson duration_seconds "$((finished_epoch - started_epoch))" \
		--argjson exit_code "$exit_code" \
		'{id:$id,status:$status,started_at:$started_at,finished_at:$finished_at,duration_seconds:$duration_seconds,exit_code:$exit_code,log:$log,sha256:$sha256}' >>"$results_file"
}

record_repository() {
	local name=$1
	local path=$2
	local commit branch dirty
	commit="$(git -C "$path" rev-parse HEAD)"
	branch="$(git -C "$path" branch --show-current)"
	if [[ -n "$(git -C "$path" status --porcelain)" ]]; then dirty=true; else dirty=false; fi
	jq -n -c --arg name "$name" --arg path "$path" --arg commit "$commit" --arg branch "$branch" --argjson dirty "$dirty" \
		'{name:$name,path:$path,commit:$commit,branch:$branch,dirty:$dirty}' >>"$repositories_file"
}

record_repository athena-protocol "$workspace_root/athena-protocol"
record_repository logx "$workspace_root/logx"
record_repository agent-runtime "$workspace_root/agent-runtime"
record_repository agent-runtime-client "$workspace_root/agent-runtime-client"
record_repository athena-launcher "$workspace_root/athena-launcher"
record_repository athena-agent-ui "$workspace_root/frontend/agent-ui"

run_gate audit.fixtures "$runtime_root" env GOCACHE="$cache_root/audit" ./scripts/test-v0.3-w0-auditors.sh
run_gate protocol.check "$workspace_root/athena-protocol" env GOCACHE="$cache_root/protocol" make check
run_gate protocol.vet "$workspace_root/athena-protocol" env GOCACHE="$cache_root/protocol" GOWORK=off go vet ./...
run_gate logx.test "$workspace_root/logx" env GOCACHE="$cache_root/logx" go test ./...
run_gate logx.vet "$workspace_root/logx" env GOCACHE="$cache_root/logx" go vet ./...
run_gate runtime.test "$runtime_root" env GOCACHE="$cache_root/runtime" go test ./...
run_gate runtime.vet "$runtime_root" env GOCACHE="$cache_root/runtime" go vet ./...
run_gate client.test "$workspace_root/agent-runtime-client" env GOCACHE="$cache_root/client" go test ./...
run_gate client.vet "$workspace_root/agent-runtime-client" env GOCACHE="$cache_root/client" go vet ./...
run_gate launcher.test "$workspace_root/athena-launcher" env GOCACHE="$cache_root/launcher" go test ./...
run_gate launcher.vet "$workspace_root/athena-launcher" env GOCACHE="$cache_root/launcher" go vet ./...
run_gate frontend.protocol "$workspace_root/frontend/agent-ui" npm run protocol:check
run_gate frontend.lint "$workspace_root/frontend/agent-ui" npm run lint
run_gate frontend.build "$workspace_root/frontend/agent-ui" npm run build

run_gate protocol.race "$workspace_root/athena-protocol" env GOCACHE="$cache_root/protocol-race" go test -race ./...
run_gate logx.race "$workspace_root/logx" env GOCACHE="$cache_root/logx-race" go test -race ./...
run_gate runtime.race "$runtime_root" env GOCACHE="$cache_root/runtime-race" go test -race ./internal/dispatcher ./internal/eino ./internal/effectspec/... ./internal/evidenceaudit
run_gate client.race "$workspace_root/agent-runtime-client" env GOCACHE="$cache_root/client-race" go test -race ./application/service/control ./application/service/runtime ./application/service/experience ./infra/repository/repo/control ./infra/repository/migration
run_gate launcher.race "$workspace_root/athena-launcher" env GOCACHE="$cache_root/launcher-race" go test -race ./internal/launcher/deployment ./internal/runtime-system/browser-runtime/...

if [[ -n "${ATHENA_W0_EVIDENCE_SOURCE:-}" ]]; then
	run_gate credential.external "$runtime_root" env GOCACHE="$cache_root/audit" go run ./cmd/v03-evidence-audit --format json "$ATHENA_W0_EVIDENCE_SOURCE"
fi

local_status="PASS"
if jq -e 'select(.status == "FAIL")' "$results_file" >/dev/null; then
	local_status="FAIL"
fi

jq -s \
	--slurpfile repositories "$repositories_file" \
	--arg schema "athena.internal.v0.3.w0.local-evidence.v1" \
	--arg run_id "$run_id" \
	--arg status "$local_status" \
	--arg generated_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
	--arg evidence_source "${ATHENA_W0_EVIDENCE_SOURCE:-}" \
	'. as $gates | {schema:$schema,run_id:$run_id,scope:"LOCAL_AUTOMATED_GATES_ONLY",status:$status,generated_at:$generated_at,repositories:$repositories,gates:$gates,external_gates:[
		{id:"database.production-like-rollback",status:"EXTERNAL_REQUIRED"},
		{id:"package.macos-signed-install-update",status:"EXTERNAL_REQUIRED"},
		{id:"package.windows-signed-install-update",status:"EXTERNAL_REQUIRED"},
		{id:"package.linux-signed-install-update",status:"EXTERNAL_REQUIRED"},
		{id:"acceptance.cross-process-seven-journeys",status:"EXTERNAL_REQUIRED"},
		{id:"reliability.cross-process-500",status:"EXTERNAL_REQUIRED"},
		{id:"spans.acceptance-trace",status:"EXTERNAL_REQUIRED"},
		{id:"credentials.release-corpus",status:([$gates[] | select(.id == "credential.external") | .status][0] // "NOT_RUN")}
	]}' "$results_file" >"$summary_file"

echo "V3-W0 local evidence: $summary_file"
if [[ "$local_status" != "PASS" ]]; then
	exit 1
fi
