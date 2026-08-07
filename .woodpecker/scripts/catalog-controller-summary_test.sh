#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly filter=${repository_root}/.woodpecker/scripts/catalog-controller-summary.jq
work_root=$(mktemp -d "${TMPDIR:-/tmp}/catalog-controller-summary-test.XXXXXX")
trap 'rm -rf "${work_root}"' EXIT

cat >"${work_root}/plan.json" <<'EOF'
{
  "test_names": ["TestAccA", "TestAccB", "TestAccC", "TestAccD"],
  "allowed_skips": ["TestAccC"],
  "released_allowed_failures": ["TestAccB"],
  "released_allowed_missing": ["TestAccD"]
}
EOF

cat >"${work_root}/known-limitation.jsonl" <<'EOF'
{"Action":"pass","Test":"TestAccA"}
{"Action":"fail","Test":"TestAccB"}
{"Action":"skip","Test":"TestAccC"}
EOF

jq --slurpfile plan "${work_root}/plan.json" \
   --arg suite_label released --argjson exit_code 1 -s -f "${filter}" \
   "${work_root}/known-limitation.jsonl" >"${work_root}/released.json"
jq -e '
  .result == "accepted_limitation" and
  .passed == ["TestAccA"] and
  .skipped == ["TestAccC"] and
  .failed == ["TestAccB"] and
  .accepted_failures == ["TestAccB"] and
  .unexpected_failures == [] and
  .missing == ["TestAccD"]
' "${work_root}/released.json" >/dev/null

jq --slurpfile plan "${work_root}/plan.json" \
   --arg suite_label candidate --argjson exit_code 1 -s -f "${filter}" \
   "${work_root}/known-limitation.jsonl" >"${work_root}/candidate.json"
jq -e '
  .result == "fail" and
  .accepted_failures == [] and
  .unexpected_failures == ["TestAccB"]
' "${work_root}/candidate.json" >/dev/null

cat >"${work_root}/extra-failure.jsonl" <<'EOF'
{"Action":"fail","Test":"TestAccA"}
{"Action":"fail","Test":"TestAccB"}
{"Action":"skip","Test":"TestAccC"}
EOF
jq --slurpfile plan "${work_root}/plan.json" \
   --arg suite_label released --argjson exit_code 1 -s -f "${filter}" \
   "${work_root}/extra-failure.jsonl" >"${work_root}/extra-failure.json"
jq -e '
  .result == "fail" and
  .accepted_failures == ["TestAccB"] and
  .unexpected_failures == ["TestAccA"] and
  .missing == ["TestAccD"]
' "${work_root}/extra-failure.json" >/dev/null

cat >"${work_root}/missing-test.jsonl" <<'EOF'
{"Action":"fail","Test":"TestAccB"}
{"Action":"skip","Test":"TestAccC"}
EOF
jq --slurpfile plan "${work_root}/plan.json" \
   --arg suite_label released --argjson exit_code 1 -s -f "${filter}" \
   "${work_root}/missing-test.jsonl" >"${work_root}/missing-test.json"
jq -e '
  .result == "fail" and
  .accepted_failures == ["TestAccB"] and
  .unexpected_failures == [] and
  .missing == ["TestAccA", "TestAccD"]
' "${work_root}/missing-test.json" >/dev/null

cat >"${work_root}/pass.jsonl" <<'EOF'
{"Action":"pass","Test":"TestAccA"}
{"Action":"pass","Test":"TestAccB"}
{"Action":"skip","Test":"TestAccC"}
{"Action":"pass","Test":"TestAccD"}
EOF
jq --slurpfile plan "${work_root}/plan.json" \
   --arg suite_label released --argjson exit_code 0 -s -f "${filter}" \
   "${work_root}/pass.jsonl" >"${work_root}/pass.json"
jq -e '
  .result == "pass" and
  .accepted_failures == [] and
  .unexpected_failures == [] and
  .missing == []
' "${work_root}/pass.json" >/dev/null
