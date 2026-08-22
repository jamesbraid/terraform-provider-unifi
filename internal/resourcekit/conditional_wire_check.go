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
	// NO EARLY RETURN FOR AN EMPTY DECLARATION, and removing it is the whole
	// point of this revision.
	//
	// The check used to return nil when a field declared no ConditionalWires,
	// and to iterate the DECLARATION when it did. Both take the population from
	// the thing being checked, so the only case that destroys anything -- a
	// conditional wire NOBODY DECLARED -- was the one case it could not see.
	// network's dhcp_server and dhcp_v6_server declare none between them and
	// would have been approved in silence over seventeen wires that end at
	// their zero.
	//
	// The population is field.Wires now. Deleting an entry from
	// ConditionalWires fails, because the wire is still checked and still turns
	// out to be conditional.
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

	// A ReadOnlyWires ENTRY THE Encode ACTUALLY WRITES IS THE OPPOSITE FAILURE
	// and it is silent: the name never reaches the mask, so the practitioner
	// sets a value and the apply sends nothing. Declared read-only means the
	// encoder cannot carry it, and that is checkable here because the objects
	// are already in hand.
	//
	// A NAME THAT IS NOT ONE OF Wires IS A TYPO THAT DISABLES NOTHING AND
	// PROTECTS NOTHING, the same hazard ConditionalWires has.
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
		// AND Encode MUST NOT WRITE IT. A wire declared read-only that Encode
		// assigns is a value the practitioner set and the mask never carries --
		// the silent drop, pointing the other way from the destruction above.
		//
		// THIS ASSERTION WAS WITHDRAWN ONCE AND THE INSTRUMENT WAS THE REASON.
		// wiresEncodeWrites compared the two runs' ENCODINGS, and a read-only
		// wire is one the encoder never emits, so it was absent from both --
		// equal, and therefore "written" by that rule. It fired on vpn_server's
		// wireguard_public_key, whose Encode does not mention it. Comparing the
		// STRUCT FIELDS instead answers the question that was actually being
		// asked, and the control in wiresEncodeWrites refuses a wire the two
		// probes cannot distinguish rather than guessing for it.
		if sawWritten[wire] {
			problems = append(problems, fmt.Sprintf(
				"%q is in ReadOnlyWires and Encode writes it, so the value is built and the "+
					"mask never carries it -- the practitioner sets an attribute and the "+
					"apply sends nothing", wire))
		}
	}

	// AN UNDECLARED WIRE THE OBJECTS SHOW IS CONDITIONAL IS THE DESTRUCTIVE
	// CASE, and it is reported first because it is the one nothing else looks
	// for. Encode wrote it for some objects and left it alone for others, so it
	// is conditional by the same definition the declared ones are judged by --
	// and with no entry it stays on the mask whatever the plan says.
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
	// THE CONTROL, AND IT RUNS BEFORE Encode DOES. The whole method rests on the
	// two objects differing at every wire: where they already agree, "Encode
	// overwrote both" and "Encode touched neither" produce the same answer and
	// this cannot tell them apart. A field kind fillSentinel does not reach, or
	// one the seed sets on both, lands there -- so it is reported rather than
	// answered.
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

// structFieldsByWire indexes a struct's fields by their json name.
//
// IT READS THE STRUCT AND NOT THE ENCODING, and that is the whole of #240. The
// encoded form cannot represent "this field was not written": a wire the
// purpose alias never emits is absent from both runs, absent equals absent, and
// the rule that agreement means a write calls it written. vpn_server's
// wireguard_public_key is exactly that -- the controller issues it, marshalUserVPN
// does not emit it, and Encode does not mention it. Absent-from-both and
// equal-in-both are the same string and different facts.
//
// The struct always has the field, whatever the alias does with it, so the
// question "did Encode change this" has an answer there and nowhere else.
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

// fillSentinelValue puts a value in v that a zero one cannot equal.
//
// A KIND WITH NO CASE HERE READS AS WRITTEN FOR EVERY WIRE OF THAT TYPE, and
// silently. wiresEncodeWrites calls a wire written when the zero run and the
// sentinel run agree, so a field the sentinel pass leaves at its zero agrees
// with itself and every Encode that never touches it looks like an Encode that
// did. Slices had no case, and network's dhcp_relay_servers is where that
// surfaced -- vpn_client's seven conditional wires are strings and pointers.
//
// THE ERROR RUNS IN THE DANGEROUS DIRECTION, which is why a missing kind is a
// defect rather than a gap. The check tells the author Encode writes the wire
// and their predicate denies it; an author who believes it declares the
// predicate always true, the name is masked when nothing wrote it, and a
// would-emit narrowing sends the zero over whatever the controller holds. A
// check that said nothing would have been safer than one arguing for that.
//
// So add the kind rather than letting it fall through, and note that for a
// slice or a map the ELEMENT's sentinel is optional: a one-element container
// already differs from nil, which is all the comparison needs.
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
