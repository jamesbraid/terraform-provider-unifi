package schemaparity

import "fmt"

// jsonObject and its siblings replace unchecked type assertions on decoded JSON.
//
// WHY THE ASSERTIONS WENT AWAY. forcetypeassert reported 104 of them across the
// tree, every one in a test file, and that count was what kept the linter off
// the fast-loop gate -- so the production code it exists to guard went unguarded
// because the tests were noisy. The assertion still happens. It happens once,
// here, where the failure can say what it found instead of only what it wanted.
//
// THEY PANIC RATHER THAN TAKING A *testing.T, which is a decision and not
// laziness. Several callers are plain helpers with no t in scope -- firstGrouping,
// groupingMembers, firstClaim, firstFlattening, flattenedMembers -- and threading
// one through them would change five signatures and every call site to improve a
// message. A panic is also exactly what the raw assertion did: same failure, same
// stack trace, better first line.
//
// A COPY PER PACKAGE, DELIBERATELY. Go has no shared unexported test helper, and
// the alternative is a non-test package that ships in every build to serve four
// test files. A copy cannot drift silently here: each is used only by its own
// package's tests, so a wrong one fails immediately and locally.

func jsonObject(value any) map[string]any {
	object, ok := value.(map[string]any)
	if !ok {
		panic(fmt.Sprintf("expected a JSON object, got %T: %v", value, value))
	}
	return object
}
