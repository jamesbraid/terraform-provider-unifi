#!/usr/bin/env bash
# Proves the guard refuses, permits, and records — in a throwaway repository.
#
# A guard whose refusal was never observed is the defect it exists to remove.
# Each case runs against a real git repository built here, because the thing
# under test is what `git status --porcelain` reports, and mocking that would
# test the mock.
set -uo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly library="${repository_root}/.woodpecker/scripts/tree-state.sh"

failures=0
work=$(mktemp -d)
# Set per case to run the guard from somewhere other than the repository it
# is describing; empty means "run from the repository itself".
case_cwd=
trap 'rm -rf "${work}"' EXIT

# fresh_repo builds a repository with one commit and prints its path.
fresh_repo() {
    local dir="${work}/repo$1"
    mkdir -p "${dir}/.woodpecker/scripts"
    git -C "${dir}" init -q
    git -C "${dir}" config user.email t@example.com
    git -C "${dir}" config user.name test
    # The guard resolves its repository from its own path, so each throwaway
    # repository carries a real copy at the same relative location. It is
    # committed, or every case would start dirty and the clean case could not
    # exist at all.
    cp "${library}" "${dir}/.woodpecker/scripts/tree-state.sh"
    printf 'one\n' >"${dir}/committed.txt"
    git -C "${dir}" add committed.txt .woodpecker/scripts/tree-state.sh
    git -C "${dir}" -c commit.gpgsign=false commit -qm first
    printf '%s' "${dir}"
}

# run_case executes the guard inside a repository and captures both streams.
run_case() {
    local name=$1 dir=$2 expected_exit=$3 allow=$4
    shift 4
    local output status
    output=$( {
        cd "${case_cwd:-${dir}}" || exit 99
        # shellcheck source=.woodpecker/scripts/tree-state.sh
        source "${dir}/.woodpecker/scripts/tree-state.sh"
        export EVIDENCE_ALLOW_DIRTY_TREE="${allow}"
        evidence_tree_state "the test artifact" && {
            printf 'STATUS=%s\n' "${evidence_tree_status}"
            evidence_tree_json
        }
    } 2>&1 )
    status=$?

    if [[ ${status} -ne ${expected_exit} ]]; then
        printf 'FAIL %s: exit %d, want %d\n  got: %s\n' "${name}" "${status}" "${expected_exit}" "${output}" >&2
        failures=$((failures + 1))
        return
    fi
    local wanted
    for wanted in "$@"; do
        if [[ ${output} != *"${wanted}"* ]]; then
            printf 'FAIL %s: output does not mention %q\n  got: %s\n' "${name}" "${wanted}" "${output}" >&2
            failures=$((failures + 1))
            return
        fi
    done
    printf 'ok   %s\n' "${name}"
}

clean=$(fresh_repo clean)
run_case "a clean tree proceeds and records its commit" "${clean}" 0 "" \
    "STATUS=clean" '"status": "clean"' '"dirty_paths": []'

# The refusal. This is the case that was not caught when the evidence inventory
# was generated against uncommitted files.
modified=$(fresh_repo modified)
printf 'two\n' >>"${modified}/committed.txt"
run_case "a modified file is refused, and named" "${modified}" 1 "" \
    "refusing to generate evidence from a dirty tree" "committed.txt" \
    "EVIDENCE_ALLOW_DIRTY_TREE"

# Untracked files are the exact shape of the original failure: evidence digested
# before it was added. Excluding them would leave the hole open.
untracked=$(fresh_repo untracked)
printf 'new\n' >"${untracked}/receipt.json"
run_case "an untracked file is refused too" "${untracked}" 1 "" \
    "refusing to generate evidence" "receipt.json"

# The escape hatch must record rather than merely permit. A run that proceeds
# quietly is the same as no guard.
acknowledged=$(fresh_repo acknowledged)
printf 'two\n' >>"${acknowledged}/committed.txt"
run_case "an acknowledged dirty run records the dirt" "${acknowledged}" 0 1 \
    "STATUS=dirty" '"status": "dirty"' "committed.txt" "DIRTY tree"

# The commit must be real, not empty: an artifact naming no commit describes
# nothing, and would satisfy a consumer that only checks the field is present.
run_case "the recorded commit is a real sha" "${clean}" 0 "" \
    "$(git -C "${clean}" rev-parse HEAD)"

# THE SEAM. Every generator resolves its own repository root from BASH_SOURCE,
# and some of them cd elsewhere before the guard runs -- the controller
# differential's self-test executes the real script from a mktemp directory. A
# guard that resolved from the working directory agreed with its callers
# everywhere except there, and reported "not a git repository", which reads
# like a broken checkout rather than a resolution bug. It took the entire
# controller campaign down on its first command across seven manual pipelines.
outside=${work}/outside-any-repository
mkdir -p "${outside}"
if git -C "${outside}" rev-parse --show-toplevel >/dev/null 2>&1; then
    # Without this the two cases below would pass by accident wherever the
    # temporary directory happened to sit inside a repository.
    printf 'FAIL: %s is inside a git repository, so cwd-independence proves nothing here\n' \
        "${outside}" >&2
    failures=$((failures + 1))
else
    case_cwd=${outside}

    resolved=$(fresh_repo resolved)
    run_case "the owning repository is found from outside any repository" "${resolved}" 0 "" \
        "STATUS=clean" "$(git -C "${resolved}" rev-parse HEAD)"

    # Resolving *a* repository is not enough. It has to be the one that owns
    # the guard, so dirt in that repository must still be seen and named.
    elsewhere=$(fresh_repo elsewhere)
    printf 'two\n' >>"${elsewhere}/committed.txt"
    run_case "dirt in the owning repository is seen from outside it" "${elsewhere}" 1 "" \
        "refusing to generate evidence from a dirty tree" "committed.txt"

    case_cwd=
fi

if [[ ${failures} -ne 0 ]]; then
    printf '\n%d case(s) did not behave as documented\n' "${failures}" >&2
    exit 1
fi
printf '\nthe tree-state guard refuses, permits and records as documented\n'
