package cmdio

import "sort"

// SortedKeys returns a map's keys in sorted order.
//
// SEVEN COPIES, THREE NAMES, FOUR VALUE TYPES, ONE BEHAVIOUR. It was written as
// sortedKeys in five places, sortedKeysOf in one and keys in one, over
// map[string]string, map[string]int, map[string]bool and map[string]any. Every
// one allocates a slice of the keys, sorts it with sort.Strings, and returns it.
//
// THIS WAS NEARLY SKIPPED ON A WRONG MEASUREMENT. A shape-hash over normalised
// syntax reported four distinct behaviours across the copies, because it does
// not collapse a type parameter, a delegated call or an exported name. Reading
// the seven showed one function wearing three names -- the inverse of what the
// count said. The hash says where to look; it is not evidence about what is
// there.
//
// Determinism is the point rather than tidiness: these keys are iterated to
// build generated files and receipt fields, and Go's map order is randomised, so
// an unsorted range produces a different artifact every run.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
