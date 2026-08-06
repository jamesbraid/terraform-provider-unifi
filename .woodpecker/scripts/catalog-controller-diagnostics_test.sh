#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly script=${repository_root}/.woodpecker/scripts/catalog-controller-diagnostics.sh
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-diagnostics-test.XXXXXX")
trap 'rm -rf "${work_root}"' EXIT

cat >"${work_root}/input.jsonl" <<'EOF'
{"Action":"output","Test":"TestAccDeviceFramework_basic","Output":"Error: request to https://controller.example.invalid/api failed for 192.0.2.10 and 02:aa:bb:cc:dd:ee at /woodpecker/src/example/device_resource_test.go:42\n"}
{"Action":"output","Test":"TestAccOther","Output":"must not be emitted\n"}
EOF

bash "${script}" TestAccDeviceFramework_basic <"${work_root}/input.jsonl" >"${work_root}/output.txt"

if ! grep -F 'Error: request to [redacted-url] failed for [redacted-ip] and [redacted-mac] at [redacted-workspace]/device_resource_test.go:42' \
    "${work_root}/output.txt" >/dev/null; then
    echo "diagnostic output did not match its redacted form" >&2
    sed -n '1,40p' "${work_root}/output.txt" >&2
    exit 1
fi
if grep -F 'must not be emitted' "${work_root}/output.txt"; then
    echo "diagnostic filter emitted another test's output" >&2
    exit 1
fi
if grep -E 'controller\.example\.invalid|192\.0\.2\.10|02:aa:bb:cc:dd:ee|/woodpecker/src/example' \
    "${work_root}/output.txt"; then
    echo "diagnostic filter retained restricted identifiers" >&2
    exit 1
fi
