package resourcekit

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ConditionalWireProblems checks a scattered field's ConditionalWires against
// what Encode actually does, in both directions: a wire Encode writes but the
// predicate denies is dropped from the mask (apply sends nothing), and a wire
// Encode skips but the predicate affirms stays masked (go-unifi sends its
// zero, blanking the controller's value).
//
// Whether Encode wrote a wire is decided by running it twice -- once onto a
// zero struct, once onto one filled with a sentinel -- and comparing: a wire
// Encode overwrites ends up equal in both runs, one it leaves alone stays at
// zero in one and sentinel in the other.
//
// The objects are the caller's to supply; one that never falsifies a
// predicate would make this vacuous, so the check reports which wires no
// object exercised in each direction rather than passing quietly.
//
// A seed is required for a discriminated type (a zero unifi.Network can't
// marshal at all, and the sentinel pass would otherwise clobber the
// discriminator too); nil is fine for a plain struct.
func ConditionalWireProblems[M any, S any](
	field ScatteredObjectField[M, S],
	objects []types.Object,
	seed func(*S),
) []string {
	// The population walked here is field.Wires, not ConditionalWires' keys,
	// so a wire nobody declared conditional is still checked rather than
	// silently approved.
	ctx := context.Background()
	var problems []string
	sawTrue, sawFalse := map[string]bool{}, map[string]bool{}
	sawWritten, sawSkipped := map[string]bool{}, map[string]bool{}

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
		for _, wire := range field.Wires {
			if written[wire] {
				sawWritten[wire] = true
			} else {
				sawSkipped[wire] = true
			}
			test, declared := field.ConditionalWires[wire]
			if !declared {
				continue
			}
			predicate := test(object)
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

	// A ReadOnlyWires entry that Encode actually writes is the opposite,
	// silent failure: the name never reaches the mask, so the practitioner
	// sets a value and the apply sends nothing. A name that's not one of
	// Wires is a typo that protects nothing.
	declared := make(map[string]bool, len(field.Wires))
	for _, wire := range field.Wires {
		declared[wire] = true
	}
	for _, wire := range field.ReadOnlyWires {
		if !declared[wire] {
			problems = append(problems, fmt.Sprintf(
				"%q is in ReadOnlyWires and is not one of Wires, so it names nothing and "+
					"keeps nothing off the mask", wire))
			continue
		}
		// Encode must not write a wire declared read-only either: that's a
		// value the practitioner set with no mask entry to carry it -- the
		// silent drop, pointing the opposite way from the destruction above.
		if sawWritten[wire] {
			problems = append(problems, fmt.Sprintf(
				"%q is in ReadOnlyWires and Encode writes it, so the value is built and the "+
					"mask never carries it -- the practitioner sets an attribute and the "+
					"apply sends nothing", wire))
		}
	}

	// An undeclared wire the objects show is conditional is the destructive
	// case: Encode wrote it for some objects and skipped it for others, so
	// with no entry it stays on the mask regardless of what the plan says.
	for _, wire := range field.Wires {
		if _, declared := field.ConditionalWires[wire]; declared {
			continue
		}
		if sawWritten[wire] && sawSkipped[wire] {
			problems = append(problems, fmt.Sprintf(
				"Encode writes %q for some of these objects and leaves it alone for others, "+
					"and it is not in ConditionalWires -- so the mask carries it even when "+
					"nothing wrote it and go-unifi sends its zero over whatever the "+
					"controller holds", wire))
		}
	}

	// A predicate no object falsified is one this run couldn't catch lying
	// in the destructive direction.
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

// WiresEncodeWrites reports which of a scattered field's wires Encode assigns
// for one object, using the two-probe method the checks above rest on.
// Exported so there is one instrument rather than two: a surface test with
// its own comparison could inherit a different conflation (one once compared
// marshalled keys, which read an assigned-nil pointer as untouched).
func WiresEncodeWrites[M any, S any](
	ctx context.Context,
	field ScatteredObjectField[M, S],
	object types.Object,
	seed func(*S),
) (map[string]bool, error) {
	return wiresEncodeWrites(ctx, field, object, seed)
}

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
	// back before either object is used.
	if seed != nil {
		seed(&zero)
		seed(&sentinel)
	}

	before, err := structFieldsByWire(&zero)
	if err != nil {
		return nil, err
	}
	after, err := structFieldsByWire(&sentinel)
	if err != nil {
		return nil, err
	}
	// This control runs before Encode: the method rests on the two objects
	// differing at every wire, so a wire fillSentinel doesn't reach, or one
	// the seed sets on both, is reported rather than silently answered wrong.
	for _, wire := range field.Wires {
		zeroField, known := before[wire]
		if !known {
			return nil, fmt.Errorf(
				"%q is not a json field of the SDK type, so nothing here can say whether "+
					"Encode writes it", wire)
		}
		if reflect.DeepEqual(zeroField.Interface(), after[wire].Interface()) {
			return nil, fmt.Errorf(
				"the two probe objects hold the same value for %q before Encode runs, so a "+
					"write and a skip are indistinguishable for it", wire)
		}
	}

	if diags := field.Encode(ctx, object, &zero); diags.HasError() {
		return nil, fmt.Errorf("encoding onto a zero struct: %v", diags)
	}
	if diags := field.Encode(ctx, object, &sentinel); diags.HasError() {
		return nil, fmt.Errorf("encoding onto a sentinel struct: %v", diags)
	}

	written := make(map[string]bool, len(field.Wires))
	for _, wire := range field.Wires {
		written[wire] = reflect.DeepEqual(before[wire].Interface(), after[wire].Interface())
	}
	return written, nil
}

// WiresAtZero reports which of a scattered field's wires hold their type's
// zero after Encode has run for this object. It answers a different question
// from WiresEncodeWrites, easily confused with it: that asks whether Encode
// assigned the field at all, this asks what it assigned --
// `sdk.Field = model.X.ValueStringPointer()` always assigns, including nil
// when X is null, so it's written, but written as nothing.
//
// The difference decides whether ConditionalWires can help: a wire Encode
// sometimes skips is conditional and declarable, but one it always assigns
// and sometimes assigns as zero has nothing to key a predicate on -- the fix
// there is to stop assigning in the mapper, not to declare anything here.
//
// A narrowing that asks "did this object emit the name" drops such a wire,
// since a zero behind omitempty is absent from the encoding; one that asks
// "would a populated object emit it" keeps it, trading a possible
// cannot-clear for a possible clobber -- which is why the population must be
// known before choosing.
func WiresAtZero[M any, S any](
	ctx context.Context,
	field ScatteredObjectField[M, S],
	object types.Object,
	seed func(*S),
) (map[string]bool, error) {
	var probe S
	if seed != nil {
		seed(&probe)
	}
	if diags := field.Encode(ctx, object, &probe); diags.HasError() {
		return nil, fmt.Errorf("encoding onto the probe: %v", diags)
	}
	fields, err := structFieldsByWire(&probe)
	if err != nil {
		return nil, err
	}
	atZero := make(map[string]bool, len(field.Wires))
	for _, wire := range field.Wires {
		value, known := fields[wire]
		if !known {
			return nil, fmt.Errorf("%q is not a json field of the SDK type", wire)
		}
		atZero[wire] = value.IsZero()
	}
	return atZero, nil
}

// structFieldsByWire indexes a struct's fields by their json name. It reads
// the struct, not the encoding: an encoded form can't represent "this field
// was not written" for a wire an alias never emits (vpn_server's
// wireguard_public_key is one), where absent-from-both and equal-in-both
// would otherwise look the same.
func structFieldsByWire(v any) (map[string]reflect.Value, error) {
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Pointer || value.Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("indexing needs a pointer to a struct, got %T", v)
	}
	elem := value.Elem()
	structType := elem.Type()
	out := make(map[string]reflect.Value, structType.NumField())
	for i := range structType.NumField() {
		tag := structType.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}
		out[name] = elem.Field(i)
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
		default:
			if err := fillSentinelValue(field); err != nil {
				continue
			}
		}
	}
	return nil
}

// fillSentinelValue puts a value in v that a zero one cannot equal. A kind
// with no case here reads as written for every wire of that type, silently:
// fillSentinel's default branch swallows this function's error and leaves
// the field at zero, so an Encode that never touches it looks identical to
// one that did. Add a case for any new kind rather than letting it fall
// through; for a slice or map, only the container needs to differ from nil.
func fillSentinelValue(v reflect.Value) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString("resourcekit-sentinel")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(9973)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(9973)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(9973)
	case reflect.Slice:
		one := reflect.MakeSlice(v.Type(), 1, 1)
		_ = fillSentinelValue(one.Index(0))
		v.Set(one)
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		if err := fillSentinelValue(key); err != nil {
			return err
		}
		val := reflect.New(v.Type().Elem()).Elem()
		_ = fillSentinelValue(val)
		m.SetMapIndex(key, val)
		v.Set(m)
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
