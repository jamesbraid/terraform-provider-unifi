package resourcekit

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ObjectField carries a nested object: a types.Object on the model, a *E on the
// SDK struct.
//
// IT DOES NOT MAP THE MEMBERS ITSELF. The descriptor supplies Encode and Decode,
// because a nested object's members need the same per-field decisions the
// top-level fields get -- elide rules, pointer-versus-value, derived values --
// and a kind that guessed them would be a second, weaker copy of the field list.
// What the kind adds is the thing no descriptor can check for itself: whether
// the nested SDK type carries members the schema does not declare and the wire
// cannot omit.
//
// WHY THAT CHECK HAS TO LIVE HERE. A field mask names TOP-LEVEL keys. `source`
// is one key, so masking it sends the whole nested object -- there is no way to
// express "send source.zone_id but not source.match_mac". The fix that closed
// #184 for top-level fields is structurally unable to reach one level down, and
// every nested capability inherits that. So a nested type with force-emitted
// members the model does not carry sends them as Go zeros on every apply, and
// the descriptor author cannot see it from the schema.
//
// TYPED OBJECTS WERE CONSIDERED AND CANNOT WORK IN THIS PIPELINE, which is
// worth recording because the door looks open: the generated tree declares an
// <X>Value for every nested object, they implement basetypes.ObjectValuable,
// and a constraint on that interface would carry them.
//
// It would not survive generation. cmd/nested-custom-type-strip removes the
// CustomType binding from every nested attribute after tfplugingen-framework
// emits it -- 52 across the estate, none of them backed -- because every nested
// emitter in the framework calls NewCustomNestedObjectType unconditionally and
// no flag controls it. So no served schema ever binds <X>Type, and a descriptor
// carrying <X>Value would fail TestServedSchemaAgreesWithItsRuntimeModel's
// identity check from the other side: a model declares the value type and
// nothing asks for it.
//
// A descriptor author trying it would get an error from a check in another
// package with no way to connect the two, which is why the constraint is
// types.Object and this comment exists.
type ObjectField[M any, S any, E any] struct {
	Wire  string
	Model func(*M) *types.Object
	SDK   func(*S) **E

	// AttrTypes types the object in state. It must match the schema's nested
	// object exactly or the value does not fit.
	AttrTypes map[string]attr.Type

	// Encode builds the SDK object from the model's object value. Returning nil
	// means "the practitioner did not configure this", and the SDK pointer is
	// left nil so an omitempty key drops out.
	Encode func(ctx context.Context, object types.Object) (*E, diag.Diagnostics)
	// Decode builds the model's object value from what the controller returned.
	Decode func(ctx context.Context, sdk *E) (types.Object, diag.Diagnostics)

	// Unmodelled enumerates the wire names of members this descriptor knowingly
	// leaves to the controller.
	//
	// IT IS A LIST RATHER THAN A FLAG ON PURPOSE. A blanket "preserve whatever I
	// do not model" absorbs a member added by a later SDK regeneration in
	// silence; an enumeration fails on it, because the new member is not in the
	// list. Same shape as the Device rescue guard, which pins {adopted,
	// port_overrides, state} so a fourth field names itself.
	//
	// Naming a member here is a claim that sending its zero is harmless. It is
	// not a way to make the check quiet.
	Unmodelled []string

	Elide ElideZero
}

func (f ObjectField[M, S, E]) WireName() string { return f.Wire }

func (f ObjectField[M, S, E]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	value := *f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		*f.SDK(sdk) = nil
		return nil
	}
	encoded, diags := f.Encode(ctx, value)
	*f.SDK(sdk) = encoded
	return diags
}

func (f ObjectField[M, S, E]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	nested := *f.SDK(sdk)
	if nested == nil {
		// An absent nested object is a null object either way, so Elide has
		// nothing to choose between here -- unlike a collection, where null and
		// empty are different values. It stays on the struct because
		// ElideProblems reflects on every field and reports one that carries no
		// Elide, and because a nested object CAN gain that distinction if a
		// controller starts returning an empty object rather than none.
		*f.Model(model) = types.ObjectNull(f.AttrTypes)
		return nil
	}
	object, diags := f.Decode(ctx, nested)
	*f.Model(model) = object
	return diags
}

func (f ObjectField[M, S, E]) SetInPlan(plan *M) bool {
	value := *f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

func (f ObjectField[M, S, E]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

// nestedMemberChecker is what NestedProblems needs from a field without knowing
// its element type.
type nestedMemberChecker interface {
	nestedProblems() []string
}

func (f ObjectField[M, S, E]) nestedProblems() []string {
	var element E
	return nestedTypeProblems(f.Wire, reflect.TypeOf(element), f.AttrTypes, f.Unmodelled)
}

// nestedTypeProblems reports every force-emitted member of a nested SDK type
// that the object's attribute types do not declare and the descriptor has not
// enumerated.
//
// A member with omitempty is fine: nil or zero drops out of the encoding and
// the controller keeps what it holds. A member WITHOUT omitempty is sent
// whatever happens, so if the model cannot carry a value for it, the value sent
// is the Go zero.
func nestedTypeProblems(
	wire string,
	element reflect.Type,
	declared map[string]attr.Type,
	exempt []string,
) []string {
	if element == nil || element.Kind() != reflect.Struct {
		return nil
	}
	var problems []string
	for i := range element.NumField() {
		field := element.Field(i)
		tag, ok := field.Tag.Lookup("json")
		if !ok || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		if name == "" || slices.Contains(parts[1:], "omitempty") {
			continue
		}
		if _, modelled := declared[name]; modelled {
			continue
		}
		if slices.Contains(exempt, name) {
			continue
		}
		problems = append(problems, wire+"."+name+" is emitted unconditionally by "+
			element.Name()+", and the object does not declare it. A field mask names "+
			"top-level keys, so masking "+wire+" sends the whole nested object and this "+
			"member goes as its Go zero on every apply. Declare it, or name it in the "+
			"field's Unmodelled list to record that sending its zero is harmless.")
	}
	return problems
}

// NestedProblems runs the check over a spec's fields. It is a descriptor-time
// check in the same idiom as ElideProblems and WireNameProblems: a test asks
// the question once, for the author, rather than the kit refusing at apply time
// where the practitioner cannot act on it.
func NestedProblems[M any, S any](spec Spec[M, S]) []string {
	var problems []string
	for _, field := range spec.Fields {
		inner := field
		if unwrapper, ok := any(field).(interface{ Unwrap() Field[M, S] }); ok {
			inner = unwrapper.Unwrap()
		}
		if checker, ok := any(inner).(nestedMemberChecker); ok {
			problems = append(problems, checker.nestedProblems()...)
		}
	}
	return problems
}
