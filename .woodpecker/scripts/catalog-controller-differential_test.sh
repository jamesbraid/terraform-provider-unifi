#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-controller-differential.sh
readonly campaign_policy=${repository_root}/provider-codegen/policy/catalog-campaign.json
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

if ! jq -e --slurpfile policy "${campaign_policy}" '
  $policy[0] as $campaign |
  . as $plan |
  .format_version == 1 and
  .gate == $campaign.gate and
  .waves == [1, 2, 3, 4, 5] and
  .surface_count == $campaign.surface_count and
  .evidence_gap_count == $campaign.evidence_gap_count and
  .allowed_skips == $campaign.allowed_skips and
  .released_allowed_failures == $campaign.released_allowed_failures and
  .released_allowed_missing == $campaign.released_allowed_missing and
  (.test_names | length) == $campaign.test_name_count and
  (.shared_scenario_owners | length) > 0 and
  ([($campaign.shared_scenario_exceptions // [])[] as $exception |
    $plan.surfaces[] |
    select(.kind == $exception.kind and .name == $exception.name)] | length)
      == (($campaign.shared_scenario_exceptions // []) | length) and
  ([.surfaces[] | select(.name == "unifi_port" and .kind == "action" and .missing_signals == ["hardware_claim"])] | length) == 1 and
  ([.surfaces[] | select(.name == "unifi_dns_record" and .kind == "managed_resource")] | length) == 1
' "${work_root}/plan.json" >/dev/null; then
    echo "full controller plan self-test failed" >&2
    jq '.' "${work_root}/plan.json" >&2
    exit 1
fi

# The invariant a count could never express. Grafting the candidate's scenario
# onto the released tree is only sound where the runtime is source-identical,
# or where the policy declares an exception and says why. A surface that
# changed and is shared anyway runs new-schema tests against the old
# implementation and reports the result as agreement.
readonly inventory=${CATALOG_EVIDENCE_INVENTORY:-${repository_root}/build/release-ready/catalog-evidence-inventory.json}

# shared_scenarios_unjustified names every surface in the given plan that
# grafts its scenario onto the released tree while its runtime differs and no
# policy exception covers it. Silence means the set is sound.
shared_scenarios_unjustified() {
    jq -r --slurpfile plan "$1" --slurpfile policy "${campaign_policy}" '
      ($plan[0].shared_scenario_owners // []) as $shared |
      ($policy[0].shared_scenario_exceptions // []) as $exceptions |
      [.surfaces[] |
        . as $surface |
        select($shared | index($surface.scenario_owner)) |
        select($surface.runtime.status != "identical") |
        select([$exceptions[] |
                select(.kind == $surface.kind and .name == $surface.name)] |
               length == 0) |
        "\($surface.kind)/\($surface.name) via \($surface.scenario_owner)"] |
      unique | .[]' "${inventory}"
}

if [[ -n $(shared_scenarios_unjustified "${work_root}/plan.json") ]]; then
    echo "a changed-runtime surface shares its scenario without a declared exception:" >&2
    shared_scenarios_unjustified "${work_root}/plan.json" | sed 's/^/  /' >&2
    exit 1
fi

# Prove that check can fail, because it very nearly could not. The plan builder
# derives the shared set from the SAME policy this check reads, so emptying the
# exceptions shrinks both sides at once and the comparison passes vacuously --
# a detector whose two inputs share the error it exists to detect. Mutating the
# policy therefore proves nothing. Mutate the PLAN instead: graft a
# changed-runtime, undeclared surface's scenario in and require it to be named.
smuggled=$(jq -r --slurpfile policy "${campaign_policy}" '
  ($policy[0].shared_scenario_exceptions // []) as $exceptions |
  [.surfaces[] |
    . as $surface |
    select($surface.runtime.status != "identical") |
    select([$exceptions[] |
            select(.kind == $surface.kind and .name == $surface.name)] |
           length == 0) |
    $surface.scenario_owner] | unique | first // empty' "${inventory}")
if [[ -z ${smuggled} ]]; then
    echo "no changed-and-undeclared surface exists, so the scenario guard is untestable" >&2
    exit 1
fi
jq --arg smuggled "${smuggled}" \
   '.shared_scenario_owners = (.shared_scenario_owners + [$smuggled] | unique)' \
   "${work_root}/plan.json" >"${work_root}/smuggled-plan.json"
if [[ -z $(shared_scenarios_unjustified "${work_root}/smuggled-plan.json") ]]; then
    echo "the scenario guard accepted ${smuggled} grafted in without a declaration" >&2
    exit 1
fi

CATALOG_ACCEPTANCE_PLAN_ONLY=true \
CATALOG_ACCEPTANCE_WAVES=1,2,3,4,5 \
CATALOG_ACCEPTANCE_TEST_NAMES=TestAccDeviceFramework_basic \
CATALOG_ACCEPTANCE_OUTPUT="${work_root}/targeted-plan.json" \
    "${script}"

if ! jq -e --slurpfile policy "${campaign_policy}" '
  $policy[0] as $campaign |
  .diagnostic_selection == true and
  .test_names == ["TestAccDeviceFramework_basic"] and
  .allowed_skips == [] and
  .released_allowed_failures == ["TestAccDeviceFramework_basic"] and
  .released_allowed_missing == [] and
  .catalog_test_count == $campaign.test_name_count
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

if ! jq -e --slurpfile policy "${campaign_policy}" '
  $policy[0] as $campaign |
  .diagnostic_selection == true and
  .test_names == ["TestAccDeviceFramework_basic", "TestAccDeviceList_basic"] and
  .allowed_skips == [] and
  .released_allowed_failures == ["TestAccDeviceFramework_basic"] and
  .released_allowed_missing == ["TestAccDeviceList_basic"] and
  .catalog_test_count == $campaign.test_name_count
' "${work_root}/targeted-multiple-plan.json" >/dev/null; then
    echo "semicolon-separated controller selection did not preserve every test" >&2
    jq '.' "${work_root}/targeted-multiple-plan.json" >&2
    exit 1
fi

followup_script=${repository_root}/.woodpecker/scripts/catalog-controller-followup.sh
jq -n --slurpfile plan "${work_root}/plan.json" \
      --slurpfile policy "${campaign_policy}" '
  $policy[0] as $campaign |
  {
    result: "blocked_evidence",
    plan: $plan[0],
    released: {
      result: "accepted_limitation",
      failed: $campaign.released_allowed_failures,
      accepted_failures: $campaign.released_allowed_failures,
      unexpected_failures: [],
      missing: $campaign.released_allowed_missing
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
