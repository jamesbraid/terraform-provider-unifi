#!/usr/bin/env bash
# Names what the dirty-tree guard does NOT cover, and counts it.
#
# A guard on some generators makes the whole suite look protected. This is the
# same defect the guard exists to remove, one level up: a silent exclusion and a
# counted one look identical in a green run and differ completely to the next
# reader.
#
# So the exclusions are listed here by name with a reason each, and this fails
# when reality stops matching the list -- a new evidence generator appearing
# unguarded, or a listed exclusion disappearing. The list has to be maintained
# deliberately, which is the point: the cost of leaving something uncovered is
# writing down that you did.
set -uo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly scripts="${repository_root}/.woodpecker/scripts"

failures=0

# GUARDED: sources tree-state.sh and calls evidence_tree_state.
readonly -a guarded=(
    catalog-evidence-inventory.sh
    catalog-build-schema.sh
    m3-dns-operation.sh
    catalog-controller-differential.sh
    catalog-unit-differential.sh
)

# NOT GUARDED, with the obstacle. Each writes an evidence artifact and each has
# a structural reason the guard is not placed yet, not an oversight.
#
#   m1-dns-compiler.sh          resolves its repository root at line 145, inside
#                               a function, so there is no early point to guard
#                               from without restructuring it
#   m0-uos-dns-qualification.sh establishes no repository root variable at all,
#                               and writes only when M0_UOS_RECEIPT_OUTPUT is set
#   m3-dns-qualification.sh     same, gated on M3_LIFECYCLE_RECEIPT_OUTPUT
#
# None of the three could be verified by running it: the pipeline that exercises
# them is down. Adding a call site never seen to execute is the shape of change
# that looks like coverage and is not.
readonly -a unguarded=(
    m1-dns-compiler.sh
    m0-uos-dns-qualification.sh
    m3-dns-qualification.sh
)

# Every script that writes an evidence artifact, guarded or not. A script
# appearing here that is in neither list above is what this test is for.
# Two of these were missed by the first enumeration, which counted backwards
# from artifacts committed under build/. catalog-controller-differential.sh and
# catalog-unit-differential.sh write evidence that is NOT committed, so counting
# from the artifact side could not see them. Rule 4 below found them, which is
# what it is for -- and it is the mirror of the orphan-artifact finding: there a
# file with no producer, here a producer with no file.
readonly -a evidence_writers=(
    catalog-evidence-inventory.sh
    catalog-build-schema.sh
    m3-dns-operation.sh
    catalog-controller-differential.sh
    catalog-unit-differential.sh
    m1-dns-compiler.sh
    m0-uos-dns-qualification.sh
    m3-dns-qualification.sh
)

fail() {
    printf 'FAIL %s\n' "$1" >&2
    failures=$((failures + 1))
}

# 1. Every script claimed guarded actually is.
for script in "${guarded[@]}"; do
    if [[ ! -f ${scripts}/${script} ]]; then
        fail "${script} is listed as guarded and does not exist"
        continue
    fi
    if ! grep -q 'evidence_tree_state' "${scripts}/${script}"; then
        fail "${script} is listed as guarded but never calls evidence_tree_state"
        continue
    fi
    printf 'ok   guarded: %s\n' "${script}"
done

# 2. Every script claimed unguarded actually is. A script quietly gaining the
# guard should move lists rather than leave a stale exclusion behind -- a
# reader trusts this list to say what is uncovered, and an over-long list
# understates coverage as surely as a short one overstates it.
for script in "${unguarded[@]}"; do
    if [[ ! -f ${scripts}/${script} ]]; then
        fail "${script} is listed as unguarded and does not exist"
        continue
    fi
    if grep -q 'evidence_tree_state' "${scripts}/${script}"; then
        fail "${script} is listed as NOT guarded but now calls evidence_tree_state; move it to guarded"
        continue
    fi
    printf 'ok   uncovered, deliberately: %s\n' "${script}"
done

# 3. The lists together must account for every evidence writer.
for script in "${evidence_writers[@]}"; do
    found=0
    for known in "${guarded[@]}" "${unguarded[@]}"; do
        [[ ${script} == "${known}" ]] && found=1 && break
    done
    if [[ ${found} -eq 0 ]]; then
        fail "${script} writes evidence and is in neither list; decide and record which"
    fi
done

# 4. A generator that appears later must not be able to arrive unnoticed. Any
# script sourcing the evidence library or writing under build/ is a candidate.
while IFS= read -r candidate; do
    name=$(basename "${candidate}")
    [[ ${name} == *_test.sh ]] && continue
    [[ ${name} == tree-state.sh ]] && continue
    known=0
    for listed in "${evidence_writers[@]}"; do
        [[ ${name} == "${listed}" ]] && known=1 && break
    done
    if [[ ${known} -eq 0 ]]; then
        fail "${name} writes under build/ and is in no list; it is a new evidence generator, or the pattern needs narrowing"
    fi
done < <(grep -rl 'build/[a-z0-9-]*/[a-z0-9-]*\.json' "${scripts}"/*.sh 2>/dev/null)

printf '\nCOVERAGE: %d of %d evidence generators carry the dirty-tree guard.\n' \
    "${#guarded[@]}" "${#evidence_writers[@]}"
printf 'UNCOVERED, by name: %s\n' "${unguarded[*]}"
printf '\nSEPARATELY, and NOT addressed by this guard: thirteen of the nineteen\n'
printf 'evidence artifacts under build/ have no producer at all -- including all\n'
printf 'five wave receipts, which are maintained by hand. A guard on generators\n'
printf 'cannot protect an artifact that has none. Recorded as task 64.\n'

if [[ ${failures} -ne 0 ]]; then
    printf '\n%d coverage claim(s) do not match the tree\n' "${failures}" >&2
    exit 1
fi
