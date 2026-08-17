#!/usr/bin/env bash
# Proves catalog-upgrade-plan.sh can fail, and fails DIFFERENTLY for each shape.
#
# The harness itself needs a controller and two real provider builds, so its
# failure paths would otherwise never execute until the day one of them mattered.
# A harness whose failure paths have never run is the defect it exists to catch,
# wearing a different hat.
#
# Both the CLI and the Go toolchain are stubbed, so every case here runs with no
# controller, no network and no provider build. What the stubs cannot prove is
# that the REAL tofu returns the exit codes this assumes; that needs the
# controller and is the one proof this file does not carry.
#
# WHAT HAS BEEN PROVEN AGAINST A REAL CONTROLLER, recorded here rather than only
# in the commits that measured it:
#
#   - A deliberately mis-declared fixture made the script exit 2 while `go run`
#     reported 1. That is why the workflow builds the runner and executes the
#     binary rather than using `go run`: under it a void fixture and an upgrade
#     regression collapse into one exit code, which is the coin flip the control
#     plan exists to prevent.
#   - The regression fixture measured control 2 (expected 2) and subject 0, so
#     the released provider genuinely does NOT settle that configuration and the
#     candidate does. A control of 0 would mean the fixture had stopped
#     demonstrating the defect. (bf59ccfd)
#   - EXPECT_OLD_PLAN has no default, and a fixture declaring nothing is refused,
#     because defaulting either way rebuilds the check that cannot fail one layer
#     up. (cdf7cc5e)
#
# THREE KNOWN HOLES IN THIS FILE, stated because a proof that hides its scope is
# worth less than one that states it. The receipt assertions in the pass,
# published_binary and source-build cases sit inside `if [ -f ... ]` with no else
# branch, so if the case that produces the receipt failed, four assertions vanish
# silently rather than failing. run_case never asserts that a receipt was written
# at all. And the old_build_fails case greps for the bare string "released",
# which at exit 1 is also emitted by three other branches.
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-upgrade-plan.sh

work=$(mktemp -d "${TMPDIR:-/tmp}/upgrade-selftest.XXXXXX")
readonly work
cleanup() {
    rm -rf "${work}"
}
trap cleanup EXIT

stub_dir=${work}/stub
mkdir -p "${stub_dir}"

# The Go stub writes a file where a binary was asked for, unless told to fail
# for a particular tree. Which tree it is building is decided by the working
# directory, because that is what the script varies.
#
# A STUB THAT SUCCEEDS PRODUCING NOTHING IS A CHECK THAT CANNOT FAIL WEARING A
# STUB'S CLOTHES. This used to end `if [ "$1" != "build" ]; then exit 0; fi`,
# and a stub named `go` is on PATH for every subcommand, not just the one it
# implements -- so it answered success, silently, to questions it had never been
# taught.
#
# It cost twelve cases and none of them named the cause.
# catalog-upgrade-plan.sh takes its tree state from `go run ./cmd/tree-state`,
# the stub swallowed the run and printed nothing, and `jq --argjson` reported
# invalid JSON -- naming the consumer, three steps from the stub that emptied
# the variable.
#
# `run` ANSWERS WITH A FIXED VALUE RATHER THAN THE REAL TOOLCHAIN. This suite
# tests upgrade-plan's logic, not tree-state's, so a well-formed constant is the
# right fidelity. The commit is deliberately forty zeros, so nothing downstream
# can mistake it for a tree that existed.
#
# Passing `run` through to the real toolchain also works and also passes; it was
# tried and replaced. It recompiles cmd/tree-state once per case, which is work
# this suite has no reason to do. No wall-clock figure is quoted for that,
# because two runs of THIS file on a shared machine measured 6.4s and 16.8s --
# the variance is larger than the difference, and an earlier draft of this
# comment cited a three-fold speedup that the second run refuted.
#
# ANY OTHER SUBCOMMAND IS A HARD ERROR, which is the half that stops this
# recurring. Answering `run` alone would fix the instance and leave the class:
# the next `go vet`, `go list` or `go mod` added to the script would be
# swallowed exactly as `run` was.
cat >"${stub_dir}/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
build) ;;
run)
    printf '{"status":"clean","commit":"%s","dirty_paths":[]}\n' 0000000000000000000000000000000000000000
    exit 0
    ;;
*)
    echo "stub go: no behaviour defined for \`go ${1:-}\`. Teach it one here rather" >&2
    echo "  than letting it succeed silently -- that is what cost twelve cases." >&2
    exit 1
    ;;
