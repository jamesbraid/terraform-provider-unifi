package cmdio

import (
	"reflect"
	"testing"
)

// TestSortedKeysIsSortedForEveryValueType covers the map value types this
// generic function replaces separate copies for.
func TestSortedKeysIsSortedForEveryValueType(t *testing.T) {
	if got := SortedKeys(map[string]string{"c": "", "a": "", "b": ""}); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("map[string]string = %v, want sorted", got)
	}
	if got := SortedKeys(map[string]int{"z": 1, "y": 2}); !reflect.DeepEqual(got, []string{"y", "z"}) {
		t.Errorf("map[string]int = %v, want sorted", got)
	}
	if got := SortedKeys(map[string]bool{"b": true, "a": false}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("map[string]bool = %v, want sorted", got)
	}
	if got := SortedKeys(map[string]any{"n": nil, "m": 1}); !reflect.DeepEqual(got, []string{"m", "n"}) {
		t.Errorf("map[string]any = %v, want sorted", got)
	}
}

// TestSortedKeysIsStableAcrossRuns is the reason this exists at all. Go
// randomises map iteration order, so an unsorted range produces a different
// generated artifact on every run and a regenerate-and-diff gate that fails for
// no reason. Ten iterations is enough that random order would almost certainly
// differ at least once.
func TestSortedKeysIsStableAcrossRuns(t *testing.T) {
	m := map[string]int{"delta": 1, "alpha": 2, "charlie": 3, "bravo": 4, "echo": 5}
	first := SortedKeys(m)
	for i := 0; i < 10; i++ {
		if got := SortedKeys(m); !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d returned %v, want %v -- the order is not stable", i, got, first)
		}
	}
	if !reflect.DeepEqual(first, []string{"alpha", "bravo", "charlie", "delta", "echo"}) {
		t.Errorf("order = %v, want alphabetical", first)
	}
}

// TestSortedKeysOnAnEmptyMap returns an empty slice rather than nil, because a
// caller ranging over it or marshalling it should not have to distinguish them.
func TestSortedKeysOnAnEmptyMap(t *testing.T) {
	got := SortedKeys(map[string]string{})
	if got == nil {
		t.Error("an empty map returned nil rather than an empty slice")
	}
	if len(got) != 0 {
		t.Errorf("an empty map returned %v", got)
	}
}
