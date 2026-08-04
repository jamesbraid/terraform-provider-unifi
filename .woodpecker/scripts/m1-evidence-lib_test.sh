#!/usr/bin/env bash
set -euo pipefail

script_directory=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
# shellcheck source=.woodpecker/scripts/m1-evidence-lib.sh
source "${script_directory}/m1-evidence-lib.sh"

work_root=$(mktemp -d "${TMPDIR:-/tmp}/m1-evidence-test.XXXXXX")
cleanup() {
    rm -rf "${work_root}"
}
trap cleanup EXIT

mkdir -p "${work_root}/repository" "${work_root}/stale-target"
printf 'stale\n' >"${work_root}/stale-target/old.json"
ln -s "${work_root}/stale-target" "${work_root}/evidence-link"
if prepare_evidence_directory "${work_root}/evidence-link" "${work_root}/repository" >/dev/null 2>&1; then
    echo "terminal symlink evidence directory was accepted" >&2
    exit 1
fi

mkdir -p "${work_root}/bundle/raw"
printf 'schema\n' >"${work_root}/bundle/raw/schema.json"
(
    cd "${work_root}/bundle"
    sha256sum raw/schema.json >SHA256SUMS
)
printf 'stale\n' >"${work_root}/bundle/old.json"
if archive_evidence_directory "${work_root}/bundle" "${work_root}/bad.tar.gz" >/dev/null 2>&1; then
    echo "unmanifested archive member was accepted" >&2
    exit 1
fi
test ! -e "${work_root}/bad.tar.gz"

rm "${work_root}/bundle/old.json"
archive_evidence_directory "${work_root}/bundle" "${work_root}/good.tar.gz"
tar -tzf "${work_root}/good.tar.gz" | LC_ALL=C sort >"${work_root}/members"
printf '%s\n' SHA256SUMS raw/schema.json | LC_ALL=C sort >"${work_root}/expected-members"
cmp "${work_root}/expected-members" "${work_root}/members"
