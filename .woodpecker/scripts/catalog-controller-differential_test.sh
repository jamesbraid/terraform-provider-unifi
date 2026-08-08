#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-controller-differential.sh
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-differential-test.XXXXXX")
trap 'rm -rf "${work_root}"' EXIT

cd "${work_root}"
if ! bash "${repository_root}/.woodpecker/scripts/catalog-controller-diagnostics_test.sh"; then
    echo "controller diagnostic redaction self-test failed" >&2
    exit 1
fi
if ! bash "${repository_root}/.woodpecker/scripts/catalog-controller-summary_test.sh"; then
    echo "controller summary self-test failed" >&2
    exit 1
fi
CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_TEST_NAMES='' \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/plan.json" \
    "${script}"

if ! jq -e '
  .format_version == 1 and
  .gate == "catalog controller differential" and
  .waves == [1, 2, 3, 4, 5] and
  .surface_count == 67 and
  .evidence_gap_count == 8 and
  .allowed_skips == [
    "TestAccSettingResource_dohCustomServers",
    "TestAccSettingResource_ipsHoneypot",
    "TestAccWLANList_basic"
  ] and
  .released_allowed_failures == ["TestAccDeviceFramework_basic"] and
  .released_allowed_missing == [
    "TestAccDeviceList_basic",
    "TestAccFirewallZoneFramework_basic",
    "TestAccFirewallZoneList_emptyOrSeeded",
    "TestAccPortAction_persistsPoeOverride"
  ] and
  (.test_names | length) == 152 and
  (.shared_scenario_owners | length) == 37 and
  ([.surfaces[] | select(.name == "unifi_port" and .kind == "action" and .missing_signals == ["hardware_claim"])] | length) == 1 and
  ([.surfaces[] | select(.name == "unifi_dns_record" and .kind == "managed_resource")] | length) == 1
' "${work_root}/plan.json" >/dev/null; then
    echo "full controller plan self-test failed" >&2
    jq '.' "${work_root}/plan.json" >&2
    exit 1
fi

CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_TEST_NAMES=TestAccDeviceFramework_basic \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/targeted-plan.json" \
    "${script}"

if ! jq -e '
  .diagnostic_selection == true and
  .test_names == ["TestAccDeviceFramework_basic"] and
  .allowed_skips == [] and
  .released_allowed_failures == ["TestAccDeviceFramework_basic"] and
  .released_allowed_missing == [] and
  .catalog_test_count == 152
' "${work_root}/targeted-plan.json" >/dev/null; then
    echo "targeted controller plan self-test failed" >&2
    jq '.' "${work_root}/targeted-plan.json" >&2
    exit 1
fi

# Woodpecker CLI uses commas to separate repeated --param values. Operators
# therefore pass multi-test diagnostic selections with semicolons; the plan
# must preserve both requested tests as one environment value.
CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_TEST_NAMES='TestAccDeviceList_basic;TestAccDeviceFramework_basic' \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/targeted-multiple-plan.json" \
    "${script}"

if ! jq -e '
  .diagnostic_selection == true and
  .test_names == ["TestAccDeviceFramework_basic", "TestAccDeviceList_basic"] and
  .allowed_skips == [] and
  .released_allowed_failures == ["TestAccDeviceFramework_basic"] and
  .released_allowed_missing == ["TestAccDeviceList_basic"] and
  .catalog_test_count == 152
' "${work_root}/targeted-multiple-plan.json" >/dev/null; then
    echo "semicolon-separated controller selection did not preserve every test" >&2
    jq '.' "${work_root}/targeted-multiple-plan.json" >&2
    exit 1
fi

