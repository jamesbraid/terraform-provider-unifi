#!/usr/bin/env bash

prepare_evidence_directory() {
    local requested_directory=$1
    local requested_repository_root=$2
    local resolved_directory resolved_repository

    if [[ -L ${requested_directory} ]]; then
        echo "M1 evidence output directory must not be a symlink" >&2
        return 1
    fi
    if [[ -e ${requested_directory} && ! -d ${requested_directory} ]]; then
        echo "M1 evidence output must be a directory" >&2
        return 1
    fi

    mkdir -p "${requested_directory}"
    resolved_directory=$(CDPATH='' cd -- "${requested_directory}" && pwd -P)
    resolved_repository=$(CDPATH='' cd -- "${requested_repository_root}" && pwd -P)
    case "${resolved_directory}" in
        "${resolved_repository}"|"${resolved_repository}"/*)
            echo "M1 evidence output must be outside the provider repository" >&2
            return 1
            ;;
    esac
    if [[ -n $(find "${resolved_directory}" -mindepth 1 -print -quit) ]]; then
        echo "M1 evidence output directory must be empty" >&2
        return 1
    fi
    printf '%s\n' "${resolved_directory}"
}

archive_evidence_directory() {
    local source_directory=$1
    local archive_path=$2
    local resolved_directory archive_parent resolved_archive temporary_directory

    resolved_directory=$(CDPATH='' cd -- "${source_directory}" && pwd -P)
    archive_parent=$(dirname -- "${archive_path}")
    mkdir -p "${archive_parent}"
    archive_parent=$(CDPATH='' cd -- "${archive_parent}" && pwd -P)
    resolved_archive=${archive_parent}/$(basename -- "${archive_path}")
    case "${resolved_archive}" in
        "${resolved_directory}"/*)
            echo "M1 evidence archive must be outside the evidence directory" >&2
            return 1
            ;;
    esac
    if [[ -e ${resolved_archive} || -L ${resolved_archive} ]]; then
        echo "M1 evidence archive output must not already exist" >&2
        return 1
    fi
    if [[ ! -f ${resolved_directory}/SHA256SUMS || -L ${resolved_directory}/SHA256SUMS ]]; then
        echo "M1 evidence checksum manifest is missing" >&2
        return 1
    fi

    temporary_directory=$(mktemp -d "${TMPDIR:-/tmp}/m1-evidence-archive.XXXXXX")
    if ! (
        cd "${resolved_directory}"
        sed -E 's/^[0-9a-f]{64} [ *]//' SHA256SUMS >"${temporary_directory}/manifest-members"
        if grep -Ev '^[^/-][^[:cntrl:]]*$' "${temporary_directory}/manifest-members" >/dev/null; then
            echo "M1 evidence manifest contains an unsafe member" >&2
            exit 1
        fi
        if grep -E '(^|/)\.\.(/|$)' "${temporary_directory}/manifest-members" >/dev/null; then
            echo "M1 evidence manifest contains a parent traversal" >&2
            exit 1
        fi
        printf '%s\n' SHA256SUMS >>"${temporary_directory}/manifest-members"
        LC_ALL=C sort -u "${temporary_directory}/manifest-members" >"${temporary_directory}/expected-members"
        find . -mindepth 1 ! -type d -print | sed 's#^\./##' | LC_ALL=C sort \
            >"${temporary_directory}/actual-members"
        if find . -type l -print -quit | grep -q .; then
            echo "M1 evidence directory contains a symlink" >&2
            exit 1
        fi
        cmp "${temporary_directory}/expected-members" "${temporary_directory}/actual-members" || exit 1
        sha256sum --check SHA256SUMS || exit 1
        tar -czf "${resolved_archive}" -T "${temporary_directory}/expected-members" || exit 1
        tar -tzf "${resolved_archive}" | LC_ALL=C sort >"${temporary_directory}/archive-members"
        cmp "${temporary_directory}/expected-members" "${temporary_directory}/archive-members" || exit 1
    ); then
        rm -rf "${temporary_directory}"
        rm -f "${resolved_archive}"
        return 1
    fi
    rm -rf "${temporary_directory}"
}
