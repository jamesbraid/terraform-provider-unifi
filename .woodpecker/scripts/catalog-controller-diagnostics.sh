#!/usr/bin/env bash
set -euo pipefail

readonly test_name=${1:?test name is required}

jq -r --arg test_name "${test_name}" '
  select(.Test == $test_name and .Action == "output") | .Output
' | sed -E \
    -e 's|https?://[^[:space:]"]+|[redacted-url]|g' \
    -e 's|/woodpecker/src/[^/[:space:]]+|[redacted-workspace]|g' \
    -e 's|([[:xdigit:]]{2}:){5}[[:xdigit:]]{2}|[redacted-mac]|g' \
    -e 's|([0-9]{1,3}\.){3}[0-9]{1,3}|[redacted-ip]|g'
