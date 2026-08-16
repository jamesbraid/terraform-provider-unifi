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
    # Five Go binaries left this list when they gained -tree-state. The reason
    # they carried -- "a Go binary cannot source a bash library" -- was true of
    # a Go binary MEASURING the tree and false of one being HANDED the answer,
    # which is the distinction the fix turned on.
    #
    # TWO stay, for a different and permanent reason: their artifacts are
    # committed and compared byte for byte against a fresh run --
    # catalog-evidence-inventory_test.sh:22 for one, catalog-build-schema.sh:152
    # for the other. tree_state contains the commit, so an artifact recording
    # its own would never reproduce: generated at one commit, committed,
    # regenerated at the next, comparison fails forever. Neither needs one --
    # reproducing byte for byte is a stronger claim about which tree an artifact
    # describes than a self-declared field. Tree state belongs in transient
    # receipts; a committed artifact proves its tree by being reproducible.
    #
    # schema-baseline reached this list carrying the refuted reason rather than
    # this one. It was not among the five that changed, so nothing revisited it,
    # and a reason survives by not being looked at. Its artifact,
    # build/m0/provider-schema-digests.json, is committed and cmp'd, which puts
    # it in this class and always did.
    "cmd/catalog-evidence|its artifact is committed and compared byte for byte, so a recorded commit could never reproduce; reproducibility is the stronger claim"
    "cmd/schema-baseline|its artifact is committed and cmp'd at catalog-build-schema.sh:152, so a recorded commit could never reproduce; reproducibility is the stronger claim"
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

    # A Go binary is guarded when BOTH halves hold, and the second is the one
    # that actually matters. The binary must declare -tree-state, and every
    # workflow line that runs it must pass it. A binary that demands the flag
    # while a call site omits it is not guarded, it is broken -- and that is the
    # only shape this can fail in, because the flag has no default.
    #
    # The guard itself stays in bash. Go never measures the tree; it is handed
    # the answer and refuses without one. So this looks for the seam, not for a
    # Go reimplementation.
    grep -qE '"tree-state"' "${dir}"*.go 2>/dev/null || continue
    call_sites=$(grep -rhoE "go run [^|&]*cmd/${command_name}[^|&]*" "${repository_root}/.woodpecker" || true)
    [[ -z ${call_sites} ]] && continue
    unpassed=$(printf '%s\n' "${call_sites}" | grep -vc -- '-tree-state' || true)
    if [[ ${unpassed} -ne 0 ]]; then
        fail "cmd/${command_name} takes -tree-state but ${unpassed} workflow call site(s) do not pass it; the binary will refuse at run time"
        continue
    fi
    guarded+=("cmd/${command_name}")
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

# 4. No Go binary measures the tree for itself.
#
#    This rule used to read "there is still no Go dirty-tree guard", and it was
#    right to assert rather than assume -- but it asserted the absence of the
#    wrong thing. Its trigger was EVIDENCE_ALLOW_DIRTY_TREE appearing in a .go
#    file, which detects a Go REIMPLEMENTATION of the guard. The guard that was
#    actually built passes the measured state IN as a required flag and never
#    reads the environment, so the old trigger would have stayed silent while
#    five Go binaries began demanding and recording tree state -- and the five
#    exemptions saying "a Go binary cannot source a bash library" would have
#    stayed on this list, false, with nothing able to notice.
#
#    So the assertion is inverted to the thing we actually want to stay true:
#    the measurement has ONE home. A Go file reading the environment variable,
#    or shelling out to git for status, means the rule has grown a second
#    implementation that can disagree with the first.
if grep -rq 'EVIDENCE_ALLOW_DIRTY_TREE' --include='*.go' "${repository_root}"; then
    fail "a Go file reads EVIDENCE_ALLOW_DIRTY_TREE; the measurement belongs in tree-state.sh alone, and a second implementation in another language can disagree with the first"
fi
if grep -rqE 'git.*status --porcelain' --include='*.go' "${repository_root}"; then
    fail "a Go file measures the working tree itself; it should be handed the state that tree-state.sh measured, not measure it again"
fi

