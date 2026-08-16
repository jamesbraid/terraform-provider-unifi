#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root

# Reads the baseline manifest and the evidence inventory and writes its own
# receipt, so a dirty run makes it disagree with two artifacts it did not
# produce -- which reads as their fault rather than its own.
# shellcheck source=.woodpecker/scripts/tree-state.sh
source "${repository_root}/.woodpecker/scripts/tree-state.sh"
evidence_tree_state "the unit differential receipt"
readonly output=${CATALOG_UNIT_OUTPUT:?CATALOG_UNIT_OUTPUT is required}
readonly baseline_manifest=${repository_root}/build/m0/provider-baseline.json
readonly inventory=${repository_root}/build/release-ready/catalog-evidence-inventory.json
readonly released_ref=${CATALOG_RELEASED_REF:-v0.101.2}

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-unit-differential.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

sha256_file() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    else
        shasum -a 256 "$1" | awk '{print $1}'
    fi
}

released_commit=$(jq -r .provider.released_commit "${baseline_manifest}")
readonly released_commit
test "$(git -C "${repository_root}" rev-parse "${released_ref}^{commit}")" = "${released_commit}"

released_root=${work_root}/released
mkdir -p "${released_root}"
git -C "${repository_root}" archive --format=tar "${released_ref}" | \
    tar -xf - -C "${released_root}"

