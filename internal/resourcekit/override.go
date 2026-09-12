package resourcekit

// Override composes a generated field list with the hand-written entries
// that outrank it. Every generated entry naming a wire some override also
// names -- through any of its wires, so a scattered override displaces each
// flat entry it absorbs -- is dropped, and the overrides are appended in
// their own order. The result is what a descriptor's judgment says the
// surface does, laid over what the generator says it has.
//
// An override whose wires match no generated entry is simply appended: the
// generator skips every field it cannot derive (custom types, durations,
// nested objects), and those arrive here as ordinary additions.
func Override[M any, S any](generated, overrides []Field[M, S]) []Field[M, S] {
	claimed := map[string]bool{}
	for _, field := range overrides {
		for _, wire := range fieldWireNames(field) {
			claimed[wire] = true
		}
	}
	out := make([]Field[M, S], 0, len(generated)+len(overrides))
	for _, field := range generated {
		if claimed[field.WireName()] {
			continue
		}
		out = append(out, field)
	}
	return append(out, overrides...)
}
