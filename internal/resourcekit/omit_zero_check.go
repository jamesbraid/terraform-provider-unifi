package resourcekit

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/controllerregex"
)

// OmitZeroProblems reports every Int64PtrField whose wire name resolves to a
// controller constraint whose pattern rejects a literal "0", and which does
// not set OmitZero. Without it, an Unknown plan value (an Optional+Computed
// field with no schema default, on create) or an explicit zero collapses to
// a non-nil pointer-to-zero that bypasses the SDK's omitempty and reaches
// the controller as a literal 0, rejected with api.err.InvalidValue. This is
// the class behind wlan's dtim_6e/dtim_na/dtim_ng and
// roaming_assistant_6e_rssi/roaming_assistant_na_rssi (R2-C Task 10b); the
// check exists so the next one is caught here instead of by a live
// acceptance run.
//
// Int64Field (the non-pointer sibling) does not share this hazard and is not
// walked: its own ToSDK skips Null and Unknown outright, so an unset value
// is never written at all and there is nothing for omitempty to bypass.
// DurationPtrField skips Null/Unknown the same way, for the same reason.
// Int64PtrField is the one kind whose ToSDK always writes a pointer, Unknown
// included, unless OmitZero says otherwise -- see its own doc comment.
//
// constraints is S's own per-struct constraint sub-map (e.g.
// unifi.FieldConstraints["WLAN"]); resourcekit has no way to name S at this
// generic boundary, so the caller resolves the lookup and hands in the result.
// Handing in the resolved sub-map, rather than the whole table plus a reflected
// type name, is what lets a surface whose constraint key differs from its Go
// type name still be checked -- unifi_setting's SDK type is settings.Radius but
// its constraints are keyed "SettingRadius".
//
// nested is the whole per-struct table (unifi.FieldConstraints) the descent
// resolves each nested block's element type against by that type's own name
// (RADIUSProfileAcctServers keys a different sub-map from RADIUSProfile). Pass
// nil to walk only the top level; a spec with no nested block never consults
// it either way.
func OmitZeroProblems[M any, S any](
	spec Spec[M, S],
	constraints map[string]ui.FieldConstraint,
	nested map[string]map[string]ui.FieldConstraint,
) []string {
	var problems []string
	for _, field := range spec.Fields {
		// Unlike ElideProblems, a read-only field is skipped rather than
		// unwrapped: readOnlyField.ToSDK is a hard no-op ("the field never
		// reaches the controller"), so there is no write path for OmitZero
		// to protect and nothing here to check. Unwrapping it anyway was
		// this check's own first bug, caught by firewall_policy.index --
		// Computed-only, wrapped in ReadOnly specifically because the
		// controller rejects any client-supplied value on create or update.
		if _, readOnly := field.(interface{ Unwrap() Field[M, S] }); readOnly {
			continue
		}
		// A nested block (ObjectField / ObjectListField) is encoded element by
		// element with no per-member OmitZero flag, so the top-level walk below
		// never reaches its members. Descend into the element type and check
		// its pointer-to-integer members against the nested struct's own
		// constraint sub-map, reusing the reflection idiom NestedProblems uses.
		if reflector, ok := field.(nestedElementReflector); ok {
			problems = append(problems, nestedOmitZeroProblems(
				spec.TypeName, reflector.nestedWireName(), reflector.nestedElementType(), nested)...)
		}
		value := reflect.ValueOf(field)
		kind := value.Type().Name()
		if i := strings.IndexByte(kind, '['); i > 0 {
			kind = kind[:i] // strip the generic instantiation
		}
		if kind != "Int64PtrField" {
			continue
		}
		constraint, ok := constraints[field.WireName()]
		if !ok || constraint.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(controllerregex.Anchored(constraint.Pattern))
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s.%s: constraint pattern %q does not compile as RE2: %v",
				spec.TypeName, field.WireName(), constraint.Pattern, err))
			continue
		}
		if re.MatchString("0") {
			continue // zero is a legal value on the wire; nothing to omit
		}
		omitZero := value.FieldByName("OmitZero")
		if omitZero.IsValid() && omitZero.Bool() {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s.%s: the controller's pattern %q rejects a literal zero but the field sets no "+
				"OmitZero, so an Unknown or unset plan value is force-emitted as the zero the "+
				"controller refuses",
			spec.TypeName, field.WireName(), constraint.Pattern))
	}
	sort.Strings(problems)
	return problems
}