esac
destination=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then destination=$2; shift 2; continue; fi
    shift
done
if [ -n "${STUB_GO_FAIL_IN:-}" ] && [ "${PWD}" != "${PWD/${STUB_GO_FAIL_IN}/}" ]; then
    echo "stub go: refusing to build in ${PWD}" >&2
    exit 1
fi
mkdir -p "$(dirname "${destination}")"
printf '#!/bin/sh\n# %s\nexit 0\n' "${destination}" >"${destination}"
chmod +x "${destination}"
STUB
chmod +x "${stub_dir}/go"

# The CLI stub answers `version`, writes state on apply, and returns a scripted
# exit code per plan invocation so the control and the subject can differ.
cat >"${stub_dir}/stub-cli" <<'STUB'
#!/usr/bin/env bash
set -uo pipefail
chdir=""
subcommand=""
for arg in "$@"; do
    case "${arg}" in
        -chdir=*) chdir=${arg#-chdir=} ;;
        -*) ;;
        *) if [ -z "${subcommand}" ]; then subcommand=${arg}; fi ;;
    esac
done
case "${subcommand}" in
    version) echo "StubTofu v9.9.9"; exit 0 ;;
    apply)
        if [ "${STUB_APPLY_EXIT:-0}" -ne 0 ]; then
            echo "stub apply: refusing"; exit "${STUB_APPLY_EXIT}"
        fi
        if [ "${STUB_APPLY_WRITES_STATE:-1}" -eq 1 ]; then
            echo '{"version":4}' >"${chdir}/terraform.tfstate"
        fi
        exit 0 ;;
    plan)
        counter=${STUB_STATE_DIR}/plan-count
        n=$(cat "${counter}" 2>/dev/null || echo 0)
        n=$((n + 1))
        echo "${n}" >"${counter}"
        if [ "${n}" -eq 1 ]; then exit "${STUB_CONTROL_EXIT:-0}"; fi
        exit "${STUB_SUBJECT_EXIT:-0}" ;;
    destroy) exit "${STUB_DESTROY_EXIT:-0}" ;;
    *) exit 0 ;;
esac
STUB
chmod +x "${stub_dir}/stub-cli"

fixture=${work}/fixture
mkdir -p "${fixture}"
echo '# fixture' >"${fixture}/main.tf"
echo 0 >"${fixture}/EXPECT_OLD_PLAN"

# A regression fixture declares that the released provider must NOT settle it.
regression=${work}/regression
mkdir -p "${regression}"
echo '# fixture' >"${regression}/main.tf"
echo 2 >"${regression}/EXPECT_OLD_PLAN"

# And one that declares nothing at all.
undeclared=${work}/undeclared
mkdir -p "${undeclared}"
echo '# fixture' >"${undeclared}/main.tf"

# The suite must not need a TAG. A CI clone carries none, and the workflow
# fetches v0.101.2 fourteen lines AFTER this runs, so every behavioural case
# died at ref resolution -- which is what "needs neither controller nor
# network" was supposed to mean.
self_test_ref=$(git -C "${repository_root}" rev-parse HEAD)

failures=0
run_case() {
    local name=$1 want_exit=$2 want_text=$3
    shift 3
    local state_dir=${work}/state-${name}
    mkdir -p "${state_dir}"
    local log=${work}/${name}.log
    local code=0
    set +e
    # Per-case assignments come LAST so they override the defaults. env applies
    # them in order and the later one wins, so putting "$@" first silently
    # discarded every override -- which made three cases measure the default
    # fixture while claiming to measure another.
    # The script now takes the tree state before anything else, so on a
    # developer's machine -- dirty, which is when tests get run -- it would
    # refuse before reaching a single case. Acknowledge the dirt rather than
    # demand a clean tree to run the suite. The refusal itself is not lost:
    # tree-state_test.sh exercises refuse, permit and record against a
    # throwaway repository, which is where that belongs.
    env PATH="${stub_dir}:${PATH}" \
        EVIDENCE_ALLOW_DIRTY_TREE=1 \
        UPGRADE_RELEASED_REF="${self_test_ref}" \
        STUB_STATE_DIR="${state_dir}" \
        TERRAFORM_BIN="${stub_dir}/stub-cli" \
        UPGRADE_FIXTURE="${fixture}" \
        UPGRADE_OUTPUT="${work}/${name}.json" \
        UNIFI_API=https://stub UNIFI_USERNAME=stub UNIFI_PASSWORD=stub \
        "$@" \
        bash "${script}" >"${log}" 2>&1
    code=$?
    set -e
    if [ "${code}" -ne "${want_exit}" ]; then
        echo "FAIL ${name}: exit ${code}, want ${want_exit}"
        sed 's/^/      /' "${log}" | head -6
        failures=$((failures + 1))
        return
    fi
    if ! grep -q -- "${want_text}" "${log}"; then
        echo "FAIL ${name}: output does not mention '${want_text}'"
        sed 's/^/      /' "${log}" | head -6
        failures=$((failures + 1))
        return
    fi
    echo "ok   ${name} (exit ${code})"
}

