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
printf '  %s surface(s) disagree on runtime status:\n' "$(jq -r 'length' <<<"${drift}")" >&2
jq -r '.[0:8][] | "    " + .' <<<"${drift}" >&2
if [[ $(jq -r 'length' <<<"${drift}") -gt 8 ]]; then
    printf '    ... and %s more\n' "$(( $(jq -r 'length' <<<"${drift}") - 8 ))" >&2
fi
printf '\n  Regenerate with .woodpecker/scripts/catalog-evidence-inventory.sh\n' >&2
printf '  and check what else is pinned to it before committing the result.\n' >&2
exit 1
