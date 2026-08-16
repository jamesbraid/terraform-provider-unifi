#!/usr/bin/env bash
# Reports how many evidence generators carry the dirty-tree guard, over a
# population DERIVED from the tree.
#
# This used to hand-maintain three arrays: the guarded, the unguarded, and every
# evidence writer. Three lists that must agree with each other AND with the tree
# is three chances to drift, and it drifted the way lists do. It reported five of
# eight while the tree held ten bash generators; it could not see a Go one at
# all; and the one generator it did catch, it caught only because that generator
# happened to name a literal build/ path.
#
# So the population is computed here, and the only thing written down is the
# EXEMPTIONS. That list has to stay, and it is the one that should: "this one is
# uncovered, and here is why" is a judgement, and a tree cannot hold a judgement.
# The cost of leaving something uncovered is still writing down that you did.
#
# HOW THE POPULATION IS DERIVED
#
#   bash  every .woodpecker/scripts/*.sh, excluding *_test.sh, whose body --
#         comment lines removed -- names a build/<dir>/<file>.json path or an
#         *OUTPUT* variable. Stripping comments is what keeps tree-state.sh out
#         of its own population: every build/ path in that file is prose.
#
#   Go    every cmd/* invoked from .woodpecker/ that writes a file. Binaries
#         named by a go:generate directive are excluded BY CONSTRUCTION rather
#         than by exception: a code generator's working condition is a tree it
#         is about to change, so a guard that refuses a dirty tree would refuse
#         the job. The two sets are disjoint, which is what makes "invoked from
#         a workflow" usable as the rule instead of "writes a file".
#
# Both rules match CANDIDATES, not proven writers. A script that only READ an
# artifact would be counted as well; today all ten write. That direction is the
# safe one: over-counting demands a written reason, under-counting is silent,
# and silence is the defect this whole file exists to remove.
set -uo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly scripts="${repository_root}/.woodpecker/scripts"

failures=0

fail() {
    printf 'FAIL %s\n' "$1" >&2
    failures=$((failures + 1))
}

# THE ONE HAND-MAINTAINED LIST. Each entry is "name|reason", and the reason is
# the point of the entry.
#
# The three m-scripts carried a different reason until now: that there was no
# usable repository root to guard from. That reason is obsolete and was left
# standing after the fact that made it obsolete had already landed.
# evidence_tree_state resolves its own root from BASH_SOURCE, and all three sit
# in this same directory, so sourcing the library needs no root either. What
# still blocks them is the second half of the original reason, which is intact:
# the pipeline that exercises them is down, so no call site could be watched
# refusing on a real dirty tree, and a call site never seen to run is the shape
# of change that looks like coverage and is not.
readonly -a exempt=(
    "m1-dns-compiler.sh|cannot be executed to verify: the pipeline that exercises it is down"
    "m0-uos-dns-qualification.sh|cannot be executed to verify: the pipeline that exercises it is down"
    "m3-dns-qualification.sh|cannot be executed to verify: the pipeline that exercises it is down"
    "cmd/catalog-admission|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/catalog-evidence|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/catalog-hardware-disposition|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/catalog-management-contract|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/catalog-migration-recovery|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/catalog-pragmatic-evidence|Go: the guard is a bash library a Go binary cannot source (task 56)"
    "cmd/schema-baseline|Go: the guard is a bash library a Go binary cannot source (task 56)"
)

is_exempt() {
    local entry
    for entry in "${exempt[@]}"; do
        [[ $1 == "${entry%%|*}" ]] && return 0
    done
    return 1
}

# ---------------------------------------------------------------- derive: bash
population=()
guarded=()

