#!/usr/bin/env bash
set -euo pipefail

repository_root=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd -P)
readonly repository_root
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-evidence-test.XXXXXX")
readonly work_root
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

output=${work_root}/catalog-evidence-inventory.json
CATALOG_EVIDENCE_OUTPUT=${output} \
    bash "${repository_root}/.woodpecker/scripts/catalog-evidence-inventory.sh"

test "$(jq -r '.surfaces | length' "${output}")" = 67
test "$(jq -r '.coverage_counts.scenario_owner' "${output}")" = 67
test "$(jq -r '.coverage_counts.action_acceptance' "${output}")" = 1

committed=${repository_root}/build/release-ready/catalog-evidence-inventory.json
readonly committed
if cmp -s "${committed}" "${output}"; then
    exit 0
fi

# cmp alone reports "differ: char 399, line 1", which tells a reader nothing
# about what went stale and sends them diffing a 98KB single-line JSON file by
# hand. The failure mode here is an artifact that stays internally consistent
# while describing a tree that no longer exists, so the report has to name the
# surfaces it now describes wrongly.
status_counts() {
    jq -r '[.surfaces[].runtime.status] | group_by(.) |
           map("\(.[0])=\(length)") | join(" ")' "$1"
}
printf 'the committed evidence inventory no longer describes this tree.\n\n' >&2
printf '  committed runtime status: %s\n' "$(status_counts "${committed}")" >&2
printf '  measured  runtime status: %s\n\n' "$(status_counts "${output}")" >&2

drift=$(jq -n --slurpfile a "${committed}" --slurpfile b "${output}" '
  ($b[0].surfaces | INDEX("\(.kind)/\(.name)")) as $measured |
  [$a[0].surfaces[] |
    "\(.kind)/\(.name)" as $key |
    select($measured[$key] != null and
           $measured[$key].runtime.status != .runtime.status) |
    "\($key): committed \(.runtime.status), measured \($measured[$key].runtime.status)"]')
report() {
    local label=$1 body=$2 count
    count=$(jq -r 'length' <<<"${body}")
    [[ ${count} -eq 0 ]] && return 0
    printf '  %s %s:\n' "${count}" "${label}" >&2
    jq -r '.[0:8][] | "    " + .' <<<"${body}" >&2
    if [[ ${count} -gt 8 ]]; then
        printf '    ... and %s more\n' "$(( count - 8 ))" >&2
    fi
    printf '\n' >&2
    return 1
}

# Status drift is one cause of a stale inventory, not the only one. A digest
# moves whenever a runtime file changes, and reporting only status printed
# "0 surface(s) disagree" while still exiting non-zero -- naming the artifact
# but not what went stale inside it.
digests=$(jq -n --slurpfile a "${committed}" --slurpfile b "${output}" '
  ($b[0].surfaces | INDEX("\(.kind)/\(.name)")) as $measured |
  [$a[0].surfaces[] |
    "\(.kind)/\(.name)" as $key |
    .runtime as $was | $measured[$key].runtime as $now |
    select($now != null and $now != $was and $now.status == $was.status) |
    [$was | keys_unsorted[] | select($was[.] != $now[.])] as $fields |
    "\($key): \($fields | join(", "))"]')

roots=$(jq -n --slurpfile a "${committed}" --slurpfile b "${output}" '
  [$a[0] | to_entries[] | select((.value | type) != "array") |
   select(.value != $b[0][.key]) | .key]')

explained=0
report "surface(s) disagree on runtime status" "${drift}" || explained=1
report "surface(s) whose runtime digest moved" "${digests}" || explained=1
report "top-level field(s) that moved" "${roots}" || explained=1

# A failure that cannot say what differs sends the reader to diff a 98KB
# single-line JSON by hand. Say so outright rather than exiting on silence.
if [[ ${explained} -eq 0 ]]; then
    printf '  The artifacts differ in a way this test cannot describe.\n' >&2
    printf '  Compare them directly:\n' >&2
    printf '    diff <(jq -S . %s) <(jq -S . %s)\n\n' "${committed}" "${output}" >&2
fi

printf '  Regenerate with .woodpecker/scripts/catalog-evidence-inventory.sh\n' >&2
printf '  and check what else is pinned to it before committing the result.\n' >&2
exit 1
