#!/usr/bin/env bash
# Single home for the go-unifi dependency pin.
#
# Sourced by the proxy bootstrap and by the publishability gate. Both used to
# carry their own copies of these facts, so a repoint that updated one and not
# the other produced a green tree locally and a failure an hour into CI. That
# is the whole reason this file exists.
#
# The version is DERIVED. Which version we depend on is a decision, and go.mod
# is where that decision is recorded; restating it here would create exactly
# the second home this file removes.
#
# The commit and the sum are DECLARED, because they are separate claims rather
# than restatements: that the tag we pin still resolves to the commit we
# reviewed, and that the module archive still hashes to what we recorded. Those
# catch a tag moved underneath us and a rebuilt archive, neither of which
# go.mod can tell us.

go_unifi_module_path=github.com/ubiquiti-community/go-unifi
go_unifi_module_origin=https://github.com/ubiquiti-community/go-unifi
go_unifi_expected_commit=${GO_UNIFI_EXPECTED_COMMIT:-a58839fe296859bbb0e91bd57efe54f9e954fe4e}
go_unifi_expected_sum=${GO_UNIFI_EXPECTED_SUM:-h1:12Qa0zjI2Rn8FT4lnieWrpXYdy29ALIDoy/afKXOohY=}
readonly go_unifi_module_path go_unifi_module_origin
readonly go_unifi_expected_commit go_unifi_expected_sum

# go_unifi_declared_version reads the required version straight out of go.mod.
# Deliberately not `go list`: this must report what the file DECLARES, so the
# publishability gate can compare it against what the build RESOLVES and notice
# a minimal-version-selection upgrade or a replace directive.
go_unifi_declared_version() {
    # Deliberately not named repository_root: both callers declare that
    # readonly at global scope, and bash refuses to shadow a readonly with a
    # local. The failure is not fatal, so the function would silently read the
    # caller's global instead of its argument.
    local root=$1
    local version
    version=$(awk -v path="${go_unifi_module_path}" \
        '$1 == path { print $2; exit }' "${root}/go.mod")
    if [[ -z ${version} ]]; then
        printf 'go.mod declares no requirement on %s\n' "${go_unifi_module_path}" >&2
        return 1
    fi
    printf '%s\n' "${version}"
}

# go_unifi_declared_sum reads the module hash go.sum recorded for that version.
go_unifi_declared_sum() {
    local root=$1
    local version=$2
    local sum
    sum=$(awk -v path="${go_unifi_module_path}" -v version="${version}" \
        '$1 == path && $2 == version { print $3; exit }' "${root}/go.sum")
    if [[ -z ${sum} ]]; then
        printf 'go.sum records no hash for %s %s\n' "${go_unifi_module_path}" "${version}" >&2
        return 1
    fi
    printf '%s\n' "${sum}"
}
