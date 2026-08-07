($plan[0].test_names) as $planned |
($plan[0].allowed_skips) as $allowed_skips |
(if $suite_label == "released"
 then ($plan[0].released_allowed_failures // [])
 else []
 end) as $allowed_failures |
(if $suite_label == "released"
 then ($plan[0].released_allowed_missing // [])
 else []
 end) as $allowed_missing |
([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
  select(.Action == "pass") | .Test] | unique) as $passed |
([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
  select(.Action == "skip") | .Test] | unique) as $skipped |
([.[] | select(.Test != null and (.Test as $test | $planned | index($test))) |
  select(.Action == "fail") | .Test] | unique) as $failed |
([$failed[] | select(. as $failure | $allowed_failures | index($failure))] | unique)
  as $accepted_failures |
($failed - $allowed_failures) as $unexpected_failures |
($planned - $passed - $skipped - $failed) as $missing |
{
  exit_code: $exit_code,
  result: (
    if $exit_code == 0 and $skipped == $allowed_skips and
       ($failed | length) == 0 and $passed == ($planned - $allowed_skips)
    then "pass"
    elif $suite_label == "released" and
         (($allowed_failures | length) > 0 or ($allowed_missing | length) > 0) and
         ((($allowed_failures | length) > 0 and $exit_code != 0) or
          (($allowed_failures | length) == 0 and $exit_code == 0)) and
         $skipped == $allowed_skips and $failed == $allowed_failures and
         $passed == ($planned - $allowed_skips - $allowed_failures - $allowed_missing) and
         $missing == $allowed_missing
    then "accepted_limitation"
    else "fail"
    end
  ),
  passed: $passed,
  skipped: $skipped,
  failed: $failed,
  accepted_failures: $accepted_failures,
  unexpected_failures: $unexpected_failures,
  missing: $missing,
  pre_test_diagnostics: (
    if ($passed | length) == 0 and ($skipped | length) == 0 and
       ($failed | length) == 0
    then [.[] | select(.Test == null and .Action == "output") | .Output] |
         unique | .[:40]
    else []
    end
  )
}
