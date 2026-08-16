#!/usr/bin/env bash
set -euo pipefail

readonly script_directory=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
readonly work_root=$(mktemp -d "${TMPDIR:-/tmp}/m3-evidence-lib-test.XXXXXX")
cleanup() {
    local result=$?
    trap - EXIT
    rm -rf "${work_root}"
    exit "${result}"
}
trap cleanup EXIT

test -f "${script_directory}/m3-evidence-lib.sh"
source "${script_directory}/m3-evidence-lib.sh"

printf '#!/bin/sh\nexit 0\n' >"${work_root}/candidate"
chmod 0755 "${work_root}/candidate"
install_prebuilt_candidate \
    "${work_root}/candidate" "${work_root}/tools/terraform-provider-unifi_v0.101.2"
test -x "${work_root}/tools/terraform-provider-unifi_v0.101.2"
cmp -s "${work_root}/candidate" "${work_root}/tools/terraform-provider-unifi_v0.101.2"

if install_prebuilt_candidate \
    "${work_root}/missing" "${work_root}/invalid/terraform-provider-unifi" 2>/dev/null; then
    echo 'prebuilt candidate installer accepted a missing binary' >&2
    exit 1
fi
