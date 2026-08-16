#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-unit-differential-test.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

CATALOG_ALLOW_DIAGNOSTIC_TOOLCHAIN=true \
CATALOG_UNIT_OUTPUT="${work_root}/receipt.json" \
    bash "${repository_root}/.woodpecker/scripts/catalog-unit-differential.sh"

jq -e '
  .format_version == 1 and
  .gate == "catalog-unit-http-differential" and
  (.result == "pass" or .result == "diagnostic_pass") and
  .network == "none" and
  .released.result == "pass" and
  .candidate.result == "pass" and
  .released.failed_test_count == 0 and
  .candidate.failed_test_count == 0 and
  .released.package_pass_count > 0 and
  .candidate.package_pass_count > 0 and
  (.released.raw_log_sha256 | length) == 64 and
  (.candidate.raw_log_sha256 | length) == 64 and
  (.released.normalized_summary_sha256 | length) == 64 and
  (.candidate.normalized_summary_sha256 | length) == 64
' "${work_root}/receipt.json" >/dev/null

if grep -n 'github\.com/.*/releases\|github\.com/.*/archive\|git@github\.com' \
    "${repository_root}/.woodpecker/scripts/catalog-unit-differential.sh"; then
    echo "unit differential contains a GitHub acquisition path" >&2
    exit 1
fi