# 5. A guarded generator RECORDS the state, it does not merely ask for it.
#
#    Calling the guard and recording its answer are different things, and for a
#    long time every guarded script did the first and none did the second. The
#    refusal worked; the third case in tree-state.sh -- dirty, acknowledged,
#    RECORD it -- wrote nothing, so under EVIDENCE_ALLOW_DIRTY_TREE=1 an
#    acknowledged-dirty artifact came out byte-identical to a clean one. That is
#    the case the library's own header calls "the point".
#
#    It went unseen because the rules above ask whether the guard is CALLED and
#    stop there. A check on one half of a mechanism reads as coverage of the
#    whole -- the same defect the guard exists to remove, one level up.
#
#    A shell generator records by calling evidence_tree_json. A Go one records
#    by being passed -tree-state, which the derivation above already required,
#    so it is recording by construction and needs no separate check.
#    One shell generator cannot record, and it is the flagship:
#    catalog-evidence-inventory.sh never touches its own artifact -- it hands
#    the path to `go run ./cmd/catalog-evidence`, which writes the JSON. The
#    script and the binary are two halves of one producer, so it inherits the
#    binary's exemption rather than earning a new one, and that artifact is the
#    committed, byte-compared one that must not carry a tree state at all.
readonly -a records_exempt=(
    "catalog-evidence-inventory.sh|writes nothing itself: cmd/catalog-evidence writes the artifact, and that artifact is committed and byte-compared, so it must not carry a tree state"
)
records_is_exempt() {
    local entry
    for entry in "${records_exempt[@]}"; do
        [[ $1 == "${entry%%|*}" ]] && return 0
    done
    return 1
}
for name in "${guarded[@]:-}"; do
    case ${name} in cmd/*) continue ;; esac
    records_is_exempt "${name}" && continue
    if ! grep -q 'evidence_tree_json' "${scripts}/${name}"; then
        fail "${name} calls the guard but never embeds evidence_tree_json; an acknowledged-dirty run would be indistinguishable from a clean one"
    fi
done
# And an exemption here must not go stale either: if it starts recording, the
# reason is wrong and the entry has to go.
for entry in "${records_exempt[@]}"; do
    name=${entry%%|*}
    if grep -q 'evidence_tree_json' "${scripts}/${name}" 2>/dev/null; then
        fail "${name} is exempt from recording but now embeds evidence_tree_json; delete its exemption"
    fi
done

# 6. The committed-and-byte-compared reason is CHECKED, not trusted.
#
#    schema-baseline sat on this list carrying a refuted reason -- "a Go binary
#    cannot source a bash library" -- straight through the change that refuted
#    it. It was not among the five that moved, so nothing revisited it, and a
#    reason survives by not being looked at. Every other rule here checks a
#    claim against the tree; the reasons themselves were the one thing taken on
#    trust.
#
#    Most reasons cannot be checked -- "the pipeline that exercises it is down"
#    is a fact about the world. But this class makes two claims that are purely
#    mechanical: the artifact is committed, and something compares it against a
#    fresh run. If either stops holding, the exemption is wrong and the binary
#    should be guarded like any other. So the artifact is NAMED rather than
#    described, and both halves are verified.
readonly -a committed_artifact=(
    "cmd/catalog-evidence|build/release-ready/catalog-evidence-inventory.json"
    "cmd/schema-baseline|build/m0/provider-schema-digests.json"
)
for entry in "${committed_artifact[@]}"; do
    name=${entry%%|*}
    artifact=${entry#*|}
    if ! is_exempt "${name}"; then
        fail "${name} names a committed artifact but is not exempt; either it is guarded now or this entry is stale"
        continue
    fi
    if ! git -C "${repository_root}" ls-files --error-unmatch "${artifact}" >/dev/null 2>&1; then
        fail "${name} is exempt because ${artifact} is committed, but git does not track it; the reason no longer holds"
        continue
    fi
    # A script that NAMES the artifact and ALSO runs cmp, not both on one line.
    # The first draft demanded them on the same line and reported a false
    # failure against catalog-evidence-inventory_test.sh, which assigns the path
    # at line 21 and cmp's the variable at line 23 -- which is how anyone would
    # write it. A check narrower than the thing it checks fails honest code.
    #
    # THIS FILE IS EXCLUDED FROM ITS OWN SEARCH, and that is not tidiness. The
    # second draft searched every script including this one, and this one names
    # both artifacts (in the array above) and contains the letters cmp (in these
    # comments). So it satisfied its own predicate: deleting the real comparer
    # would have left the check green, pointing at itself as the evidence. It
    # gave the right answer only because grep happened to reach the genuine
    # script first. Mutating the entry to a tracked file nothing compares --
    # build/m0/README.md -- passed, naming README.md "committed and compared".
    #
    # A MENTION IS NOT AN INVOCATION either, so comments are stripped before
    # looking for cmp. Both holes are the shape this rule exists to catch: a
    # reason that survives by nothing being able to contradict it.
    comparer=""
    while IFS= read -r candidate; do
        [[ $(basename "${candidate}") == "$(basename "${BASH_SOURCE[0]}")" ]] && continue
        sed 's/#.*//' "${candidate}" | grep -qE '(^|[^[:alnum:]_])cmp[[:space:]]' &&
            comparer=${candidate} && break
    done < <(grep -rlF "${artifact}" "${scripts}" 2>/dev/null)
    if [[ -z ${comparer} ]]; then
        fail "${name} is exempt because ${artifact} is compared byte for byte, but no script both names it and runs cmp; the reason no longer holds"
        continue
    fi
    printf 'ok   reason verified:        %s -- %s is committed and compared\n' "${name}" "${artifact}"
done

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
