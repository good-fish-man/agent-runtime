#!/usr/bin/env bash
set -euo pipefail

local_evidence=${LOCAL_EVIDENCE:-/private/tmp/athena-v03-w0-local-20260817/local-evidence.json}
database_evidence=${DATABASE_EVIDENCE:-/private/tmp/athena-v03-w0-database-20260817/database-evidence.json}
package_evidence=${PACKAGE_EVIDENCE:-/private/tmp/athena-v03-w0-cross-package-evidence/cross-package-evidence.json}
browser_evidence=${BROWSER_EVIDENCE:-/private/tmp/athena-v03-w0-browser-20260817/browser-evidence.json}
component_evidence=${COMPONENT_EVIDENCE:-/private/tmp/athena-v03-w0-components-20260817/component-acceptance-evidence.json}
credential_evidence=${CREDENTIAL_EVIDENCE:-/private/tmp/athena-v03-w0-credential-20260817/credential-audit.json}
packaged_evidence=${PACKAGED_EVIDENCE:-/private/tmp/athena-v03-w0-packaged-final5-20260817/packaged-e2e-evidence.json}
output_dir=${OUTPUT_DIR:-/private/tmp/athena-v03-w0-final-20260817}
run_id=${RUN_ID:-v03-w0-final-20260817}
refs_file="$output_dir/evidence-refs.ndjson"
report_file="$output_dir/final-evidence-review.json"

command -v jq >/dev/null 2>&1 || { echo "jq is required" >&2; exit 2; }
mkdir -p "$output_dir"
: >"$refs_file"

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

require_evidence() {
	local path=$1
	local expression=$2
	local label=$3
	[[ -f "$path" ]] || { echo "missing $label evidence: $path" >&2; exit 1; }
	jq -e "$expression" "$path" >/dev/null || { echo "$label evidence failed validation: $path" >&2; exit 1; }
}

record_ref() {
	local id=$1
	local level=$2
	local path=$3
	local digest
	digest=$(sha256_file "$path")
	jq -n -c --arg id "$id" --arg level "$level" --arg path "$path" --arg sha256 "$digest" \
		'{id:$id,evidence_level:$level,path:$path,sha256:$sha256}' >>"$refs_file"
}

require_evidence "$local_evidence" '.status == "PASS" and ([.gates[].status] | all(. == "PASS"))' "local automated gate"
require_evidence "$database_evidence" '.result == "PASS" and ([.assertions[].status] | all(. == "PASS"))' "database rollback drill"
require_evidence "$package_evidence" '.status == "PASS" and (.assets | length) == 20 and ([.assertions[].status] | all(. == "PASS"))' "cross-package structure"
require_evidence "$browser_evidence" '.result == "PASS" and .runs.requested == 10 and .runs.passed == 10 and (.acceptance_scenarios | length) == 3 and ([.acceptance_scenarios[].status] | all(. == "PASS")) and ([.acceptance_scenarios[].evidence_level] | all(. == "REAL_BROWSER_E2E"))' "real browser"
require_evidence "$component_evidence" '.status == "PASS" and .soak.requested_cycles == 500 and .soak.passed_cycles == 500 and .soak.duplicate_irreversible_effects == 0 and (.acceptance_scenarios | length) == 4 and ([.acceptance_scenarios[].status] | all(. == "PASS"))' "component acceptance"
require_evidence "$credential_evidence" '.scanned_files > 0 and (.findings | length) == 0 and ((.errors // []) | length) == 0' "release credential audit"
require_evidence "$packaged_evidence" '.result == "PASS" and .scope == "LOCAL_PACKAGED_CROSS_PROCESS_DARWIN_ARM64" and (.acceptance_scenarios | length) == 7 and ([.acceptance_scenarios[].status] | all(. == "PASS")) and ([.acceptance_scenarios[] | select(.id == "v0.2.acceptance.5" and .evidence_level == "FAULT_INJECTION")] | length) == 1 and ([.acceptance_scenarios[] | select(.id == "v0.2.acceptance.7" and .evidence_level == "PACKAGED_CROSS_PROCESS")] | length) == 1' "packaged cross-process acceptance"

