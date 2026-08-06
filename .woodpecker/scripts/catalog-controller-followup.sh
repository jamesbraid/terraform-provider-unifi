#!/usr/bin/env bash
set -euo pipefail

readonly receipt=${1:?controller receipt is required}

jq -e '
  def released_result_accepted:
    ((.released.result == "pass") and
     ((.released.accepted_failures // []) == []) and
     ((.released.unexpected_failures // []) == []) and
     ((.released.failed // []) == []) and
     ((.released.missing // []) == [])) or
    ((.released.result == "accepted_limitation") and
     (.released.accepted_failures == .plan.released_allowed_failures) and
     (.released.failed == .plan.released_allowed_failures) and
     (.released.unexpected_failures == []) and
     (.released.missing == []));
  .result == "blocked_evidence" and
  released_result_accepted and
  .candidate.result == "pass" and
  ((.candidate.accepted_failures // []) == []) and
  ((.candidate.unexpected_failures // []) == []) and
  ((.candidate.failed // []) == []) and
  ((.candidate.missing // []) == []) and
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
          ] and
          .plan.released_allowed_failures == ["TestAccDeviceFramework_basic"]
        ' "${receipt}" >/dev/null
        printf '%s\n' full
        ;;
    *)
        echo "controller receipt has an invalid diagnostic selection marker" >&2
        exit 1
        ;;
esac
