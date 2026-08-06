#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-controller-differential.sh
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-differential-test.XXXXXX")
trap 'rm -rf "${work_root}"' EXIT

cd "${work_root}"
bash "${repository_root}/.woodpecker/scripts/catalog-controller-diagnostics_test.sh"
CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/plan.json" \
    "${script}"

jq -e '
  .format_version == 1 and
  .gate == "catalog controller differential" and
  .waves == [1, 2, 3, 4, 5] and
  .surface_count == 67 and
  .evidence_gap_count == 10 and
  .allowed_skips == [
    "TestAccSettingResource_dohCustomServers",
    "TestAccSettingResource_ipsHoneypot",
    "TestAccWLANList_basic"
  ] and
  (.test_names | length) == 150 and
  (.shared_scenario_owners | length) == 40 and
  ([.surfaces[] | select(.name == "unifi_port" and .kind == "action" and .missing_signals == ["hardware_claim"])] | length) == 1 and
  ([.surfaces[] | select(.name == "unifi_dns_record" and .kind == "managed_resource")] | length) == 1
' "${work_root}/plan.json" >/dev/null

CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_TEST_NAMES=TestAccDeviceFramework_basic \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/targeted-plan.json" \
    "${script}"

jq -e '
  .diagnostic_selection == true and
  .test_names == ["TestAccDeviceFramework_basic"] and
  .allowed_skips == [] and
  .catalog_test_count == 150
' "${work_root}/targeted-plan.json" >/dev/null

if CATALOG_ACCEPTANCE_PLAN_ONLY=true \
   CATALOG_ACCEPTANCE_TEST_NAMES=TestAccNotInCatalog \
   CATALOG_ACCEPTANCE_OUTPUT="${work_root}/invalid-plan.json" \
       "${script}" 2>"${work_root}/invalid-plan.stderr"; then
    echo "controller differential accepted a test outside the catalog" >&2
    exit 1
fi
grep -F 'requested diagnostic test is not in the selected catalog: TestAccNotInCatalog' \
    "${work_root}/invalid-plan.stderr" >/dev/null

if grep -n 'github\.com/.*/releases\|github\.com/.*/archive\|git@github\.com' "${script}"; then
    echo "controller differential contains a GitHub acquisition path" >&2
    exit 1
fi
