package providercompiler

import (
	"fmt"
	"strings"
	"testing"
)

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

func jsonArray(value any) []any {
	array, ok := value.([]any)
	if !ok {
		panic(fmt.Sprintf("expected a JSON array, got %T: %v", value, value))
	}
	return array
}

func jsonString(value any) string {
	text, ok := value.(string)
	if !ok {
		panic(fmt.Sprintf("expected a JSON string, got %T: %v", value, value))
	}
	return text
}

func jsonBool(value any) bool {
	flag, ok := value.(bool)
	if !ok {
		panic(fmt.Sprintf("expected a JSON boolean, got %T: %v", value, value))
	}
	return flag
}

// TestTheJSONHelpersRefuseTheWrongType is here because the helpers replaced 104
// assertions and are now the only thing standing between a mistyped fixture and
// a confusing panic. Their failure branch had never been executed.
//
// Only this package carries the test. The other three copies are the same six
// lines, and a second copy of this test would assert the same thing about the
// same code without covering a case this one misses.
func TestTheJSONHelpersRefuseTheWrongType(t *testing.T) {
	for _, c := range []struct {
		name, says string
		call       func()
	}{
		{"an object that is a string", "expected a JSON object", func() { jsonObject("not an object") }},
		{"an array that is a map", "expected a JSON array", func() { jsonArray(map[string]any{}) }},
		{"a string that is a number", "expected a JSON string", func() { jsonString(1.0) }},
		{"a boolean that is nil", "expected a JSON boolean", func() { jsonBool(nil) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				raised := recover()
				if raised == nil {
					t.Fatal("the helper returned instead of refusing, so a mistyped fixture " +
						"would reach the assertion below it as a zero value")
				}
				// The TYPE FOUND is the half that makes the message worth
				// having; "expected a JSON object" alone leaves the reader
				// where the interface-conversion panic did.
				if message := fmt.Sprint(raised); !strings.Contains(message, c.says) ||
					!strings.Contains(message, "got") {
					t.Fatalf("panicked with %q, want it to say %q and what it found", message, c.says)
				}
			}()
			c.call()
		})
	}
}
