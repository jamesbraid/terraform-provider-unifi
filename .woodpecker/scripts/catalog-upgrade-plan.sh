#!/usr/bin/env bash
# Answers the one question the acceptance suite cannot express: does a
# configuration APPLIED BY THE PREVIOUS RELEASE still plan clean under this one?
#
# The acceptance harness compiles the provider INTO the test binary through
# ProtoV6ProviderFactories, so a run holds exactly one provider and can only
# ever ask "does this build create and then re-plan cleanly". That is a strictly
# easier question and it is not the one an operator faces on upgrade.
#
# THREE PLANS, AND THE MIDDLE ONE IS WHY THE RESULT MEANS ANYTHING.
#
#   apply with OLD                    the state must be written by the old binary
#   plan with OLD on that state       CONTROL   must report no changes
#   plan with NEW on that same state  SUBJECT   must report no changes
#
# Without the control a red subject is unreadable: a fixture that never settles
# under ANY build looks exactly like a regression the new build introduced. That
# is not theoretical here -- plain networks settle by themselves, which is why
# the fixture that reproduces is one attached to a firewall zone, chosen
# precisely because it does not settle easily. A green control is what converts
# a red subject into a finding.
#
# The two failures therefore carry DIFFERENT messages. One message for both
# would hand the reader a coin flip.
#
# THE CONTROLLER MUST ALREADY BE RUNNING. This script does not start one, on
# purpose: two agents starting controllers against one docker daemon is a
# recorded hazard, and a script that both provisions and measures has two
# reasons to fail with one exit code.
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root

# The receipt stamps candidate_commit from git rev-parse HEAD, so on a dirty
# tree it names a commit that does not contain what was measured. Take the tree
# state here, before anything is built or planned, so what the receipt records
# is the tree the run started from.
# The guard runs before anything it protects, and it REFUSES a dirty tree
# unless EVIDENCE_ALLOW_DIRTY_TREE acknowledges it -- in which case the dirt
# is recorded in the receipt rather than hidden. cmd/tree-state prints the
# JSON on stdout and the refusal on stderr, so a failure here stops the run
# whether or not anybody reads the message.
evidence_tree_json=$(cd "${repository_root}" && go run ./cmd/tree-state -what "the upgrade-plan receipt") || exit 1
readonly evidence_tree_json

readonly cli_bin=${TERRAFORM_BIN:?TERRAFORM_BIN is required (this repository sets it to an OpenTofu binary)}
readonly released_ref=${UPGRADE_RELEASED_REF:-v0.101.2}
readonly fixture_dir=${UPGRADE_FIXTURE:-${repository_root}/.woodpecker/fixtures/upgrade}
readonly output=${UPGRADE_OUTPUT:-${repository_root}/build/release-ready/catalog-upgrade-plan.json}

fail() {
    printf 'catalog-upgrade-plan: %s\n' "$*" >&2
    exit 1
}

test -x "${cli_bin}" || fail "TERRAFORM_BIN=${cli_bin} is not executable"
test -d "${fixture_dir}" || fail "fixture directory ${fixture_dir} does not exist"

# Name every missing variable rather than the first. An operator fixing them one
# per run is the same waste as a gate that reports one disagreeing count.
missing=()
for name in UNIFI_API UNIFI_USERNAME UNIFI_PASSWORD; do
    if [ -z "${!name:-}" ]; then
        missing+=("${name}")
    fi
done
if [ "${#missing[@]}" -ne 0 ]; then
    fail "the controller environment is incomplete: ${missing[*]} unset. This script measures a running controller and does not start one."
fi

fixture_count=$(find "${fixture_dir}" -maxdepth 1 -name '*.tf' -type f | wc -l | tr -d ' ')
readonly fixture_count
test "${fixture_count}" -gt 0 || fail "fixture directory ${fixture_dir} contains no .tf files"

# THE CONTROL'S EXPECTED VALUE IS A PROPERTY OF THE FIXTURE, AND IT HAS NO
# DEFAULT.
#
# Two different questions run through this same machinery and the control's
# polarity is what tells them apart:
#
#   is the upgrade safe   expect 0 -- the released provider settles the fixture,
#                         so a red subject is the candidate regressing
#   does a fix work       expect 2 -- the released provider demonstrably does NOT
#                         settle it, so a green subject is the fix working
#
# A fixture whose expectation is 0 when it should be 2 reports a fix as proven
# by a fixture that never showed the defect. Defaulting either way rebuilds the
# check that cannot fail one layer up, so an undeclared fixture is a hard error.
expectation_file=${fixture_dir}/EXPECT_OLD_PLAN
test -f "${expectation_file}" ||
    fail "${fixture_dir} does not declare EXPECT_OLD_PLAN. A fixture that does not say whether the released provider should settle it cannot judge anything; write 0 (upgrade fixture) or 2 (regression fixture)."
