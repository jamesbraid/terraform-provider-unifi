package providercompiler

import (
	"fmt"
	"strings"
	"testing"
)

// jsonObject and its siblings replace unchecked type assertions on decoded
// JSON, panicking with what was found instead of only what was wanted. They
// panic rather than take a *testing.T since several callers (firstGrouping,
// groupingMembers, firstClaim, firstFlattening, flattenedMembers) have no t
// in scope. Copied per package on purpose: Go has no shared unexported test
// helper.
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

// TestTheJSONHelpersRefuseTheWrongType is the only copy of this test: the
// helper is duplicated per package, but the behaviour it checks is not.
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
				// The type found is what makes the message worth having.
				if message := fmt.Sprint(raised); !strings.Contains(message, c.says) ||
					!strings.Contains(message, "got") {
					t.Fatalf("panicked with %q, want it to say %q and what it found", message, c.says)
				}
			}()
			c.call()
		})
	}
}
