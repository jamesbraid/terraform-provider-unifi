#!/usr/bin/env bash
set -euo pipefail

readonly receipt=${1:?controller receipt is required}

jq -e '
  .result == "blocked_evidence" and
  .released.result == "pass" and
  .candidate.result == "pass" and
  .plan.surface_count == 67 and
  .plan.evidence_gap_count == 10
' "${receipt}" >/dev/null

case $(jq -r '.plan.diagnostic_selection // false' "${receipt}") in
    true)
        jq -e '
          .plan.catalog_test_count == 150 and
          (.plan.test_names | length) > 0 and
          (.plan.test_names | length) < .plan.catalog_test_count
        ' "${receipt}" >/dev/null
        printf '%s\n' diagnostic_complete
        ;;
    false)
        jq -e '
          (.plan | has("diagnostic_selection") | not) and
          (.plan.test_names | length) == 150 and
          .plan.allowed_skips == [
            "TestAccSettingResource_dohCustomServers",
            "TestAccSettingResource_ipsHoneypot",
            "TestAccWLANList_basic"
          ]
        ' "${receipt}" >/dev/null
        printf '%s\n' full
        ;;
    *)
        echo "controller receipt has an invalid diagnostic selection marker" >&2
        exit 1
        ;;
esac