expect_old_plan=$(tr -d '[:space:]' <"${expectation_file}")
readonly expect_old_plan
case "${expect_old_plan}" in
    0 | 2) ;;
    *) fail "${expectation_file} says '${expect_old_plan}'; it must be 0 or 2" ;;
esac

work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-upgrade.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

# THE CLI IS RECORDED BY WHAT IT SAYS IT IS, NOT BY THE VARIABLE THAT HELD IT.
# The variable is called TERRAFORM_BIN and on this machine it holds OpenTofu.
# A receipt that reports "terraform" because of the variable's name is a receipt
# that lies about the tool that produced it.
cli_identity=$("${cli_bin}" version | head -1)
readonly cli_identity

released_commit=$(git -C "${repository_root}" rev-parse "${released_ref}^{commit}") ||
    fail "cannot resolve ${released_ref}; the released ref must exist locally"
readonly released_commit

# The released TREE is only extracted when it is going to be built. With a
# published binary in hand the archive is work nobody consumes, and a step that
# can fail without affecting the result is a step that can fail the run for no
# reason.
released_root=${work_root}/released
extract_released_tree() {
    mkdir -p "${released_root}"
    git -C "${repository_root}" archive "${released_commit}" | tar -xf - -C "${released_root}" ||
        fail "cannot extract the released tree at ${released_ref}"
}

# Each binary gets its own directory because dev_overrides addresses a
# DIRECTORY, and swapping which directory the override names is how the same
# configuration is planned by two different providers without re-initialising.
old_plugin_dir=${work_root}/plugins/old
new_plugin_dir=${work_root}/plugins/new
mkdir -p "${old_plugin_dir}" "${new_plugin_dir}"

build_provider() {
    local source_root=$1 destination=$2 label=$3
    ( cd "${source_root}" && go build -o "${destination}/terraform-provider-unifi" . ) ||
        fail "the ${label} provider did not build; nothing downstream of this can be measured"
    test -x "${destination}/terraform-provider-unifi" ||
        fail "the ${label} provider built without producing a binary at ${destination}"
}

# PREFER THE PUBLISHED BINARY OVER A REBUILD OF ITS SOURCE.
#
# An operator upgrades from the artifact that was released, not from a fresh
# compile of the tag it was cut at, and those are not guaranteed to be the same
# program -- different toolchain, different module resolution. The controller
# differential workflow already downloads the release zip with a pinned digest,
# so the faithful input exists and is reusable.
#
# Building from the archived tree stays as the fallback, because it is the only
# thing that works on a machine with no release artifact to hand, and a harness
# that can only run in CI is one nobody develops against.
released_provenance=source-build
if [ -n "${UPGRADE_RELEASED_BINARY:-}" ]; then
    test -x "${UPGRADE_RELEASED_BINARY}" ||
        fail "UPGRADE_RELEASED_BINARY=${UPGRADE_RELEASED_BINARY} is not executable"
    # dev_overrides resolves a directory and requires the binary inside it to be
    # named terraform-provider-<TYPE>. Release artifacts carry a version suffix,
    # so the copy is renamed rather than linked under its published name.
    cp "${UPGRADE_RELEASED_BINARY}" "${old_plugin_dir}/terraform-provider-unifi"
    chmod +x "${old_plugin_dir}/terraform-provider-unifi"
    released_provenance=published-binary
else
    extract_released_tree
    build_provider "${released_root}" "${old_plugin_dir}" "released (${released_ref})"
fi
readonly released_provenance
build_provider "${repository_root}" "${new_plugin_dir}" "candidate"

# dev_overrides silently uses whatever sits in the directory, so a stale binary
# is indistinguishable from a fresh one. accept nearly measured the wrong tree
# this way. If the two providers are byte-identical there is nothing to compare
# and a green means only that a program agrees with itself.
old_sha=$(sha256sum "${old_plugin_dir}/terraform-provider-unifi" | awk '{print $1}')
new_sha=$(sha256sum "${new_plugin_dir}/terraform-provider-unifi" | awk '{print $1}')
readonly old_sha new_sha
if [ "${old_sha}" = "${new_sha}" ]; then
    fail "the released and candidate providers are byte-identical (${old_sha}); this measurement would be void"
fi

write_cli_config() {
    local plugin_dir=$1 destination=$2
    cat >"${destination}" <<EOF
provider_installation {
  dev_overrides { "registry.terraform.io/ubiquiti-community/unifi" = "${plugin_dir}" }
  direct {}
}
EOF
}
old_cli_config=${work_root}/old.tfrc
new_cli_config=${work_root}/new.tfrc
write_cli_config "${old_plugin_dir}" "${old_cli_config}"
write_cli_config "${new_plugin_dir}" "${new_cli_config}"

