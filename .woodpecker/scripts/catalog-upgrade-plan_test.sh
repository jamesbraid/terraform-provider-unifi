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
cat >"${stub_dir}/go" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" != "build" ]; then exit 0; fi
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
printf '#!/bin/sh\nexit 0\n' >"${destination}"
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

failures=0
run_case() {
    local name=$1 want_exit=$2 want_text=$3
    shift 3
    local state_dir=${work}/state-${name}
    mkdir -p "${state_dir}"
    local log=${work}/${name}.log
    local code=0
    set +e
    env "$@" \
        PATH="${stub_dir}:${PATH}" \
        STUB_STATE_DIR="${state_dir}" \
        TERRAFORM_BIN="${stub_dir}/stub-cli" \
        UPGRADE_FIXTURE="${fixture}" \
        UPGRADE_OUTPUT="${work}/${name}.json" \
        UNIFI_API=https://stub UNIFI_USERNAME=stub UNIFI_PASSWORD=stub \
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
run_case old_build_fails 1 "released" STUB_GO_FAIL_IN=released

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
printf '#!/bin/sh\nexit 0\n' >"${published}"
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

if [ "${failures}" -ne 0 ]; then
    echo "${failures} case(s) failed"
    exit 1
fi
echo "all upgrade-harness cases passed"