for path in "${scripts}"/*.sh; do
    name=$(basename "${path}")
    case ${name} in *_test.sh) continue ;; esac

    body=$(grep -vE '^[[:space:]]*#' "${path}")
    printf '%s\n' "${body}" |
        grep -qE 'build/[a-z0-9-]+/[a-z0-9-]+\.json|\$\{?[A-Z0-9_]*OUTPUT[A-Z0-9_]*' || continue

    population+=("${name}")
    if printf '%s\n' "${body}" | grep -q 'evidence_tree_state'; then
        guarded+=("${name}")
    fi
done

# ------------------------------------------------------------------ derive: Go
# A go:generate binary must never be guarded, so it is not in the population.
generators=$(grep -rh 'go:generate' --include='*.go' "${repository_root}" |
    grep -oE 'cmd/[a-z0-9-]+' | sed 's|cmd/||' | sort -u)

for dir in "${repository_root}"/cmd/*/; do
    command_name=$(basename "${dir}")

    printf '%s\n' "${generators}" | grep -qx "${command_name}" && continue

    grep -rqE "cmd/${command_name}([^a-z0-9-]|\$)" "${repository_root}/.woodpecker" || continue

    writes=0
    while IFS= read -r source_file; do
        if grep -qE 'os\.(WriteFile|Create|CreateTemp|OpenFile)' "${source_file}"; then
            writes=1
            break
        fi
    done < <(find "${dir}" -name '*.go' ! -name '*_test.go')
    [[ ${writes} -eq 0 ]] && continue

    population+=("cmd/${command_name}")
    # No Go generator carries the guard. Rule 4 asserts that rather than
    # assuming it, so this loop cannot quietly keep reporting zero after
    # somebody does the work.
done

# ------------------------------------------------------------------------ rules

# 1. Every derived generator is guarded, or exempt with a reason. This is the
#    rule that stops a new generator arriving unnoticed, and unlike the pattern
#    it replaces it does not depend on the generator naming a literal path.
for name in "${population[@]}"; do
    printf '%s\n' "${guarded[@]:-}" | grep -qx "${name}" && continue
    if is_exempt "${name}"; then
        continue
    fi
    fail "${name} generates evidence, does not carry the guard, and is not exempt; guard it or write down why not"
done

# 2. No exemption is stale. One that quietly gained the guard must leave this
#    list: a reader trusts it to say what is uncovered, and an over-long list
#    understates coverage as surely as a short one overstates it.
for entry in "${exempt[@]}"; do
    name=${entry%%|*}
    if printf '%s\n' "${guarded[@]:-}" | grep -qx "${name}"; then
        fail "${name} is exempt but now carries the guard; delete its exemption"
    fi
done

# 3. No exemption names something that is not a generator any more. A reason for
#    a file that no longer writes evidence is a claim about nothing, and it makes
#    the uncovered count read higher than the truth.
for entry in "${exempt[@]}"; do
    name=${entry%%|*}
    if ! printf '%s\n' "${population[@]}" | grep -qx "${name}"; then
        fail "${name} is exempt but is no longer a derived evidence generator; delete its exemption"
    fi
done

# 4. There is still no Go dirty-tree guard. Asserted, not assumed: without this
#    the Go half of the exemption list would keep its reason forever, and the
#    day somebody writes the guard the count would go on reporting zero because
#    nothing here knows how to see a Go call site.
if grep -rq 'EVIDENCE_ALLOW_DIRTY_TREE' --include='*.go' "${repository_root}"; then
    fail "a Go file now honours EVIDENCE_ALLOW_DIRTY_TREE; teach this check to detect Go call sites, then move the covered commands out of the exemption list"
fi

# ----------------------------------------------------------------------- report
printf '\n'
for name in "${population[@]}"; do
    if printf '%s\n' "${guarded[@]:-}" | grep -qx "${name}"; then
        printf 'ok   guarded:                %s\n' "${name}"
    elif is_exempt "${name}"; then
        for entry in "${exempt[@]}"; do
            [[ ${name} == "${entry%%|*}" ]] && printf 'ok   uncovered, on purpose:  %s -- %s\n' "${name}" "${entry#*|}"
        done
    else
        # Rule 1 has already failed on this one. Say so here too: a reader who
        # reads only the report should not have to infer a member's existence
        # from the total not adding up.
        printf 'FAIL unaccounted:            %s\n' "${name}"
    fi
done

printf '\nCOVERAGE: %d of %d derived evidence generators carry the dirty-tree guard.\n' \
    "${#guarded[@]}" "${#population[@]}"

printf '\nSEPARATELY, and NOT addressed by this guard: thirteen of the nineteen\n'
printf 'evidence artifacts under build/ have no producer at all -- including all\n'
printf 'five wave receipts, which are maintained by hand. A guard on generators\n'
printf 'cannot protect an artifact that has none. Recorded as task 64.\n'

if [[ ${failures} -ne 0 ]]; then
    printf '\n%d coverage claim(s) do not match the tree\n' "${failures}" >&2
    exit 1
fi
