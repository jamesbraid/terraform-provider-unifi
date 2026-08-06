#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-evidence-test.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

output=${work_root}/catalog-evidence-inventory.json
CATALOG_EVIDENCE_OUTPUT=${output} \
    bash "${repository_root}/.woodpecker/scripts/catalog-evidence-inventory.sh"

test "$(jq -r '.surfaces | length' "${output}")" = 67
test "$(jq -r '.coverage_counts.scenario_owner' "${output}")" = 67
test "$(jq -r '.coverage_counts.action_acceptance' "${output}")" = 1
cmp "${repository_root}/build/release-ready/catalog-evidence-inventory.json" "${output}"
