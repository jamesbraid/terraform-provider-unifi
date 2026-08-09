#!/usr/bin/env bash
set -euo pipefail

repository_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
readonly repository_root
readonly receipt=${1:?controller receipt is required}
readonly campaign_policy=${CATALOG_CAMPAIGN_POLICY:-${repository_root}/provider-codegen/policy/catalog-campaign.json}

jq -e --slurpfile policy "${campaign_policy}" '
  $policy[0] as $campaign |
  def released_result_accepted:
    ((.released.result == "pass") and
     ((.released.accepted_failures // []) == []) and
     ((.released.unexpected_failures // []) == []) and
     ((.released.failed // []) == []) and
     ((.released.missing // []) == [])) or
    ((.released.result == "accepted_limitation") and
     (.released.accepted_failures == .released.failed) and
     ((.released.failed - .plan.released_allowed_failures) == []) and
     (.released.unexpected_failures == []) and
     (.released.missing == .plan.released_allowed_missing));
  .result == "blocked_evidence" and
  released_result_accepted and
  .candidate.result == "pass" and
  ((.candidate.accepted_failures // []) == []) and
  ((.candidate.unexpected_failures // []) == []) and
  ((.candidate.failed // []) == []) and
  ((.candidate.missing // []) == []) and
  .plan.surface_count == $campaign.surface_count and
  .plan.evidence_gap_count == $campaign.evidence_gap_count
' "${receipt}" >/dev/null

case $(jq -r '.plan.diagnostic_selection // false' "${receipt}") in
    true)
        jq -e --slurpfile policy "${campaign_policy}" '
          $policy[0] as $campaign |
          .plan.catalog_test_count == $campaign.test_name_count and
          (.plan.test_names | length) > 0 and
          (.plan.test_names | length) < .plan.catalog_test_count
        ' "${receipt}" >/dev/null
        printf '%s\n' diagnostic_complete
        ;;
    false)
        jq -e --slurpfile policy "${campaign_policy}" '
          $policy[0] as $campaign |
          (.plan | has("diagnostic_selection") | not) and
          (.plan.test_names | length) == $campaign.test_name_count and
          .plan.allowed_skips == $campaign.allowed_skips and
          .plan.released_allowed_failures == $campaign.released_allowed_failures and
          .plan.released_allowed_missing == $campaign.released_allowed_missing
        ' "${receipt}" >/dev/null
        printf '%s\n' full
        ;;
    *)
        echo "controller receipt has an invalid diagnostic selection marker" >&2
        exit 1
        ;;
esac