record_ref local.automated LOCAL_AUTOMATED "$local_evidence"
record_ref database.rollback PRODUCTION_LIKE_ISOLATED "$database_evidence"
record_ref package.structure CROSS_COMPILED_UNSIGNED "$package_evidence"
record_ref browser.acceptance-1-3 REAL_LOCAL_CDP_FIXTURE "$browser_evidence"
record_ref component.acceptance-4-7 COMPONENT_VERIFIED "$component_evidence"
record_ref credentials.release-corpus RELEASE_CORPUS "$credential_evidence"
record_ref packaged.acceptance-1-7 LOCAL_PACKAGED_CROSS_PROCESS_DARWIN_ARM64 "$packaged_evidence"

jq -s \
	--arg schema "athena.internal.v0.3.w0.final-evidence-review.v1" \
	--arg run_id "$run_id" \
	--arg generated_at "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
	'{
		schema:$schema,
		run_id:$run_id,
		generated_at:$generated_at,
		status:"PARTIAL_EXTERNAL_GATES_REQUIRED",
		release_ready:false,
		architecture:{semantic_baseline:"FROZEN",public_object_wire_storage_contract:"DRAFT_V0ALPHA_NOT_FROZEN",v0_4_allowed:false},
		workstreams:[
			{id:"V3-W0",status:"PARTIAL_EXTERNAL_GATES_REQUIRED",release_blocking:true},
			{id:"V3-W1",status:"ENGINEERING_COMPLETE",release_blocking:false},
			{id:"V3-W2",status:"ENGINEERING_COMPLETE",release_blocking:false},
			{id:"V3-W3",status:"ENGINEERING_COMPLETE_PRODUCTION_COVERAGE_PENDING",release_blocking:false},
			{id:"V3-W4",status:"ENGINEERING_COMPLETE",release_blocking:false},
			{id:"V3-W5",status:"ENGINEERING_REVIEW_COMPLETE_RELEASE_BLOCKED_BY_W0",release_blocking:false}
		],
		evidence_refs:.,
		closed_gates:[
			{id:"local.automated-suites",status:"PASS",evidence_ref:"local.automated"},
			{id:"database.production-like-backup-rollback",status:"PASS",evidence_ref:"database.rollback"},
			{id:"package.cross-platform-unsigned-structure",status:"PASS",evidence_ref:"package.structure"},
			{id:"acceptance.browser-scenarios-1-3",status:"PASS",evidence_ref:"browser.acceptance-1-3"},
			{id:"acceptance.component-scenarios-4-7",status:"PASS",evidence_ref:"component.acceptance-4-7"},
			{id:"reliability.component-500",status:"PASS",evidence_ref:"component.acceptance-4-7"},
			{id:"credentials.release-corpus",status:"PASS",evidence_ref:"credentials.release-corpus"},
			{id:"acceptance.packaged-cross-process-seven-journeys",status:"PASS",evidence_ref:"packaged.acceptance-1-7"}
		],
		external_gates:[
			{id:"package.macos-signed-notarized-install-update",status:"EXTERNAL_REQUIRED"},
			{id:"package.windows-authenticode-install-update",status:"EXTERNAL_REQUIRED"},
			{id:"package.linux-signed-appimage-install-update",status:"EXTERNAL_REQUIRED"},
			{id:"reliability.packaged-cross-process-500",status:"EXTERNAL_REQUIRED"},
			{id:"spans.real-acceptance-trace",status:"EXTERNAL_REQUIRED"},
			{id:"experience.terminal-task-production-coverage-95-percent",status:"EXTERNAL_REQUIRED"}
		],
		limitations:[
			"The packaged cross-process run uses real service and browser processes with deterministic local websites, not production websites.",
			"The packaged cross-process run covers local Darwin ARM64 only and does not replace signed installer smoke tests on every supported platform.",
			"The 500-cycle soak covers component idempotency and durability paths, not 500 complete packaged user journeys.",
			"The real CDP browser uses a deterministic local fixture and does not claim production-site compatibility.",
			"Unsigned structural cross-compilation does not satisfy platform signing or installer execution gates."
		],
		decision:"Keep V3-W0 open, retain completed V3-W1 through V3-W5 engineering evidence, do not enter v0.4, and do not freeze new public contracts."
	}' "$refs_file" >"$report_file"

echo "V3-W0 final evidence review: $report_file"
