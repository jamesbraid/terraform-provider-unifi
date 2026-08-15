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

# released_allowed_missing is one uniform fact restated per name, not thirteen
# judgements. A test that exists only in the candidate tree cannot run against
# the released provider unless its scenario is grafted, and the graft rule
# refuses to lend a scenario whose surface's runtime changed. Every surface
# behind these names EXISTS at the released ref -- what is candidate-only is the
# TEST, not the thing it tests -- so this list registers acceptance coverage the
# released provider never had, not surfaces it lacks.
#
# It is therefore checked against TWO derivations that share no input: one reads
# the committed inventory, the other reads the git tag. A stale inventory cannot
# satisfy the check on its own. That is the property the shared-scenario guard
# above lacks, which is why that one has to be mutation-proven through the plan
# rather than through the policy.
readonly released_ref=${CATALOG_RELEASED_REF:-v0.101.2}

# grafted_scenarios prints every scenario name whose surface the policy excepts,
# because those DO run on the released tree and so are not missing from it.
grafted_scenarios() {
    jq -r --slurpfile policy "$1" '
      ($policy[0].shared_scenario_exceptions // []) as $exceptions |
      [.surfaces[] | . as $surface |
        select([$exceptions[] |
                select(.kind == $surface.kind and .name == $surface.name)] |
               length > 0) |
        ($surface.scenarios // [])[] | .name] | unique | .[]' "$2"
}

# derived_from_inventory reads the committed inventory: a scenario recorded
# "added" exists in the candidate tree and not in the released one.
derived_from_inventory() {
    local grafted
    grafted=$(grafted_scenarios "$1" "$2")
    jq -r '[.surfaces[] | (.scenarios // [])[] |
            select(.status == "added") | .name] | unique | .[]' "$2" |
        grep -vxF -f <(printf '%s\n' "${grafted}" | grep -v '^$' || true) 2>/dev/null |
        sort || true
}

# derived_from_git measures the same set against the released tag itself. A
# planned test with no func definition there cannot run there, whatever any
# committed artifact claims.
derived_from_git() {
    local grafted name
    grafted=$(grafted_scenarios "$1" "$2")
    while IFS= read -r name; do
        [[ -n ${name} ]] || continue
        printf '%s\n' "${grafted}" | grep -qxF "${name}" && continue
        git -C "${repository_root}" grep -q "func ${name}(" "${released_ref}" \
            -- '*_test.go' 2>/dev/null && continue
        printf '%s\n' "${name}"
    done < <(jq -r '.test_names[]' "$3") | sort
}

# missing_declaration_disagreements prints every way the declaration and the two
# derivations fail to agree, naming names in both directions. Silence is a pass.
missing_declaration_disagreements() {
    local declaration=$1 measured=$2 plan_file=$3
    local declared derived_inventory derived_git
    declared=$(jq -r '.released_allowed_missing[]' "${declaration}" | sort)
    derived_inventory=$(derived_from_inventory "${declaration}" "${measured}")
    derived_git=$(derived_from_git "${declaration}" "${measured}" "${plan_file}")

    if [[ -z ${derived_git} ]]; then
        echo "no candidate-only test was derived from the released tag, so this check cannot fail"
        return
    fi
    if [[ ${derived_inventory} != "${derived_git}" ]]; then
        comm -23 <(printf '%s\n' "${derived_inventory}") <(printf '%s\n' "${derived_git}") |
            sed 's/^/  inventory says candidate-only, the released tag has it: /'
        comm -13 <(printf '%s\n' "${derived_inventory}") <(printf '%s\n' "${derived_git}") |
            sed 's/^/  absent from the released tag, inventory does not record it: /'
        echo "  the committed inventory is stale; regenerate it before trusting either side"
        return
    fi
    comm -23 <(printf '%s\n' "${declared}") <(printf '%s\n' "${derived_inventory}") |
        sed 's/^/  declared missing, but it can run on the released tree: /'
    comm -13 <(printf '%s\n' "${declared}") <(printf '%s\n' "${derived_inventory}") |
        sed 's/^/  candidate-only and undeclared, so the gate will fail on it: /'
}

if [[ -n $(missing_declaration_disagreements \
    "${campaign_policy}" "${inventory}" "${work_root}/plan.json") ]]; then
    echo "released_allowed_missing does not match what cannot run on the released tree:" >&2
    missing_declaration_disagreements \
        "${campaign_policy}" "${inventory}" "${work_root}/plan.json" >&2
    exit 1
fi

# Prove it can fail, four ways, because a check nobody has watched fail is a
# check nobody should believe.
#
# 1: a declared name that CAN run on the released tree must be named.
jq '.released_allowed_missing += ["TestAccDeviceFramework_basic"] |
    .released_allowed_missing |= unique' \
    "${campaign_policy}" >"${work_root}/policy-overdeclared.json"
if [[ -z $(missing_declaration_disagreements \
    "${work_root}/policy-overdeclared.json" "${inventory}" "${work_root}/plan.json") ]]; then
    echo "the declaration guard accepted a name that runs on the released tree" >&2
    exit 1
fi

# 2: a candidate-only test left undeclared must be named.
jq '.released_allowed_missing |= .[1:]' \
    "${campaign_policy}" >"${work_root}/policy-underdeclared.json"
if [[ -z $(missing_declaration_disagreements \
    "${work_root}/policy-underdeclared.json" "${inventory}" "${work_root}/plan.json") ]]; then
    echo "the declaration guard accepted a candidate-only test with no declaration" >&2
    exit 1
fi

# 3: emptying shared_scenario_exceptions grows the derived set while the
# declaration stays put, so the exception is proven to be doing work rather than
# merely being written down.
jq '.shared_scenario_exceptions = []' \
    "${campaign_policy}" >"${work_root}/policy-no-exceptions.json"
if [[ -z $(missing_declaration_disagreements \
    "${work_root}/policy-no-exceptions.json" "${inventory}" "${work_root}/plan.json") ]]; then
    echo "the declaration guard did not notice the shared-scenario exception being removed" >&2
    exit 1
fi

# 4: a stale inventory must be caught by the tag rather than believed. Dropping
# a scenario's "added" status is exactly what a not-regenerated inventory does.
jq '.surfaces |= map(.scenarios |= ((. // []) |
      map(if .status == "added" then .status = "identical" else . end)))' \
    "${inventory}" >"${work_root}/inventory-stale.json"
if [[ -z $(missing_declaration_disagreements \
    "${campaign_policy}" "${work_root}/inventory-stale.json" "${work_root}/plan.json") ]]; then
    echo "the declaration guard believed a stale inventory over the released tag" >&2
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