followup_script=${repository_root}/.woodpecker/scripts/catalog-controller-followup.sh
jq -n --slurpfile plan "${work_root}/plan.json" '
  {
    result: "blocked_evidence",
    plan: $plan[0],
    released: {
      result: "accepted_limitation",
      failed: ["TestAccDeviceFramework_basic"],
      accepted_failures: ["TestAccDeviceFramework_basic"],
      unexpected_failures: [],
      missing: [
        "TestAccDeviceList_basic",
        "TestAccFirewallZoneFramework_basic",
        "TestAccFirewallZoneList_emptyOrSeeded",
        "TestAccPortAction_persistsPoeOverride"
      ]
    },
    candidate: {
      result: "pass",
      failed: [],
      accepted_failures: [],
      unexpected_failures: [],
      missing: []
    }
  }
' >"${work_root}/full-receipt.json"
if [[ $(bash "${followup_script}" "${work_root}/full-receipt.json") != full ]]; then
    echo "full controller receipt did not continue to downstream evidence" >&2
    exit 1
fi

jq '
  .released.failed = [] |
  .released.accepted_failures = []
' "${work_root}/full-receipt.json" >"${work_root}/allowed-failure-passed-receipt.json"
if [[ $(bash "${followup_script}" "${work_root}/allowed-failure-passed-receipt.json") != full ]]; then
    echo "full controller receipt rejected an allowed failure that passed" >&2
    exit 1
fi

jq '.released.accepted_failures = ["TestAccDeviceFramework_basic", "TestAccUnexpected"]' \
    "${work_root}/full-receipt.json" >"${work_root}/invalid-limitation-receipt.json"
if bash "${followup_script}" "${work_root}/invalid-limitation-receipt.json" >/dev/null 2>&1; then
    echo "full controller receipt accepted an unexpected released limitation" >&2
    exit 1
fi

jq '.released.missing = ["TestAccDeviceList_basic", "TestAccUnexpected"]' \
    "${work_root}/full-receipt.json" >"${work_root}/invalid-missing-receipt.json"
if bash "${followup_script}" "${work_root}/invalid-missing-receipt.json" >/dev/null 2>&1; then
    echo "full controller receipt accepted an unexpected released missing test" >&2
    exit 1
fi

jq '
  .released.result = "pass" |
  .released.failed = ["TestAccUnexpected"] |
  .released.accepted_failures = []
' "${work_root}/full-receipt.json" >"${work_root}/invalid-released-pass.json"
if bash "${followup_script}" "${work_root}/invalid-released-pass.json" >/dev/null 2>&1; then
    echo "full controller receipt accepted a released pass with a failed test" >&2
    exit 1
fi

jq -n --slurpfile plan "${work_root}/targeted-plan.json" '
  {
    result: "blocked_evidence",
    plan: $plan[0],
    released: {result: "pass"},
    candidate: {result: "pass"}
  }
' >"${work_root}/targeted-receipt.json"
if [[ $(bash "${followup_script}" "${work_root}/targeted-receipt.json") != diagnostic_complete ]]; then
    echo "targeted controller receipt was not terminated before full-plan evidence" >&2
    exit 1
fi

if CATALOG_ACCEPTANCE_PLAN_ONLY=true \
   CATALOG_ACCEPTANCE_TEST_NAMES=TestAccNotInCatalog \
   CATALOG_ACCEPTANCE_OUTPUT="${work_root}/invalid-plan.json" \
       "${script}" 2>"${work_root}/invalid-plan.stderr"; then
    echo "controller differential accepted a test outside the catalog" >&2
    exit 1
fi
if ! grep -F 'requested diagnostic test is not in the selected catalog: TestAccNotInCatalog' \
    "${work_root}/invalid-plan.stderr" >/dev/null; then
    echo "invalid controller test selection did not report its rejected name" >&2
    sed -n '1,40p' "${work_root}/invalid-plan.stderr" >&2
    exit 1
fi

if grep -n 'github\.com/.*/releases\|github\.com/.*/archive\|git@github\.com' "${script}"; then
    echo "controller differential contains a GitHub acquisition path" >&2
    exit 1
fi
