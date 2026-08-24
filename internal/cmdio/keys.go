package cmdio

import "sort"

// SortedKeys returns a map's keys in sorted order. Determinism is the point
// rather than tidiness: these keys are iterated to build generated files
// and receipt fields, and Go's map order is randomised, so an unsorted
// range produces a different artifact every run.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
