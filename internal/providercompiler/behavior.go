package providercompiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

// behaviorDocument mirrors the wrapper cmd/sdk-bootstrap writes around the
// SDK's schemas/behavior.json: the artifact verbatim, stamped with the module
// it was resolved from.
type behaviorDocument struct {
	FormatVersion int             `json:"format_version"`
	Source        bootstrapSource `json:"source"`
	Behavior      json.RawMessage `json:"behavior"`
}

// behaviorFacts is the slice of the artifact this compiler consumes. Decoded
// loosely on purpose: the artifact carries families nothing here reads yet
// (ownership, discarded, empty, coercions), and the SDK growing it must not
// refuse every compile.
type behaviorFacts struct {
	Writes map[string]behaviorWrites `json:"writes"`
}

// behaviorWrites is one SDK type's measured write behaviour. Only
// required_on_create is derived so far; the verbs and paths stay with the
// SDK's own clients.
type behaviorWrites struct {
	RequiredOnCreate []string `json:"required_on_create"`
}

// behaviorRequiredWires resolves the artifact's required_on_create facts
// against this compile's structs: the bootstrap's lead struct and its
// companions, each matched against the artifact's writes by Go type name.
// The result is keyed exactly as sourceFields is (bare for the lead,
// qualified for a companion). Nil when there is no artifact, no writes, or
// the surface never creates (only a managed resource does).
//
// A required wire the catalog does not observe is refused: the artifact and
// the bootstrap resolve from the same module, so disagreement means one of
// them is stale, not a fact to skip past.
func behaviorRequiredWires(
	behavior []byte,
	kind SurfaceKind,
	source bootstrap,
	sourceFields map[string]bootstrapField,
) (map[string]struct{}, error) {
	if len(behavior) == 0 {
		return nil, nil
	}
	var document behaviorDocument
	if err := decodeJSON("behaviour artifact", behavior, &document, true); err != nil {
		return nil, err
	}
	if document.FormatVersion != 1 {
		return nil, fmt.Errorf("unsupported behaviour artifact format %d", document.FormatVersion)
	}
	var facts behaviorFacts
	if err := decodeJSON("behaviour facts", document.Behavior, &facts, false); err != nil {
		return nil, err
	}
	if kind != ManagedResource || len(facts.Writes) == 0 {
		return nil, nil
	}
	if source.Resource.Struct == "" {
		return nil, fmt.Errorf(
			"the behaviour artifact names write behaviour by SDK struct, but the bootstrap "+
				"records no lead struct for %s; regenerate the bootstrap",
			source.Resource.Name,
		)
	}
	structs := []struct{ qualifier, name string }{{"", source.Resource.Struct}}
	for _, companion := range source.Companions {
		structs = append(structs, struct{ qualifier, name string }{companion.Struct, companion.Struct})
	}
	required := map[string]struct{}{}
	for _, entry := range structs {
		writes, measured := facts.Writes[entry.name]
		if !measured {
			continue
		}
		for _, wire := range writes.RequiredOnCreate {
			if !requiredWireObserved(sourceFields, entry.qualifier, wire) {
				return nil, fmt.Errorf(
					"the behaviour artifact marks %q required on create of %s, but the catalog "+
						"does not observe that field; the artifact and the bootstrap disagree",
					wire, entry.name,
				)
			}
			required[qualifyField(entry.qualifier, wire)] = struct{}{}
		}
	}
	return required, nil
}

// requiredWireObserved reports whether a required_on_create wire names a field
// the bootstrap observed. A flat wire is a key of sourceFields (bare for the
// lead struct, qualified for a companion). A nested wire -- the artifact's
// dotted path into an object member (source.zone_id) or an array<object>
// element member (areas[].network_ids) -- is resolved segment by segment
// against the lead struct's observed nested Fields. Companions carry no
// nested behaviour, so a qualified wire is only ever a flat companion field.
func requiredWireObserved(sourceFields map[string]bootstrapField, qualifier, wire string) bool {
	if _, ok := sourceFields[qualifyField(qualifier, wire)]; ok {
		return true
	}
	if qualifier != "" || !strings.ContainsAny(wire, ".[") {
		return false
	}
	return resolveNestedStructuralWire(sourceFields, wire)
}

// resolveNestedStructuralWire walks a dotted wire path into the lead struct's
// observed nested Fields. The "[]" marking an array<object> segment is
// dropped -- a required member of the element is required whether the element
// stands alone or repeats.
func resolveNestedStructuralWire(sourceFields map[string]bootstrapField, wire string) bool {
	segments := strings.Split(wire, ".")
	field, ok := sourceFields[strings.TrimSuffix(segments[0], "[]")]
	if !ok {
		return false
	}
	for _, segment := range segments[1:] {
		member, ok := structuralMemberField(field.Fields, strings.TrimSuffix(segment, "[]"))
		if !ok {
			return false
		}
		field = member
	}
	return true
}

// structuralMemberField finds a nested member by its observed wire name.
func structuralMemberField(fields []bootstrapField, name string) (bootstrapField, bool) {
	for _, field := range fields {
		if field.Name == name {
			return field, true
		}
	}
	return bootstrapField{}, false
}

// forceRequiredOnCreate rewrites one attribute body's disposition to
// required. The rest of the body is the policy's to keep; only requiredness
// is a measured fact here, so only it is overridden.
func forceRequiredOnCreate(attribute json.RawMessage) (json.RawMessage, error) {
	body := map[string]json.RawMessage{}
	if len(attribute) > 0 {
		if err := json.Unmarshal(attribute, &body); err != nil {
			return nil, fmt.Errorf("attribute: %w", err)
		}
	}
	body["computed_optional_required"] = json.RawMessage(`"required"`)
	return json.Marshal(body)
}
