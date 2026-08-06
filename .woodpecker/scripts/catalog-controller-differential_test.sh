#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-controller-differential.sh
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-differential-test.XXXXXX")
trap 'rm -rf "${work_root}"' EXIT

cd "${work_root}"
CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4 \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/plan.json" \
    "${script}"

jq -e '
  .format_version == 1 and
  .gate == "catalog controller differential" and
  .waves == [1, 2, 3, 4] and
  .surface_count == 66 and
  .evidence_gap_count > 0 and
  (.test_names | length) == 138 and
  ([.surfaces[] | select(.name == "unifi_dns_record" and .kind == "managed_resource")] | length) == 1
' "${work_root}/plan.json" >/dev/null

if grep -n 'github\.com/.*/releases\|github\.com/.*/archive\|git@github\.com' "${script}"; then
    echo "controller differential contains a GitHub acquisition path" >&2
    exit 1
fi
