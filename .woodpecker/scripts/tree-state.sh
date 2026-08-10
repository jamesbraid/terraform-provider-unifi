#!/usr/bin/env bash
# Evidence must say which tree it describes.
#
# An evidence artifact generated from a dirty working tree pins content that is
# not in any commit. It is honest about what it saw and wrong about what exists,
# and it heals silently once the files land, so the window where it was wrong
# leaves no trace. build/release-ready/catalog-evidence-inventory.json was
# produced that way once: it recorded digests for files that had not been
# committed, and the mismatch it caused was diagnosed three times before the
# cause was found, because the artifact looked internally consistent.
#
# The fix is not to forbid a dirty run. A human iterating locally has a real
# reason to generate evidence against uncommitted work, and a guard that blocks
# that is one they will work around by editing the artifact afterwards -- which
# is worse, because then nothing records anything. So:
#
#   clean tree                 -> record the commit, proceed
#   dirty, not acknowledged    -> refuse, and name the files
#   dirty, acknowledged        -> proceed, and RECORD the dirt in the artifact
#
# The third case is the point. The artifact stops being able to claim a commit
# it does not describe, whichever way the run went.
#
# Usage:
#
#   source "${repository_root}/.woodpecker/scripts/tree-state.sh"
#   evidence_tree_state "the M1 compiler receipt"
#   ... then include ${evidence_tree_*} in the receipt JSON.
#
# WHAT THIS DOES NOT COVER. Five of the eight evidence generators call it. These
# three do not, and the reason is the same in each: the guard has to run before
# anything it protects, and there is no usable repository root at that point.
#
#   m1-dns-compiler.sh           resolves its root at line 145, inside a
#                                function
#   m0-uos-dns-qualification.sh  establishes no root variable at all; writes
#                                only when M0_UOS_RECEIPT_OUTPUT is set
#   m3-dns-qualification.sh      the same, gated on M3_LIFECYCLE_RECEIPT_OUTPUT
#
# What unblocks them is being able to EXECUTE them. The five that are wired were
# each watched refusing on a real dirty tree; these three cannot be, because the
# pipeline that exercises them is down. A call site never seen to run is the
# shape of change that looks like coverage and is not, so they are left undone
# and written down rather than done blind.
#
# tree-state-coverage_test.sh enforces this list: it fails if one of the three
# quietly gains the guard, or if a new evidence generator appears in neither
# list.
#
# SEPARATELY: thirteen of the nineteen evidence artifacts under build/ have no
# producer at all, including all five wave receipts, which are maintained by
# hand. A guard on generators cannot protect an artifact that has none. That is
# a different defect and is recorded as its own item.

# evidence_tree_state sets evidence_tree_status, evidence_tree_commit and
# evidence_tree_dirty_files, or ends the run.
evidence_tree_state() {
    local what=${1:?evidence_tree_state needs the artifact it is guarding}
    local root
    root=$(git rev-parse --show-toplevel 2>/dev/null) || {
        printf '%s: not a git repository, so the tree state cannot be recorded\n' "${what}" >&2
        exit 1
    }

    evidence_tree_commit=$(git -C "${root}" rev-parse HEAD 2>/dev/null) || {
        printf '%s: HEAD cannot be resolved, so the evidence cannot name its commit\n' "${what}" >&2
        exit 1
    }

    local dirty
    dirty=$(git -C "${root}" status --porcelain 2>/dev/null)

    if [[ -z ${dirty} ]]; then
        evidence_tree_status=clean
        evidence_tree_dirty_files=""
        return 0
    fi

    # Untracked files count. The inventory failure was untracked evidence files
    # being digested before they were added, so excluding them would leave the
    # exact hole this exists to close.
    if [[ -z ${EVIDENCE_ALLOW_DIRTY_TREE:-} ]]; then
        printf '%s: refusing to generate evidence from a dirty tree.\n\n' "${what}" >&2
        printf '%s\n\n' "${dirty}" >&2
        printf '    The artifact would pin content that is in no commit -- honest about\n' >&2
        printf '    what it saw and wrong about what exists, and it heals silently once\n' >&2
        printf '    the files land, so the window where it was wrong leaves no trace.\n\n' >&2
        printf '    Commit the changes, or set EVIDENCE_ALLOW_DIRTY_TREE=1 to proceed --\n' >&2
        printf '    which records the dirty state IN the artifact rather than hiding it.\n' >&2
        exit 1
    fi

    evidence_tree_status=dirty
    evidence_tree_dirty_files=${dirty}
    printf '%s: generating from a DIRTY tree, as permitted by EVIDENCE_ALLOW_DIRTY_TREE.\n' "${what}" >&2
    printf '  The artifact will record this. %d path(s) differ from %s.\n' \
        "$(printf '%s\n' "${dirty}" | wc -l | tr -d ' ')" "${evidence_tree_commit}" >&2
}

# evidence_tree_json renders the fields for embedding in a receipt. A single
# object so a consumer can find the whole claim in one place, and so adding a
# field later does not mean editing every generator.
evidence_tree_json() {
    local files=()
    if [[ -n ${evidence_tree_dirty_files:-} ]]; then
        local line
        while IFS= read -r line; do
            [[ -n ${line} ]] && files+=("${line}")
        done <<<"${evidence_tree_dirty_files}"
    fi

    if [[ ${#files[@]} -eq 0 ]]; then
        jq -n --arg status "${evidence_tree_status:-unknown}" \
            --arg commit "${evidence_tree_commit:-}" \
            '{status: $status, commit: $commit, dirty_paths: []}'
        return
    fi
    printf '%s\n' "${files[@]}" | jq -R . | jq -s \
        --arg status "${evidence_tree_status:-unknown}" \
        --arg commit "${evidence_tree_commit:-}" \
        '{status: $status, commit: $commit, dirty_paths: .}'
}