// nestedOmitZeroProblems reports every pointer-to-integer member of a nested
// block's element type whose wire name resolves to a controller constraint that
// rejects a literal "0". A nested block is encoded one element at a time by the
// descriptor's Encode closure, and a field mask names top-level keys only, so
// masking the block sends the whole nested object; a member the closure
// force-emits as its Go zero -- an unset Optional+Computed number reaches Encode
// as Unknown, and types.Int64.ValueInt64Pointer() turns Unknown into a non-nil
// pointer to zero -- then reaches the controller as the zero its pattern
// refuses, exactly the top-level Int64PtrField hazard one level down.
//
// Unlike the top-level walk there is no OmitZero flag to consult: a nested
// member is not an Int64PtrField, it is a struct field the closure fills in. So
// every zero-rejecting member is reported and the descriptor author decides the
// fix -- make Encode omit the zero/unknown, or pin it as already guarded.
//
// Only pointer-to-integer members are walked, matching the top-level scope: a
// non-pointer int is the nested analog of Int64Field, whose zero is
// indistinguishable from unset and is a MaskedZeroProblems question, not this
// one.
func nestedOmitZeroProblems(
	typeName, outerWire string,
	element reflect.Type,
	constraints map[string]map[string]ui.FieldConstraint,
) []string {
	if element == nil || element.Kind() != reflect.Struct {
		return nil
	}
	sub, ok := constraints[element.Name()]
	if !ok || len(sub) == 0 {
		return nil
	}
	var problems []string
	for i := range element.NumField() {
		structField := element.Field(i)
		if !structField.IsExported() {
			continue
		}
		fieldType := structField.Type
		if fieldType.Kind() != reflect.Pointer || !isIntegerKind(fieldType.Elem().Kind()) {
			continue
		}
		tag, ok := structField.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		wire := tag
		if comma := strings.IndexByte(wire, ','); comma >= 0 {
			wire = wire[:comma]
		}
		if wire == "" {
			continue
		}
		constraint, ok := sub[wire]
		if !ok || constraint.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(controllerregex.Anchored(constraint.Pattern))
		if err != nil {
			problems = append(problems, fmt.Sprintf(
				"%s.%s.%s: constraint pattern %q does not compile as RE2: %v",
				typeName, outerWire, wire, constraint.Pattern, err))
			continue
		}
		if re.MatchString("0") {
			continue // zero is a legal value on the wire; nothing to omit
		}
		problems = append(problems, fmt.Sprintf(
			"%s.%s.%s: the controller's pattern %q rejects a literal zero but this nested member "+
				"is encoded element by element with no OmitZero, so an Unknown or unset plan value "+
				"is force-emitted as the zero the controller refuses (a field mask names top-level "+
				"keys only, so masking %s sends the whole nested object)",
			typeName, outerWire, wire, constraint.Pattern, outerWire))
	}
	return problems
}

// isIntegerKind reports whether k is one of the signed or unsigned integer
// kinds a controller numeric constraint can apply to.
func isIntegerKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

// OmitZeroProblems on the resource binds the spec to its own SDK struct's
// constraint sub-map, so a caller holding only a resource.Resource can ask the
// same question ZeroReadProblems answers, through one non-generic interface
// that reaches every kit surface without naming any. It resolves the top-level
// sub-map by S's type name (which, for the go-unifi types every kit resource
// binds, is also its constraint-table key) and hands the whole table on for the
// nested descent to resolve each block's element type against.
func (r *Resource[M, S]) OmitZeroProblems() []string {
	var probe S
	name := reflect.TypeOf(probe).Name()
	return OmitZeroProblems(r.Spec, ui.FieldConstraints[name], ui.FieldConstraints)
}