run_suite() {
    local label=$1
    local source_root=$2
    local log=${work_root}/${label}.jsonl
    local cache=${work_root}/${label}-go-cache
    local exit_code

    mkdir -p "${cache}"
    set +e
    (
        cd "${source_root}"
        env -u TF_ACC \
            GOPROXY=off GOSUMDB=off 'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 \
            GOTOOLCHAIN=local GOCACHE="${cache}" \
            go test -json -count=1 ./...
    ) >"${log}" 2>&1
    exit_code=$?
    set -e

    jq -R -s --argjson exit_code "${exit_code}" '
      (split("\n") | map(select(length > 0))) as $lines |
      ($lines | map(try fromjson catch null)) as $events |
      ($events | map(select(. != null))) as $parsed |
      ($parsed |
        map(select(.Action == "pass" or .Action == "fail" or .Action == "skip") |
          {package: .Package, test: (.Test // null), action: .Action}) |
        unique_by([.package, .test, .action]) |
        sort_by([.package, .test, .action])) as $normalized |
      {
        exit_code: $exit_code,
        # A FLOOR ON THE MEASUREMENT, not only on the failures.
        #
        # The three conditions below it all hold over an empty run: exit code
        # zero, no failing events, and every line parsed because there were no
        # lines. Measured by feeding this filter empty input -- it returns
        # result "pass" with package_pass_count 0. A `go test ./...` that
        # produced nothing was therefore indistinguishable from a full green
        # suite, and this receipt is consumed by catalog admission.
        #
        # catalog-unit-differential_test.sh already asserts package_pass_count
        # is above zero. That file is invoked by no pipeline and no script, so
        # the one assertion standing between us and a hollow pass lived
        # somewhere nothing runs. It belongs in the thing that always runs.
        result: (if $exit_code == 0 and
                    ([$normalized[] | select(.action == "fail")] | length) == 0 and
                    ($lines | length) == ($parsed | length) and
                    ([$normalized[] |
                      select(.test == null and .action == "pass")] | length) > 0
                 then "pass" else "fail" end),
        unparsed_line_count: (($lines | length) - ($parsed | length)),
        package_pass_count: ([$normalized[] |
          select(.test == null and .action == "pass")] | length),
        package_fail_count: ([$normalized[] |
          select(.test == null and .action == "fail")] | length),
        passed_test_count: ([$normalized[] |
          select(.test != null and .action == "pass")] | length),
        skipped_test_count: ([$normalized[] |
          select(.test != null and .action == "skip")] | length),
        failed_test_count: ([$normalized[] |
          select(.test != null and .action == "fail")] | length),
        normalized: $normalized
      }
    ' "${log}" >"${work_root}/${label}-summary-with-events.json"

    jq 'del(.normalized)' "${work_root}/${label}-summary-with-events.json" \
        >"${work_root}/${label}-summary.json"
    jq --sort-keys --compact-output '.normalized' \
        "${work_root}/${label}-summary-with-events.json" \
        >"${work_root}/${label}-normalized.json"
}

run_suite released "${released_root}"
run_suite candidate "${repository_root}"

go_version=$(go env GOVERSION)
platform=$(go env GOOS)/$(go env GOARCH)
readonly go_version platform
promotion_blockers=()
[[ ${go_version} == "$(jq -r .toolchain.go_version "${baseline_manifest}")" ]] || \
    promotion_blockers+=(go_version)
[[ ${platform} == "$(jq -r .provider.platform "${baseline_manifest}")" ]] || \
    promotion_blockers+=(platform)
promotion_blockers_json=$(jq --null-input --compact-output --args \
    '$ARGS.positional' "${promotion_blockers[@]}")
readonly promotion_blockers_json

released_result=$(jq -r .result "${work_root}/released-summary.json")
candidate_result=$(jq -r .result "${work_root}/candidate-summary.json")
readonly released_result candidate_result
result=pass
if [[ ${released_result} != pass || ${candidate_result} != pass ]]; then
    result=fail
elif (( ${#promotion_blockers[@]} > 0 )); then
    if [[ ${CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN:-false} != true ]]; then
        printf 'catalog unit differential promotion blockers: %s\n' \
            "${promotion_blockers[*]}" >&2
        exit 1
    fi
    result=diagnostic_pass
fi
readonly result

mkdir -p "$(dirname -- "${output}")"
jq --indent 2 --null-input \
    --arg result "${result}" \
    --argjson promotion_blockers "${promotion_blockers_json}" \
    --arg source_commit "$(git -C "${repository_root}" rev-parse HEAD)" \
    --argjson tree_state "$(evidence_tree_json)" \
    --arg released_commit "${released_commit}" \
    --arg platform "${platform}" \
    --arg go_version "${go_version}" \
    --arg inventory_sha256 "$(sha256_file "${inventory}")" \
    --slurpfile released "${work_root}/released-summary.json" \
    --slurpfile candidate "${work_root}/candidate-summary.json" \
    --arg released_raw_log_sha256 "$(sha256_file "${work_root}/released.jsonl")" \
    --arg candidate_raw_log_sha256 "$(sha256_file "${work_root}/candidate.jsonl")" \
    --arg released_normalized_sha256 "$(sha256_file "${work_root}/released-normalized.json")" \
    --arg candidate_normalized_sha256 "$(sha256_file "${work_root}/candidate-normalized.json")" '
      {
        format_version: 1,
        gate: "catalog-unit-http-differential",
        result: $result,
        promotion_blockers: $promotion_blockers,
        network: "none",
        source_commit: $source_commit,
        # source_commit alone can be a commit this receipt does not describe.
        # tree_state says whether it does.
        tree_state: $tree_state,
        released_commit: $released_commit,
        platform: $platform,
        go_version: $go_version,
        catalog_evidence_inventory_sha256: $inventory_sha256,
        released: ($released[0] + {
          raw_log_sha256: $released_raw_log_sha256,
          normalized_summary_sha256: $released_normalized_sha256
        }),
        candidate: ($candidate[0] + {
          raw_log_sha256: $candidate_raw_log_sha256,
          normalized_summary_sha256: $candidate_normalized_sha256
        })
      }
    ' >"${output}"

test -s "${output}"
jq -e 'type == "object"' "${output}" >/dev/null
jq '.' "${output}"
jq -e '.released.result == "pass" and .candidate.result == "pass"' \
    "${output}" >/dev/null
