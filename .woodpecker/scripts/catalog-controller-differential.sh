#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root

# This receipt attests a differential run against a live controller. Generated
# from a dirty tree it attests a comparison of code that is in no commit.
# shellcheck source=.woodpecker/scripts/tree-state.sh
source "${repository_root}/.woodpecker/scripts/tree-state.sh"
evidence_tree_state "the controller differential receipt"
readonly inventory=${CATALOG_EVIDENCE_INVENTORY:-${repository_root}/build/release-ready/catalog-evidence-inventory.json}
readonly campaign_policy=${CATALOG_CAMPAIGN_POLICY:-${repository_root}/provider-codegen/policy/catalog-campaign.json}
readonly waves=${CATALOG_ACCEPTANCE_WAVES:-1,2,3,4}
readonly output=${CATALOG_ACCEPTANCE_OUTPUT:-${repository_root}/build/release-ready/controller-differential.json}

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-differential.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

plan_path=${work_root}/plan.json
# The campaign policy is read here, not only in the pass below, because the
# shared scenario owner set depends on it. A surface whose runtime CHANGED may
# still graft its scenario onto the released tree, but only if the policy
# declares the exception and says why -- the port action is the one such case.
# That exception used to be hardcoded in this expression as well as declared in
# the policy, which is one fact with two homes and the shape of drift this
# campaign has been bitten by repeatedly.
jq --arg waves "${waves}" --slurpfile campaign "${campaign_policy}" '
  def acceptance_tests:
    if .kind == "managed_resource" then
      [.test_functions[] | select(startswith("TestAcc") and (contains("List") | not))]
    elif .kind == "list_resource" then
      [.test_functions[] | select(startswith("TestAcc") and contains("List"))]
    else
      [.test_functions[] | select(startswith("TestAcc"))]
    end;
  ($waves | split(",") | map(tonumber)) as $selected |
  [.surfaces[] | select(.wave as $wave | $selected | index($wave)) |
    {kind, name, wave, missing_signals, test_names: acceptance_tests}] as $surfaces |
  ($campaign[0].shared_scenario_exceptions // []) as $exceptions |
  [.surfaces[] |
    select(.wave as $wave | $selected | index($wave)) |
    . as $surface |
    select($surface.runtime.status == "identical" or
           ([$exceptions[] |
             select(.kind == $surface.kind and .name == $surface.name)] |
            length) > 0) |
    .scenario_owner] | unique as $shared_scenario_owners |
  {
    format_version: 1,
    gate: "catalog controller differential",
    waves: $selected,
    surfaces: $surfaces,
    surface_count: ($surfaces | length),
    evidence_gap_count: ([$surfaces[].missing_signals[]] | length),
    shared_scenario_owners: $shared_scenario_owners,
    test_names: ([$surfaces[].test_names[]] | unique)
  }
' "${inventory}" >"${plan_path}"

# The dispositions come from the committed campaign policy so the plan
# builder, the followup gate, and Go admission cannot drift apart.
jq --slurpfile policy "${campaign_policy}" '
  . as $plan |
  $policy[0] as $campaign |
  .allowed_skips = ($campaign.allowed_skips |
    map(. as $skip | select($plan.test_names | index($skip) != null))) |
  .released_allowed_failures = ($campaign.released_allowed_failures |
    map(. as $failure | select($plan.test_names | index($failure) != null))) |
  .released_allowed_missing = ($campaign.released_allowed_missing |
    map(. as $missing | select($plan.test_names | index($missing) != null)))
' "${plan_path}" >"${plan_path}.allowed"
mv "${plan_path}.allowed" "${plan_path}"

jq -e '.surface_count > 0 and (.test_names | length) > 0' "${plan_path}" >/dev/null

if [[ -n ${CATALOG_ACCEPTANCE_TEST_NAMES:-} ]]; then
    catalog_test_count=$(jq '.test_names | length' "${plan_path}")
    requested_test_names=${CATALOG_ACCEPTANCE_TEST_NAMES//;/,}
    IFS=',' read -r -a requested_tests <<<"${requested_test_names}"
    for requested_test in "${requested_tests[@]}"; do
        if ! jq -e --arg requested_test "${requested_test}" \
            '.test_names | index($requested_test) != null' "${plan_path}" >/dev/null; then
            echo "requested diagnostic test is not in the selected catalog: ${requested_test}" >&2
            exit 1
        fi
    done

    requested_json=$(printf '%s\n' "${requested_tests[@]}" | jq -Rsc 'split("\n") | map(select(length > 0)) | unique')
    jq --argjson requested "${requested_json}" \
       --argjson catalog_test_count "${catalog_test_count}" '
      .diagnostic_selection = true |
      .catalog_test_count = $catalog_test_count |
      .test_names = $requested |
      .allowed_skips = [.allowed_skips[] | select(. as $skip | $requested | index($skip))] |
      .released_allowed_failures = [
        .released_allowed_failures[] |
        select(. as $failure | $requested | index($failure))
      ] |
      .released_allowed_missing = [
        .released_allowed_missing[] |
        select(. as $missing | $requested | index($missing))
      ]
    ' "${plan_path}" >"${plan_path}.targeted"
    mv "${plan_path}.targeted" "${plan_path}"
fi

if [[ ${CATALOG_ACCEPTANCE_PLAN_ONLY:-} == true ]]; then
    cp "${plan_path}" "${output}"
    exit 0
fi

readonly controller_image=${CATALOG_CONTROLLER_IMAGE:?CATALOG_CONTROLLER_IMAGE is required}
readonly synthetic_image=${CATALOG_SYNTHETIC_IMAGE:?CATALOG_SYNTHETIC_IMAGE is required}
readonly ryuk_image=${CATALOG_RYUK_IMAGE:?CATALOG_RYUK_IMAGE is required}
readonly herder_bin=${CATALOG_HERDER_BIN:?CATALOG_HERDER_BIN is required}
readonly terraform_bin=${TERRAFORM_BIN:?TERRAFORM_BIN is required}
readonly released_ref=${CATALOG_RELEASED_REF:-v0.101.2}
readonly candidate_ref=${CI_COMMIT_SHA:-$(git -C "${repository_root}" rev-parse HEAD)}

test "$(uname -s)" = Linux
test "$(uname -m)" = x86_64
test -x "${herder_bin}"
test -x "${terraform_bin}"
docker image inspect "${controller_image}" >/dev/null
docker image inspect "${synthetic_image}" >/dev/null
docker image inspect "${ryuk_image}" >/dev/null
git -C "${repository_root}" cat-file -e "${released_ref}^{commit}"
git -C "${repository_root}" cat-file -e "${candidate_ref}^{commit}"

released_root=${work_root}/released
mkdir -p "${released_root}"
git -C "${repository_root}" archive "${released_ref}" | tar -xf - -C "${released_root}"

# The fixture is campaign infrastructure, not provider runtime. Use the same
# digest-aware harness for both sides. Source-identical runtime paths also use
# the same candidate scenario owner, so newly added coverage exercises both
# implementations instead of appearing as a missing released test. A changed
# runtime path retains its released test owner and needs separate migration
# evidence.
rm -rf "${released_root}/internal/controllertest"
cp -R "${repository_root}/internal/controllertest" "${released_root}/internal/controllertest"
cp "${repository_root}/docker-compose.yaml" "${released_root}/docker-compose.yaml"
while IFS= read -r scenario_owner; do
    cp "${repository_root}/${scenario_owner}" "${released_root}/${scenario_owner}"
done < <(jq -r '.shared_scenario_owners[]' "${plan_path}")

test_regex=$(jq -r '.test_names | join("|")' "${plan_path}")
readonly test_regex

run_suite() {
    local label=$1
    local source_root=$2
    local log=${work_root}/${label}.jsonl
    local status_file=${work_root}/${label}.status
    local cache=${work_root}/${label}-go-cache

    mkdir -p "${cache}"
    set +e
    (
        cd "${source_root}"
        env \
            TF_ACC=1 \
            TF_ACC_TERRAFORM_PATH="${terraform_bin}" \
            UNIFI_TEST_HERDER_BIN="${herder_bin}" \
            UNIFI_TEST_HERDER_SYNTHETIC_IMAGE="${synthetic_image}" \
            UNIFI_TEST_CONTROLLER_IMAGE="${controller_image}" \
            UNIFI_TEST_CONTROLLER_PULL_POLICY=never \
            TESTCONTAINERS_RYUK_CONTAINER_IMAGE="${ryuk_image}" \
            GOPROXY=off GOSUMDB=off 'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 \
            GOTOOLCHAIN=local GOCACHE="${cache}" \
            go test -json -count=1 -timeout 90m ./unifi \
                -run "^(${test_regex})(/.*)?$" >"${log}" 2>&1
    )
    printf '%s\n' "$?" >"${status_file}"
    set -e

    jq --slurpfile plan "${plan_path}" \
       --arg suite_label "${label}" \
       --argjson exit_code "$(cat "${status_file}")" \
       -s -f "${repository_root}/.woodpecker/scripts/catalog-controller-summary.jq" \
       "${log}" >"${work_root}/${label}-summary.json"

    if [[ ${CATALOG_PRINT_FAILURE_DIAGNOSTICS:-false} == true ]]; then
        while IFS= read -r failed_test; do
            echo "sanitized controller diagnostic: ${label}/${failed_test}"
            bash "${repository_root}/.woodpecker/scripts/catalog-controller-diagnostics.sh" \
                "${failed_test}" <"${log}"
        done < <(jq -r '.failed[]' "${work_root}/${label}-summary.json")
    fi
}

run_suite candidate "${repository_root}"
run_suite released "${released_root}"

mkdir -p "$(dirname "${output}")"
controller_id=$(docker image inspect --format '{{.Id}}' "${controller_image}")
synthetic_id=$(docker image inspect --format '{{.Id}}' "${synthetic_image}")
ryuk_id=$(docker image inspect --format '{{.Id}}' "${ryuk_image}")
herder_sha256=$(sha256sum "${herder_bin}" | awk '{print $1}')
terraform_sha256=$(sha256sum "${terraform_bin}" | awk '{print $1}')
released_commit=$(git -C "${repository_root}" rev-parse "${released_ref}^{commit}")
plan_sha256=$(sha256sum "${plan_path}" | awk '{print $1}')

jq -n --slurpfile plan "${plan_path}" \
   --slurpfile released "${work_root}/released-summary.json" \
   --slurpfile candidate "${work_root}/candidate-summary.json" \
   --arg released_commit "${released_commit}" \
   --arg candidate_commit "${candidate_ref}" \
   --arg controller_image "${controller_image}" \
   --arg controller_id "${controller_id}" \
   --arg synthetic_image "${synthetic_image}" \
   --arg synthetic_id "${synthetic_id}" \
   --arg ryuk_image "${ryuk_image}" \
   --arg ryuk_id "${ryuk_id}" \
   --arg herder_sha256 "${herder_sha256}" \
   --arg terraform_sha256 "${terraform_sha256}" \
   --arg plan_sha256 "${plan_sha256}" '
  {
    format_version: 1,
    gate: "catalog controller differential",
    result: (if ($released[0].result == "pass" or
                 $released[0].result == "accepted_limitation") and
                $candidate[0].result == "pass"
             then (if $plan[0].evidence_gap_count == 0 then "pass" else "blocked_evidence" end)
             else "fail" end),
    plan_sha256: $plan_sha256,
    released_commit: $released_commit,
    candidate_commit: $candidate_commit,
    target: {image: $controller_image, image_id: $controller_id, pull_policy: "never"},
    fleet: {image: $synthetic_image, image_id: $synthetic_id, herder_sha256: $herder_sha256},
    testcontainers: {ryuk_image: $ryuk_image, ryuk_image_id: $ryuk_id},
    terraform_binary_sha256: $terraform_sha256,
    plan: $plan[0],
    released: $released[0],
    candidate: $candidate[0]
  }
' >"${output}"

test -s "${output}"
jq -e 'type == "object"' "${output}" >/dev/null
jq '.' "${output}"
jq -e '
  (
    (.released.result == "pass" and
     .released.accepted_failures == [] and
     .released.unexpected_failures == [] and
     .released.failed == [] and
     .released.missing == []) or
    (.released.result == "accepted_limitation" and
     .released.accepted_failures == .released.failed and
     (.released.failed - .plan.released_allowed_failures) == [] and
     .released.unexpected_failures == [] and
     .released.missing == .plan.released_allowed_missing)
  ) and
  .candidate.result == "pass" and
  .candidate.accepted_failures == [] and
  .candidate.unexpected_failures == [] and
  .candidate.failed == [] and
  .candidate.missing == []
' "${output}" >/dev/null
if [[ ${CATALOG_REQUIRE_COMPLETE:-false} == true ]]; then
    jq -e '.result == "pass"' "${output}" >/dev/null
fi
