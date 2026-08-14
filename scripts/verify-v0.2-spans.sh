#!/usr/bin/env bash
set -euo pipefail

log_source="${1:-${ATHENA_HOME:-$HOME/.athena}/logs}"
trace_filter="${2:-${ATHENA_TRACE_ID:-}}"
if [[ ! -e "$log_source" ]]; then
  echo "Athena log source does not exist: $log_source" >&2
  exit 2
fi

required_spans=(
  intent.parse
  route.plan
  world.query
  model.invoke
  action.policy
  action.dispatch
  device.execute
  perception.observe
  world.apply
  task.verify
)

log_files=()
if [[ -f "$log_source" ]]; then
  log_files+=("$log_source")
else
  while IFS= read -r -d '' log_file; do
    log_files+=("$log_file")
  done < <(find "$log_source" -type f \( -name '*.log' -o -name '*.log.[0-9]*' \) -print0)
fi

if (( ${#log_files[@]} == 0 )); then
  echo "No Athena .log or rotated .log.N files found in: $log_source" >&2
  exit 2
fi

required_csv="$(IFS=,; echo "${required_spans[*]}")"
if result=$(awk -v required="$required_csv" -v wanted_trace="$trace_filter" '
  BEGIN {
    required_count = split(required, span_names, ",")
  }

  /cost_ms=/ {
    trace_id = ""
    if (match($0, /\[[^]]+\]/)) {
      trace_id = substr($0, RSTART + 1, RLENGTH - 2)
    }
    if (trace_id == "" || (wanted_trace != "" && trace_id != wanted_trace)) {
      next
    }

    traces[trace_id] = 1
    for (i = 1; i <= required_count; i++) {
      if (index($0, "span_name=" span_names[i]) > 0) {
        seen[trace_id SUBSEP span_names[i]] = 1
      }
    }
  }

  END {
    best_trace = wanted_trace
    best_count = (wanted_trace == "" ? -1 : 0)
    for (trace_id in traces) {
      count = 0
      for (i = 1; i <= required_count; i++) {
        if (seen[trace_id SUBSEP span_names[i]]) {
          count++
        }
      }
      if (count == required_count) {
        print "complete\t" trace_id
        exit 0
      }
      if (count > best_count) {
        best_count = count
        best_trace = trace_id
      }
    }

    if (best_trace == "") {
      print "missing-trace"
      exit 1
    }

    missing = ""
    for (i = 1; i <= required_count; i++) {
      if (!seen[best_trace SUBSEP span_names[i]]) {
        missing = missing (missing == "" ? "" : ",") span_names[i]
      }
    }
    print "incomplete\t" best_trace "\t" best_count "\t" missing
    exit 1
  }
' "${log_files[@]}"); then
  verified_trace="${result#*$'\t'}"
  echo "All required v0.2 spans were observed with timing for Trace ID $verified_trace"
  exit 0
fi

case "$result" in
  missing-trace)
    echo "No completed trace-correlated v0.2 spans were found in: $log_source" >&2
    ;;
  incomplete$'\t'*)
    IFS=$'\t' read -r _ best_trace observed_count missing_csv <<< "$result"
    echo "Trace ID $best_trace contains $observed_count/${#required_spans[@]} required completed spans." >&2
    echo "Missing spans: ${missing_csv//,/, }" >&2
    ;;
  *)
    echo "Unable to verify v0.2 spans in: $log_source" >&2
    ;;
esac
exit 1
