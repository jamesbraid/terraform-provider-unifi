#!/usr/bin/env bash

install_prebuilt_candidate() {
    local source=${1:?candidate provider binary is required}
    local destination=${2:?candidate provider destination is required}

    test -f "${source}"
    test -x "${source}"
    mkdir -p "$(dirname -- "${destination}")"
    install -m 0755 "${source}" "${destination}"
    cmp -s "${source}" "${destination}"
}
