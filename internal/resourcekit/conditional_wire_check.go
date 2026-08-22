package resourcekit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ConditionalWireProblems checks a scattered field's ConditionalWires against
// what Encode actually does, IN BOTH DIRECTIONS, for each object supplied.
//
// ConditionalWires is a second list that has to agree with a decision already
// made inside Encode, and this codebase's standing lesson about two lists that
// must agree is that nothing checks them until something does. The failure is
// not symmetric:
//
//   - a wire Encode writes but the predicate reports false is DROPPED from the
//     mask, so the practitioner sets a value and the apply sends nothing;
//   - a wire Encode skips but the predicate reports true is MASKED ANYWAY, and
//     go-unifi sends its zero, which blanks whatever the controller holds.
//
// The second is the destruction ConditionalWires exists to stop, so a check
// asserting only one direction is worse than none: a predicate that always
// returns false passes it while silently dropping every write.
//
// HOW "DID Encode WRITE IT" IS DECIDED WITHOUT GUESSING A VALUE. Encode runs
// twice -- once onto a zero SDK struct and once onto one whose every field
// carries a sentinel. A wire Encode writes ends up with the same value both
// times, because Encode overwrote whatever was there. A wire it leaves alone
// keeps the zero in one and the sentinel in the other. So agreement across the
// two runs IS the write, and no assumption about what Encode would produce
// enters the comparison.
//
// The objects are the surface's to supply, because only the descriptor knows
// which shapes make a predicate true and which make it false. Supplying one
// that never falsifies a predicate is the way to make this vacuous, so it
// reports the wires no object exercised in each direction rather than passing
// quietly.
// SEED IS NOT OPTIONAL FOR A DISCRIMINATED TYPE, and finding that out was the
// first thing this check did. A zero unifi.Network cannot marshal at all --
// "unknown network purpose" -- because its encoder dispatches on Purpose, and
// the sentinel pass would otherwise set that field to a sentinel string and
// break it a second way. go-unifi's maskedBody documents the same requirement
// for the same reason: where the encoding varies by a discriminator, the
// discriminator has to be set even when nothing names it. So the surface
// supplies it, and a nil seed is fine for every plain struct.
func ConditionalWireProblems[M any, S any](
	field ScatteredObjectField[M, S],
	objects []types.Object,
	seed func(*S),
) []string {
	if len(field.ConditionalWires) == 0 {
		return nil
	}
	ctx := context.Background()
	var problems []string
	sawTrue, sawFalse := map[string]bool{}, map[string]bool{}

	for index, object := range objects {
		if object.IsNull() || object.IsUnknown() {
			problems = append(problems, fmt.Sprintf(
				"object %d is null or unknown, which Encode never sees; it exercises nothing",
				index))
			continue
		}
		written, err := wiresEncodeWrites(ctx, field, object, seed)
		if err != nil {
			problems = append(problems, fmt.Sprintf("object %d: %v", index, err))
			continue
		}
		for _, wire := range sortedKeys(field.ConditionalWires) {
			predicate := field.ConditionalWires[wire](object)
			if predicate {
				sawTrue[wire] = true
			} else {
				sawFalse[wire] = true
			}
			switch {
			case predicate && !written[wire]:
				problems = append(problems, fmt.Sprintf(
					"object %d: the predicate for %q says it is written and Encode leaves it "+
						"alone, so the mask carries it and go-unifi sends its zero over "+
						"whatever the controller holds", index, wire))
			case !predicate && written[wire]:
				problems = append(problems, fmt.Sprintf(
					"object %d: Encode writes %q and the predicate says it does not, so the "+
						"name is dropped from the mask and the value is never sent",
					index, wire))
			}
		}
	}

	// A CHECK THAT ONLY EVER SAW ONE ANSWER IS HALF A CHECK. A predicate no
	// object falsified is one this run could not have caught lying in the
	// destructive direction.
	for _, wire := range sortedKeys(field.ConditionalWires) {
		switch {
		case !sawTrue[wire]:
			problems = append(problems, fmt.Sprintf(
				"no object makes the predicate for %q true, so the written direction is "+
					"unexercised", wire))
		case !sawFalse[wire]:
			problems = append(problems, fmt.Sprintf(
				"no object makes the predicate for %q false, so the direction that blanks "+
					"the controller is unexercised", wire))
		}
	}
	return problems
}

// wiresEncodeWrites reports, per wire name, whether Encode assigned it.
func wiresEncodeWrites[M any, S any](
	ctx context.Context,
	field ScatteredObjectField[M, S],
	object types.Object,
	seed func(*S),
) (map[string]bool, error) {
	var zero, sentinel S
	if err := fillSentinel(&sentinel); err != nil {
		return nil, err
	}
	// AFTER the sentinel fill, so a discriminator the sentinel clobbered is put
	// back before either object is marshalled.
	if seed != nil {
		seed(&zero)
		seed(&sentinel)
	}
	if diags := field.Encode(ctx, object, &zero); diags.HasError() {
		return nil, fmt.Errorf("encoding onto a zero struct: %v", diags)
	}
	if diags := field.Encode(ctx, object, &sentinel); diags.HasError() {
		return nil, fmt.Errorf("encoding onto a sentinel struct: %v", diags)
	}
	fromZero, err := encodedKeys(&zero)
	if err != nil {
		return nil, err
	}
	fromSentinel, err := encodedKeys(&sentinel)
	if err != nil {
		return nil, err
	}
	written := make(map[string]bool, len(field.Wires))
	for _, wire := range field.Wires {
		written[wire] = string(fromZero[wire]) == string(fromSentinel[wire])
	}
	return written, nil
}

func encodedKeys(v any) (map[string]json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

// fillSentinel gives every settable field a value distinguishable from its
// zero, so a field Encode does not touch reads differently from one it does.
func fillSentinel(v any) error {
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Pointer || value.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("sentinel needs a pointer to a struct, got %T", v)
	}
	elem := value.Elem()
	for i := range elem.NumField() {
		field := elem.Field(i)
		if !field.CanSet() {
			continue
		}
		switch field.Kind() {
		case reflect.String:
			field.SetString("resourcekit-sentinel")
		case reflect.Bool:
			field.SetBool(true)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			field.SetInt(9973)
		case reflect.Float32, reflect.Float64:
			field.SetFloat(9973)
		case reflect.Pointer:
			if field.Type().Elem().Kind() == reflect.Struct {
				continue
			}
			pointed := reflect.New(field.Type().Elem())
			if err := fillSentinelValue(pointed.Elem()); err == nil {
				field.Set(pointed)
			}
		}
	}
	return nil
}

func fillSentinelValue(v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString("resourcekit-sentinel")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(9973)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(9973)
	default:
		return fmt.Errorf("no sentinel for %s", v.Kind())
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
