package providercompiler

import (
	"encoding/json"
	"fmt"
	"sort"
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
// (ownership, discarded, coercions), and the SDK growing it must not refuse
// every compile.
type behaviorFacts struct {
	Writes map[string]behaviorWrites `json:"writes"`
	// Empty is the empty-write family, keyed by controller collection: for
	// each field, what the controller does with a literal "" and with the
	// field left out of the request. Consumed to suppress an empty write
	// where the controller refuses one but clears on omission.
	Empty map[string]map[string]behaviorEmptyDisposition `json:"empty"`
	// EmptyWhen is the per-discriminator empty family: the same measurement as
	// Empty, but for a field whose disposition varies by a sibling
	// discriminator's value, keyed collection -> field -> "wire=value" ->
	// disposition. A field carries an EmptyWhen entry exactly when its flat
	// Empty omit verdict is OMIT-VARIES-BY-TYPE, the sentinel that says the one
	// flat value would be false for at least one branch. Consumed into a
	// discriminator-conditional write guard.
	EmptyWhen map[string]map[string]map[string]behaviorEmptyDisposition `json:"empty_when"`
}

// behaviorWrites is one SDK type's measured write behaviour. required_on_create,
// min_items and the update path are derived; the verbs stay with the SDK's own
// clients.
type behaviorWrites struct {
	RequiredOnCreate []string `json:"required_on_create"`
	// UpdatePath is the controller path the masked update addresses. Kept so
	// a surface with no bootstrap collection (a v2-API resource such as nat)
	// can still be tied to its empty-family entry by the path's collection
	// segment.
	UpdatePath string `json:"update_path,omitempty"`
	// MinItems records the minimum element count the controller requires on a
	// list or set wire, keyed by the same dotted structural path
	// required_on_create uses (areas[].network_ids). Derived into a plan-time
	// size validator.
	MinItems map[string]int `json:"min_items,omitempty"`
}

// behaviorEmptyDisposition is one field's measured empty-write behaviour: what
// the controller does with a literal "" (empty) and with the field left out
// of the request (omit). EMPTY-REJECTED with OMIT-CLEARS is the pair that
// wants suppression -- the controller refuses the "" but clears on omission,
// so a field carrying "" must omit rather than send it.
type behaviorEmptyDisposition struct {
	Empty string `json:"empty"`
	Omit  string `json:"omit"`
}

// The empty-family verdicts this compiler acts on. A field the controller
// refuses an empty write on (EMPTY-REJECTED) but clears when omitted
// (OMIT-CLEARS) is the one pair suppression is both necessary and safe for:
// necessary because sending "" is refused, safe because omitting it clears.
const (
	emptyRejected = "EMPTY-REJECTED"
	omitClears    = "OMIT-CLEARS"
	// omitVariesByType is the flat omit sentinel for a field whose omit verdict
	// is not one value but depends on a sibling discriminator. It never matches
	// the suppression pair on its own; it routes the field to its EmptyWhen
	// branch, where the real per-value verdicts live.
	omitVariesByType = "OMIT-VARIES-BY-TYPE"
	// omitOK: the controller accepts the field left out. Paired with
	// EMPTY-REJECTED for a branch, it means that discriminator value cannot
	// carry the field at all -- it must be omitted from the write.
	omitOK = "OMIT-OK"
	// omitRejected: the controller refuses the field left out, so that
	// discriminator value must always carry it.
	omitRejected = "OMIT-REJECTED"
)

// conditionalOmitWire is the derived write rule for a field whose omit verdict
// varies by a discriminator (flat omit OMIT-VARIES-BY-TYPE). The write is
// omitted when the discriminator wire equals one of OmitWhenValues and sent
// otherwise. Wires are structural names; the emitter resolves them to model
// members.
type conditionalOmitWire struct {
	DiscriminatorWire string   `json:"discriminator_wire"`
	OmitWhenValues    []string `json:"omit_when_values"`
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

// behaviorEmptySuppressedWires resolves the artifact's empty family for the
// surface's controller collection and returns the lead-struct wires whose
// empty write must be suppressed: those the controller refuses an empty value
// on (EMPTY-REJECTED) but clears when omitted (OMIT-CLEARS). A rule carrying
// no value for such a field must omit it rather than send "". Nil when there
// is no artifact, the surface never writes, no empty family names the
// surface's collection, or that collection cannot be resolved.
//
// A field the controller refuses both empty and omitted (OMIT-REJECTED) is
// deliberately left out: omitting it is not safe, so there is no suppression
// to derive -- that field simply must always carry a value.
func behaviorEmptySuppressedWires(behavior []byte, kind SurfaceKind, source bootstrap) (map[string]struct{}, error) {
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
	if kind != ManagedResource || len(facts.Empty) == 0 {
		return nil, nil
	}
	collection := emptyFamilyCollection(source, facts.Writes)
	if collection == "" {
		return nil, nil
	}
	fields, named := facts.Empty[collection]
	if !named {
		return nil, nil
	}
	suppressed := map[string]struct{}{}
	for wire, disposition := range fields {
		if disposition.Empty == emptyRejected && disposition.Omit == omitClears {
			suppressed[wire] = struct{}{}
		}
	}
	if len(suppressed) == 0 {
		return nil, nil
	}
	return suppressed, nil
}

// behaviorConditionalOmitWires resolves, for the surface's controller
// collection, the wires whose empty family varies by a discriminator: a flat
// omit verdict of OMIT-VARIES-BY-TYPE, with the real per-value verdicts in the
// EmptyWhen branch. Each rule names the discriminator wire and the
// discriminator values the field must be omitted for -- a branch the
// controller accepts omitted (OMIT-OK) but refuses any value on
// (EMPTY-REJECTED), so the field cannot exist for that value. Values the
// controller requires the field on (OMIT-REJECTED) are sent. Nil when there is
// no artifact, the surface never writes, or no field on it varies.
//
// It refuses a branch it cannot model rather than guess: an omit verdict
// outside {OMIT-OK, OMIT-REJECTED}, an empty verdict other than
// EMPTY-REJECTED, a key that is not "wire=value", a field marked
// OMIT-VARIES-BY-TYPE with no branch, or two branches naming different
// discriminator wires for one field.
func behaviorConditionalOmitWires(behavior []byte, kind SurfaceKind, source bootstrap) (map[string]conditionalOmitWire, error) {
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
	if kind != ManagedResource || len(facts.Empty) == 0 {
		return nil, nil
	}
	collection := emptyFamilyCollection(source, facts.Writes)
	if collection == "" {
		return nil, nil
	}
	fields, named := facts.Empty[collection]
	if !named {
		return nil, nil
	}
	rules := map[string]conditionalOmitWire{}
	for wire, disposition := range fields {
		if disposition.Omit != omitVariesByType {
			continue
		}
		branches, ok := facts.EmptyWhen[collection][wire]
		if !ok || len(branches) == 0 {
			return nil, fmt.Errorf(
				"behaviour artifact: %s.%s carries omit %s but no empty_when branch to resolve it",
				collection, wire, omitVariesByType)
		}
		rule := conditionalOmitWire{}
		for key, branch := range branches {
			discriminator, value, found := strings.Cut(key, "=")
			if !found || discriminator == "" || value == "" {
				return nil, fmt.Errorf(
					"behaviour artifact: %s.%s empty_when key %q is not \"wire=value\"", collection, wire, key)
			}
			if rule.DiscriminatorWire == "" {
				rule.DiscriminatorWire = discriminator
			} else if rule.DiscriminatorWire != discriminator {
				return nil, fmt.Errorf(
					"behaviour artifact: %s.%s empty_when mixes discriminators %q and %q",
					collection, wire, rule.DiscriminatorWire, discriminator)
			}
			if branch.Empty != emptyRejected {
				return nil, fmt.Errorf(
					"behaviour artifact: %s.%s branch %q has empty %q, only %s is modelled",
					collection, wire, key, branch.Empty, emptyRejected)
			}
			switch branch.Omit {
			case omitOK:
				rule.OmitWhenValues = append(rule.OmitWhenValues, value)
			case omitRejected:
				// The controller requires the field for this value; it is sent.
			default:
				return nil, fmt.Errorf(
					"behaviour artifact: %s.%s branch %q has omit %q, only %s and %s are modelled",
					collection, wire, key, branch.Omit, omitOK, omitRejected)
			}
		}
		sort.Strings(rule.OmitWhenValues)
		rules[wire] = rule
	}
	if len(rules) == 0 {
		return nil, nil
	}
	return rules, nil
}

// emptyFamilyCollection resolves the controller collection the empty family
// keys a surface by. A REST-API surface carries it in the bootstrap; a v2-API
// surface (nat) carries none there, so fall back to the collection segment of
// the lead struct's measured update path.
func emptyFamilyCollection(source bootstrap, writes map[string]behaviorWrites) string {
	if source.Resource.Collection != "" {
		return source.Resource.Collection
	}
	entry, measured := writes[source.Resource.Struct]
	if !measured {
		return ""
	}
	return pathCollection(entry.UpdatePath)
}

// pathCollection is the collection segment of a controller path: the last
// segment that is neither empty nor a {placeholder}. For
// v2/api/site/{site}/nat/{id} it is "nat"; for
// api/s/{site}/rest/networkconf/{id} it is "networkconf".
func pathCollection(path string) string {
	segments := strings.Split(path, "/")
	for i := len(segments) - 1; i >= 0; i-- {
		segment := segments[i]
		if segment == "" || strings.HasPrefix(segment, "{") {
			continue
		}
		return segment
	}
	return ""
}

// behaviorMinItems resolves the artifact's min_items facts for the surface's
// lead struct: each names a list or set wire the controller requires a minimum
// number of elements in. The keys are canonicalised to the dotted structural
// path the policy build uses -- the array marker "[]" dropped, since a minimum
// applies to the collection whether it stands alone or repeats. A path the
// catalog does not observe is refused, exactly as an unobserved
// required_on_create wire is: the artifact and bootstrap resolve from one
// module, so disagreement is staleness. Nil when there is no artifact, the
// surface never writes, or the lead struct records no min_items.
func behaviorMinItems(behavior []byte, kind SurfaceKind, source bootstrap, sourceFields map[string]bootstrapField) (map[string]int, error) {
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
	if kind != ManagedResource {
		return nil, nil
	}
	entry, measured := facts.Writes[source.Resource.Struct]
	if !measured || len(entry.MinItems) == 0 {
		return nil, nil
	}
	resolved := map[string]int{}
	for wire, count := range entry.MinItems {
		if !requiredWireObserved(sourceFields, "", wire) {
			return nil, fmt.Errorf(
				"the behaviour artifact records min_items on %q of %s, but the catalog does not "+
					"observe that field; the artifact and the bootstrap disagree",
				wire, source.Resource.Struct,
			)
		}
		resolved[strings.ReplaceAll(wire, "[]", "")] = count
	}
	return resolved, nil
}

// applyMinItemsValidators injects a list or set SizeAtLeast validator into
// every policy field whose structural path the artifact records a min_items
// count for. It walks the field tree the way the build does, so a nested
// member (areas.network_ids) is reached at the path its min_items key names.
// The validator lands in the field's own policy attribute before the build
// reads it, exactly as a required-on-create override does. applied records
// which paths it reached, so the caller can notice a min_items fact whose
// field the policy omitted rather than drop it silently.
func applyMinItemsValidators(fields []fieldPolicy, prefix string, minItems map[string]int, applied map[string]bool) error {
	for i := range fields {
		field := &fields[i]
		if field.StructuralName == "" {
			continue
		}
		path := field.StructuralName
		if prefix != "" {
			path = prefix + "." + field.StructuralName
		}
		if count, marked := minItems[path]; marked && field.Disposition == "managed" {
			injected, err := injectSizeAtLeast(path, field.TerraformType, field.Attribute, count)
			if err != nil {
				return err
			}
			field.Attribute = injected
			applied[path] = true
		}
		if len(field.Fields) > 0 {
			if err := applyMinItemsValidators(field.Fields, path, minItems, applied); err != nil {
				return err
			}
		}
	}
	return nil
}

// providerFilledWires resolves the policy's provider-filled-wire opt-outs
// against the required-on-create set. Each names a wire the controller
// refuses a create without, but the provider supplies (the descriptor lists
// it in AlwaysWire and a BeforeSend fills a default), so a config may omit it
// and the attribute stays user-optional -- it must not be forced Required.
// The returned set is keyed exactly as requiredWires is.
//
// An opt-out is refused when it names a wire the artifact does not mark
// required on create: the exception would then lift a force that never fires,
// reading as an exception where there is none, and it would linger unnoticed
// once the SDK stops requiring the wire. This is per-declared-wire only --
// every required wire the policy does not name stays forced. The reason is
// required for the same purpose the claim's is: the compiler cannot check the
// fill, so a bare flag would assert the exception with nothing behind it.
func providerFilledWires(declared []providerFilledWire, requiredWires map[string]struct{}) (map[string]struct{}, error) {
	if len(declared) == 0 {
		return nil, nil
	}
	filled := make(map[string]struct{}, len(declared))
	for _, wire := range declared {
		if wire.StructuralName == "" {
			return nil, fmt.Errorf("a provider_filled_wires entry names no structural field")
		}
		if strings.TrimSpace(wire.Reason) == "" {
			return nil, fmt.Errorf(
				"provider_filled_wires entry for %q gives no reason; the provider fill it stands "+
					"in for must be stated", wire.StructuralName)
		}
		key := qualifyField(wire.StructuralSource, wire.StructuralName)
		if _, required := requiredWires[key]; !required {
			return nil, fmt.Errorf(
				"policy opts %q out of required-on-create forcing, but the behaviour artifact does "+
					"not mark it required on create; the opt-out is stale", key)
		}
		if _, dup := filled[key]; dup {
			return nil, fmt.Errorf("provider_filled_wires names %q twice", key)
		}
		filled[key] = struct{}{}
	}
	return filled, nil
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
