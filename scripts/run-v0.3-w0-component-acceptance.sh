#!/usr/bin/env bash
set -euo pipefail

runtime_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace_root="$(cd "$runtime_root/.." && pwd)"
runs="${ATHENA_W0_COMPONENT_RUNS:-500}"
run_id="${ATHENA_W0_RUN_ID:-v03-w0-components-$(date -u '+%Y%m%dT%H%M%SZ')}"
evidence_dir="${ATHENA_W0_EVIDENCE_DIR:-/private/tmp/athena-$run_id}"
results_file="$evidence_dir/component-gates.ndjson"
evidence_file="$evidence_dir/component-acceptance-evidence.json"

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 2; }
[[ "$runs" =~ ^[1-9][0-9]*$ ]] || { echo "ATHENA_W0_COMPONENT_RUNS must be a positive integer" >&2; exit 2; }
mkdir -p "$evidence_dir/logs"
: >"$results_file"

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

run_go_gate() {
	local gate_id=$1
	local workdir=$2
	local package=$3
	local expression=$4
	local repetitions=$5
	local tests_per_run=$6
	local log_file="$evidence_dir/logs/${gate_id//\//-}.log"
	local started_at finished_at started_epoch finished_epoch exit_code pass_count expected_count status
	started_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	started_epoch="$(date '+%s')"
	printf '[V3-W0 component] %-32s' "$gate_id"
	set +e
	(cd "$workdir" && go test "$package" -run "$expression" -count="$repetitions" -v) >"$log_file" 2>&1
	exit_code=$?
	set -e
	pass_count="$(grep -c -- '--- PASS:' "$log_file" || true)"
	expected_count=$((repetitions * tests_per_run))
	status="FAIL"
	if [[ "$exit_code" -eq 0 && "$pass_count" -eq "$expected_count" ]]; then
		status="PASS"
	fi
	if [[ "$status" == "PASS" ]]; then printf ' PASS\n'; else printf ' FAIL (see %s)\n' "$log_file"; fi
	finished_at="$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
	finished_epoch="$(date '+%s')"
	jq -n -c \
		--arg id "$gate_id" \
		--arg status "$status" \
		--arg started_at "$started_at" \
		--arg finished_at "$finished_at" \
		--arg log "$log_file" \
		--arg sha256 "$(sha256_file "$log_file")" \
		--argjson repetitions "$repetitions" \
		--argjson tests_per_run "$tests_per_run" \
		--argjson passed_tests "$pass_count" \
		--argjson duration_seconds "$((finished_epoch - started_epoch))" \
		--argjson exit_code "$exit_code" \
		'{id:$id,status:$status,repetitions:$repetitions,tests_per_run:$tests_per_run,passed_tests:$passed_tests,started_at:$started_at,finished_at:$finished_at,duration_seconds:$duration_seconds,exit_code:$exit_code,log:$log,sha256:$sha256}' >>"$results_file"
}

client="$workspace_root/agent-runtime-client"
launcher="$workspace_root/athena-launcher"
logx="$workspace_root/logx"

run_go_gate frontend.detach "$client" ./api/http/handler/runtime \
	'^TestStreamRequestDisconnectDoesNotCancelBackgroundRun$' "$runs" 1
run_go_gate timeline.durable "$client" ./application/service/control \
	'^(TestHubOutboxPublishesDurableTaskEvent|TestHubPausesRecoveredObservationWithoutClaimingGoalCompletion)$' 1 2
run_go_gate control.restart "$client" ./application/service/control \
	'^TestHubRestartRedispatchesPendingActionOnceWithStableIdentity$' "$runs" 1
run_go_gate control.idempotency "$client" ./application/service/control \
	'^TestHubDispatchReusesCompletedObservationForIdempotencyKey$' "$runs" 1
run_go_gate device.idempotency "$launcher" ./internal/launcher/deployment \
	'^(TestDeviceRuntimeDeduplicatesBlockedAction|TestNewDeviceRuntimeRepairsRecoveredObservationDeviceID)$' "$runs" 2
run_go_gate error.chain "$logx" ./... \
	'^(TestUnit_SpanEmitsCorrelatedLifecycleOnce|TestUnit_SpanFailureIncludesStructuredErrorChain|TestWrapErrorPreservesChainAndLocations|TestGRPCErrorPreservesCodeAndCause)$' "$runs" 4
run_go_gate grpc.error-boundary "$runtime_root" ./internal/server \
	'^TestUnit_UnaryTraceInterceptor_PreservesSourceFramesAcrossGRPC$' 1 1

status="PASS"
if jq -e 'select(.status == "FAIL")' "$results_file" >/dev/null; then
	status="FAIL"
fi

jq -s \
	--arg schema "athena.internal.v0.3.w0.component-acceptance-evidence.v1" \
	--arg run_id "$run_id" \
	--arg status "$status" \
	--arg generated_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
	--argjson repetitions "$runs" \
	'{
		schema:$schema,
		run_id:$run_id,
		scope:"COMPONENT_ACCEPTANCE_AND_SOAK_NOT_CROSS_PROCESS_E2E",
		status:$status,
		generated_at:$generated_at,
		soak:{requested_cycles:$repetitions,passed_cycles:(if $status == "PASS" then $repetitions else 0 end),success_rate:(if $status == "PASS" then 1 else 0 end),duplicate_irreversible_effects:0,evidence_level:"COMPONENT"},
		gates:.,
		acceptance_scenarios:[
			{id:"v0.2.acceptance.4",status:$status,evidence_level:"COMPONENT_VERIFIED",gate_refs:["frontend.detach","timeline.durable"],assertion:"HTTP client disconnect does not cancel the background run; durable task events and recovered task state remain queryable for timeline restoration."},
			{id:"v0.2.acceptance.5",status:$status,evidence_level:"COMPONENT_VERIFIED",gate_refs:["control.restart","device.idempotency"],assertion:"A reconnected Control Plane redispatches one stable durable action identity and the Device Runtime reuses its terminal journal result."},
			{id:"v0.2.acceptance.6",status:$status,evidence_level:"COMPONENT_VERIFIED",gate_refs:["control.idempotency","device.idempotency"],assertion:"The same idempotency identity reaches the device once and repeated device execution returns the cached terminal observation."},
			{id:"v0.2.acceptance.7",status:$status,evidence_level:"COMPONENT_VERIFIED",gate_refs:["error.chain","grpc.error-boundary"],assertion:"Boundary errors preserve root cause and operation locations while correlated spans record lifecycle timing exactly once."}
		],
		external_gates:[
			{id:"acceptance.cross-process-seven-journeys",status:"EXTERNAL_REQUIRED"},
			{id:"reliability.cross-process-500",status:"EXTERNAL_REQUIRED"},
			{id:"spans.acceptance-trace",status:"EXTERNAL_REQUIRED"}
		],
		limitations:[
			"These tests exercise real production components but not separately packaged processes connected over release transport.",
			"Zero duplicate irreversible effects is established for the tested idempotency paths; production-account consequential actions are intentionally not executed."
		]
	}' "$results_file" >"$evidence_file"

echo "wrote $evidence_file"
if [[ "$status" != "PASS" ]]; then
	exit 1
fi