config_dir=${work_root}/config
mkdir -p "${config_dir}"
cp "${fixture_dir}"/*.tf "${config_dir}/"

# EXIT CODES ARE READ DIRECTLY, NEVER THROUGH A PIPE. With -detailed-exitcode
# the exit status IS the result -- 0 no changes, 1 error, 2 changes -- and
# piping the command to tee or head reports the exit status of tee or head. A
# harness that pipes here reads 0 from a plan that never ran.
run_cli() {
    local cli_config=$1 log=$2
    shift 2
    local code=0
    set +e
    TF_CLI_CONFIG_FILE="${cli_config}" TF_IN_AUTOMATION=1 \
        "${cli_bin}" -chdir="${config_dir}" "$@" >"${log}" 2>&1
    code=$?
    set -e
    return "${code}"
}

apply_log=${work_root}/apply-old.log
control_log=${work_root}/plan-old.log
subject_log=${work_root}/plan-new.log

apply_code=0
run_cli "${old_cli_config}" "${apply_log}" apply -auto-approve -input=false || apply_code=$?
if [ "${apply_code}" -ne 0 ]; then
    sed 's/^/    /' "${apply_log}" >&2
    fail "the released provider could not apply the fixture (exit ${apply_code}); no state was written, so neither plan below would mean anything"
fi

test -s "${config_dir}/terraform.tfstate" ||
    fail "the apply reported success but wrote no state; the plans below would compare against nothing"

control_code=0
run_cli "${old_cli_config}" "${control_log}" plan -detailed-exitcode -input=false || control_code=$?

subject_code=0
run_cli "${new_cli_config}" "${subject_log}" plan -detailed-exitcode -input=false || subject_code=$?

# Best effort, and reported rather than hidden: a fixture left behind poisons the
# next run, and silence about it is how that becomes somebody else's mystery.
destroy_code=0
run_cli "${old_cli_config}" "${work_root}/destroy.log" destroy -auto-approve -input=false || destroy_code=$?

result=pass
verdict="a configuration applied by ${released_ref} plans clean under the candidate"
if [ "${control_code}" -eq 1 ]; then
    result=void
    verdict="the released provider could not plan its own state (exit 1); the subject result is meaningless"
elif [ "${control_code}" -ne "${expect_old_plan}" ]; then
    result=void
    if [ "${expect_old_plan}" -eq 0 ]; then
        verdict="the fixture does not settle under the released provider, so it cannot judge the candidate; fix the fixture, do not report a regression"
    else
        verdict="the fixture was declared to demonstrate a defect under the released provider and did not (control ${control_code}, expected ${expect_old_plan}); a clean subject would prove nothing"
    fi
elif [ "${subject_code}" -eq 1 ]; then
    result=fail
    verdict="the candidate provider could not plan state written by ${released_ref} (exit 1)"
elif [ "${subject_code}" -ne 0 ]; then
    result=fail
    verdict="the candidate provider does not settle on state written by ${released_ref}; this is an upgrade regression"
fi

mkdir -p "$(dirname "${output}")"
jq -n \
    --arg gate catalog-upgrade-plan \
    --arg cli "${cli_identity}" \
    --arg released_ref "${released_ref}" \
    --arg released_provenance "${released_provenance}" \
    --arg released_commit "${released_commit}" \
    --arg candidate_commit "$(git -C "${repository_root}" rev-parse HEAD)" \
    --arg old_sha256 "${old_sha}" \
    --arg new_sha256 "${new_sha}" \
    --argjson fixture_files "${fixture_count}" \
    --argjson control_exit "${control_code}" \
    --argjson subject_exit "${subject_code}" \
    --argjson destroy_exit "${destroy_code}" \
    --argjson expect_old_plan "${expect_old_plan}" \
    --arg result "${result}" \
    --arg verdict "${verdict}" \
    --argjson tree "${evidence_tree_json}" \
    '{format_version: 1, gate: $gate, cli: $cli, released_ref: $released_ref,
      released_commit: $released_commit, candidate_commit: $candidate_commit,
      tree: $tree,
      released_provenance: $released_provenance,
      released_provider_sha256: $old_sha256, candidate_provider_sha256: $new_sha256,
      fixture_files: $fixture_files, control_exit: $control_exit,
      subject_exit: $subject_exit, expected_control_exit: $expect_old_plan,
      destroy_exit: $destroy_exit,
      result: $result, verdict: $verdict}' >"${output}"

printf 'catalog-upgrade-plan: %s\n' "${verdict}"
printf 'catalog-upgrade-plan: baseline=%s (%s) provenance=%s\n' \
    "${released_ref}" "${released_commit}" "${released_provenance}"
printf 'catalog-upgrade-plan: cli=%s control=%d (expected %d) subject=%d receipt=%s\n' \
    "${cli_identity}" "${control_code}" "${expect_old_plan}" "${subject_code}" "${output}"

if [ "${destroy_code}" -ne 0 ]; then
    printf 'catalog-upgrade-plan: the fixture was NOT destroyed (exit %d); the controller holds leftover objects\n' \
        "${destroy_code}" >&2
fi

case "${result}" in
    pass) exit 0 ;;
    fail) sed 's/^/    /' "${subject_log}" >&2; exit 1 ;;
    *) sed 's/^/    /' "${control_log}" >&2; exit 2 ;;
esac
