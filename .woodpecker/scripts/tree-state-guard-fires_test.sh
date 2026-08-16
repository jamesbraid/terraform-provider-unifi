#!/usr/bin/env bash
# Watches every guarded generator REFUSE, rather than reading that it calls the
# guard.
#
# tree-state-coverage_test.sh reports a script as guarded when its body contains
# evidence_tree_state. That is a static match, and it is the weakest claim in
# that file: it proves the call is written, not that it runs, not that it runs
# FIRST, and not that the script stops when it refuses. Two scripts reached "ok
# guarded" on that grep alone with no self-test anywhere -- coverage asserted on
# the strength of a string.
#
# It is the same shape as the thing the coverage check exists to find, one level
# up. A check reports coverage; the coverage claim itself had no proof.
#
# So this runs them. The tree is made dirty on purpose, each guarded generator
# is executed with a stripped environment, and each must exit non-zero having
# said it is refusing. Running them is safe BECAUSE of the property being
# tested: in all seven the guard precedes the first network, docker, build or
# CLI call, so a script that refuses correctly never reaches anything with a
# side effect. One that does not refuse is the defect this exists to catch, and
# it fails immediately afterwards on a missing required variable.
#
# The environment is stripped rather than populated for the same reason the
# ordering matters: with TERRAFORM_BIN and friends unset, a script that checked
# its inputs before its tree would fail naming the variable. Every one of them
# names the receipt instead, which is how the ordering is established rather
# than assumed.
#
# PROVEN TO FAIL, twice, each restored afterwards:
#
#   export EVIDENCE_ALLOW_DIRTY_TREE=1 inserted above m3-dns-operation.sh's
#   guard (227 -> 228 lines, counted rather than re-grepped) turned this red
#   with "failed on a dirty tree but not because of the guard ... generating
#   from a DIRTY tree, as permitted by EVIDENCE_ALLOW_DIRTY_TREE".
#
#   the probe repointed outside the repository turned it red with "the probe
#   did not make the tree dirty, so every case below would prove nothing" --
#   which is the case that matters most, because without it a suite that
#   dirtied nothing would report seven passes against seven clean-tree runs.
set -uo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly scripts="${repository_root}/.woodpecker/scripts"

failures=0
fail() {
    printf 'FAIL %s\n' "$1" >&2
    failures=$((failures + 1))
}

# The probe is what makes the tree dirty. It is untracked, so it cannot alter a
# tracked file, and the trap removes it on every exit path including a failure
# in the middle of the loop -- a stray file here would be read by every later
# step as an uncommitted change.
readonly probe="${repository_root}/.tree-state-guard-probe"
cleanup() {
    rm -f "${probe}"
}
trap cleanup EXIT
printf 'making the tree dirty on purpose\n' >"${probe}"

if [[ -z $(git -C "${repository_root}" status --porcelain 2>/dev/null) ]]; then
    fail "the probe did not make the tree dirty, so every case below would prove nothing"
    exit 1
fi

# Derived the same way the coverage check derives it, deliberately restated
# rather than shared. If the two derivations ever disagree that is worth
# knowing; a shared helper would make them agree no matter what either did.
guarded=()
for path in "${scripts}"/*.sh; do
    name=$(basename "${path}")
    case ${name} in *_test.sh | tree-state.sh) continue ;; esac
    grep -vE '^[[:space:]]*#' "${path}" | grep -q 'evidence_tree_state' || continue
    guarded+=("${name}")
done

if [[ ${#guarded[@]} -eq 0 ]]; then
    fail "no guarded generator was derived, so this suite asserts nothing"
    exit 1
fi

for name in "${guarded[@]}"; do
    output=$(env -i PATH="${PATH}" HOME="${HOME}" bash "${scripts}/${name}" 2>&1)
    status=$?

    if [[ ${status} -eq 0 ]]; then
        fail "${name} ran to completion on a dirty tree; its guard did not stop it"
        continue
    fi
    if ! printf '%s' "${output}" | grep -q 'refusing to generate evidence from a dirty tree'; then
        fail "${name} failed on a dirty tree but not because of the guard; it exited ${status} saying: $(printf '%s' "${output}" | head -1)"
        continue
    fi
    if ! printf '%s' "${output}" | grep -q "${probe##*/}"; then
        fail "${name} refused without naming the file that made the tree dirty; the operator cannot tell what to commit"
        continue
    fi
    printf 'ok   refused, and named the dirt: %s\n' "${name}"
done

printf '\n%d of %d guarded generator(s) were watched refusing.\n' \
    "$((${#guarded[@]} - failures))" "${#guarded[@]}"

if [[ ${failures} -ne 0 ]]; then
    printf '\n%d guarded generator(s) could not be shown to refuse\n' "${failures}" >&2
    exit 1
fi
