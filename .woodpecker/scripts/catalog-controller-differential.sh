#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly inventory=${CATALOG_EVIDENCE_INVENTORY:-${repository_root}/build/release-ready/catalog-evidence-inventory.json}
readonly waves=${CATALOG_ACCEPTANCE_WAVES:-1,2,3,4}
readonly output=${CATALOG_ACCEPTANCE_OUTPUT:-${repository_root}/build/release-ready/controller-differential.json}

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-differential.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

plan_path=${work_root}/plan.json
jq --arg waves "${waves}" '
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
  {
    format_version: 1,
    gate: "catalog controller differential",
    waves: $selected,
    surfaces: $surfaces,
    surface_count: ($surfaces | length),
    evidence_gap_count: ([$surfaces[].missing_signals[]] | length),
    test_names: ([$surfaces[].test_names[]] | unique)
  }
' "${inventory}" >"${plan_path}"

jq -e '.surface_count > 0 and (.test_names | length) > 0' "${plan_path}" >/dev/null

if [[ ${CATALOG_ACCEPTANCE_PLAN_ONLY:-} == true ]]; then
    cp "${plan_path}" "${output}"
    exit 0
fi

readonly controller_image=${CATALOG_CONTROLLER_IMAGE:?CATALOG_CONTROLLER_IMAGE is required}
readonly synthetic_image=${CATALOG_SYNTHETIC_IMAGE:?CATALOG_SYNTHETIC_IMAGE is required}
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
docker image inspect testcontainers/ryuk:0.13.0 >/dev/null
git -C "${repository_root}" cat-file -e "${released_ref}^{commit}"
git -C "${repository_root}" cat-file -e "${candidate_ref}^{commit}"

released_root=${work_root}/released
mkdir -p "${released_root}"
git -C "${repository_root}" archive "${released_ref}" | tar -xf - -C "${released_root}"

# The fixture is campaign infrastructure, not provider runtime. Use the same
# digest-aware harness for both sides so only provider code and released tests
# differ. This also keeps the released teardown from evicting the target.
rm -rf "${released_root}/internal/controllertest"
cp -R "${repository_root}/internal/controllertest" "${released_root}/internal/controllertest"
cp "${repository_root}/docker-compose.yaml" "${released_root}/docker-compose.yaml"

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
            GOPROXY=off GOSUMDB=off 'GOVCS=*:off' GIT_TERMINAL_PROMPT=0 \
            GOTOOLCHAIN=local GOCACHE="${cache}" \
            go test -json -count=1 -timeout 90m ./unifi \
                -run "^(${test_regex})(/.*)?$" >"${log}" 2>&1
    )
    printf '%s\n' "$?" >"${status_file}"
    set -e

    jq --slurpfile plan "${plan_path}" --argjson exit_code "$(cat "${status_file}")" -s '
      ($plan[0].test_names) as $planned |
      ([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
        select(.Action == "pass") | .Test] | unique) as $passed |
      ([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
        select(.Action == "skip") | .Test] | unique) as $skipped |
      ([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
        select(.Action == "fail") | .Test] | unique) as $failed |
      {
        exit_code: $exit_code,
        result: (if $exit_code == 0 and ($skipped | length) == 0 and
                    ($failed | length) == 0 and ($passed | length) == ($planned | length)
                 then "pass" else "fail" end),
        passed: $passed,
        skipped: $skipped,
        failed: $failed,
        missing: ($planned - $passed - $skipped - $failed)
      }
    ' "${log}" >"${work_root}/${label}-summary.json"
}

run_suite candidate "${repository_root}"
run_suite released "${released_root}"

mkdir -p "$(dirname "${output}")"
controller_id=$(docker image inspect --format '{{.Id}}' "${controller_image}")
synthetic_id=$(docker image inspect --format '{{.Id}}' "${synthetic_image}")
herder_sha256=$(sha256sum "${herder_bin}" | awk '{print $1}')
terraform_sha256=$(sha256sum "${terraform_bin}" | awk '{print $1}')
released_commit=$(git -C "${repository_root}" rev-parse "${released_ref}^{commit}")
plan_sha256=$(sha256sum "${plan_path}" | awk '{print $1}')

jq --slurpfile plan "${plan_path}" \
   --slurpfile released "${work_root}/released-summary.json" \
   --slurpfile candidate "${work_root}/candidate-summary.json" \
   --arg released_commit "${released_commit}" \
   --arg candidate_commit "${candidate_ref}" \
   --arg controller_image "${controller_image}" \
   --arg controller_id "${controller_id}" \
   --arg synthetic_image "${synthetic_image}" \
   --arg synthetic_id "${synthetic_id}" \
   --arg herder_sha256 "${herder_sha256}" \
   --arg terraform_sha256 "${terraform_sha256}" \
   --arg plan_sha256 "${plan_sha256}" '
  {
    format_version: 1,
    gate: "catalog controller differential",
    result: (if $released[0].result == "pass" and $candidate[0].result == "pass"
             then (if $plan[0].evidence_gap_count == 0 then "pass" else "blocked_evidence" end)
             else "fail" end),
    plan_sha256: $plan_sha256,
    released_commit: $released_commit,
    candidate_commit: $candidate_commit,
    target: {image: $controller_image, image_id: $controller_id, pull_policy: "never"},
    fleet: {image: $synthetic_image, image_id: $synthetic_id, herder_sha256: $herder_sha256},
    terraform_binary_sha256: $terraform_sha256,
    plan: $plan[0],
    released: $released[0],
    candidate: $candidate[0]
  }
' </dev/null >"${output}"

jq '.' "${output}"
jq -e '.released.result == "pass" and .candidate.result == "pass"' "${output}" >/dev/null
if [[ ${CATALOG_REQUIRE_COMPLETE:-false} == true ]]; then
    jq -e '.result == "pass"' "${output}" >/dev/null
fi