# 1. The happy path must pass, or every rejection below proves only that the
#    harness rejects everything.
run_case pass 0 "plans clean under the candidate" STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=0

# 2. The two red shapes must NOT share a message.
run_case control_dirty 2 "does not settle under the released provider" \
    STUB_CONTROL_EXIT=2 STUB_SUBJECT_EXIT=0
run_case subject_dirty 1 "upgrade regression" \
    STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=2

# 3. A plan that cannot run is not a plan that reported no changes.
run_case control_error 2 "could not plan its own state" \
    STUB_CONTROL_EXIT=1 STUB_SUBJECT_EXIT=0
run_case subject_error 1 "could not plan state written by" \
    STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=1

# 4. Silence must not read as success.
run_case apply_fails 1 "could not apply the fixture" STUB_APPLY_EXIT=3
run_case no_state 1 "wrote no state" STUB_APPLY_WRITES_STATE=0
run_case old_build_fails 1 "released .*provider did not build" STUB_GO_FAIL_IN=released

# 5. The receipt must carry what the CLI called itself, not the variable name.
if [ -f "${work}/pass.json" ]; then
    cli=$(jq -r '.cli' "${work}/pass.json")
    if [ "${cli}" != "StubTofu v9.9.9" ]; then
        echo "FAIL receipt: cli = ${cli}, want the CLI's own identity"
        failures=$((failures + 1))
    else
        echo "ok   receipt records the CLI's self-reported identity"
    fi
    result=$(jq -r '.result' "${work}/pass.json")
    test "${result}" = pass || { echo "FAIL receipt: result=${result}"; failures=$((failures + 1)); }
fi

# 6. A published released binary must be preferred over rebuilding its source,
#    and the receipt must say which was used. Provenance that is not recorded is
#    provenance nobody can check later.
published=${work}/published/terraform-provider-unifi_v0.101.2
mkdir -p "$(dirname "${published}")"
printf '#!/bin/sh\n# published\nexit 0\n' >"${published}"
chmod +x "${published}"
run_case published_binary 0 "plans clean under the candidate" \
    STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=0 UPGRADE_RELEASED_BINARY="${published}"
if [ -f "${work}/published_binary.json" ]; then
    provenance=$(jq -r '.released_provenance' "${work}/published_binary.json")
    if [ "${provenance}" != "published-binary" ]; then
        echo "FAIL provenance: ${provenance}, want published-binary"
        failures=$((failures + 1))
    else
        echo "ok   receipt records that the published binary was used"
    fi
fi
if [ -f "${work}/pass.json" ]; then
    provenance=$(jq -r '.released_provenance' "${work}/pass.json")
    if [ "${provenance}" != "source-build" ]; then
        echo "FAIL provenance fallback: ${provenance}, want source-build"
        failures=$((failures + 1))
    else
        echo "ok   receipt distinguishes the source-build fallback"
    fi
fi

# 7. The control's expected value is a property of the fixture, and a fixture
#    that declares nothing must be refused rather than defaulted. Defaulting is
#    how a fix gets reported as proven by a fixture that never showed the defect.
run_case undeclared_fixture 1 "does not declare EXPECT_OLD_PLAN" \
    STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=0 UPGRADE_FIXTURE="${undeclared}"

# 8. A regression fixture inverts the control: a released provider that SETTLES
#    it has failed to demonstrate the defect, so a clean subject proves nothing.
run_case regression_ok 0 "plans clean under the candidate" \
    STUB_CONTROL_EXIT=2 STUB_SUBJECT_EXIT=0 UPGRADE_FIXTURE="${regression}"
run_case regression_control_settled 2 "did not" \
    STUB_CONTROL_EXIT=0 STUB_SUBJECT_EXIT=0 UPGRADE_FIXTURE="${regression}"

if [ "${failures}" -ne 0 ]; then
    echo "${failures} case(s) failed"
    exit 1
fi
echo "all upgrade-harness cases passed"
