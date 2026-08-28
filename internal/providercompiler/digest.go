package providercompiler

import (
	"bytes"
	"fmt"
	"regexp"
)

// DigestMismatchError reports a policy pinned to a bootstrap other than the
// one presented. The caller decides whether that is a bump to re-pin or
// drift to refuse; Compile itself never rewrites its inputs.
type DigestMismatchError struct {
	Bootstrap, Policy string
}

func (e *DigestMismatchError) Error() string {
	return fmt.Sprintf("bootstrap digest mismatch: bootstrap %q, policy %q", e.Bootstrap, e.Policy)
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// WellFormedDigest reports whether s is the hex of a SHA-256: the difference
// between a stale pin (re-pinnable) and a corrupt one (refused).
func WellFormedDigest(s string) bool { return digestPattern.MatchString(s) }

// RepinPolicy replaces the policy's source digest, touching nothing else, so
// the re-pin shows in review as a one-line diff. It refuses unless the old
// digest appears exactly once: a policy is not a place to guess in.
func RepinPolicy(policy []byte, from, to string) ([]byte, error) {
	if !WellFormedDigest(from) || !WellFormedDigest(to) {
		return nil, fmt.Errorf("re-pin needs two well-formed digests, got %q -> %q", from, to)
	}
	if n := bytes.Count(policy, []byte(from)); n != 1 {
		return nil, fmt.Errorf("digest %s appears %d times in the policy, want exactly once", from[:8], n)
	}
	return bytes.Replace(policy, []byte(from), []byte(to), 1), nil
}
