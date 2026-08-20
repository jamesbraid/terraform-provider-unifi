package resourcekit

import (
	"context"
	"reflect"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ObjectSetField carries a SET of nested objects: a types.Set on the model, a
// []E on the SDK.
//
// IT EXISTS BECAUSE A SetNestedBlock CANNOT USE ObjectListField, and the
// difference is load-bearing rather than cosmetic. unifi_device's port_override
// is declared set_nested_block, so its model attribute is types.Set and
// ObjectListField's Model accessor -- func(*M) *types.List -- does not compile
// against it. The kit had eleven field kinds and exactly one Set-typed model
// accessor, StringSetField's, so a set of OBJECTS had no kind at all.
//
// ORDER-INSENSITIVITY IS THE POINT OF THE SET AND IT MAKES ONE THING SIMPLER.
// ObjectListField.CopyPlanToState replaces wholesale rather than merging per
// element, and its comment gives the reason: list elements have no identity, so
// nothing says the plan's second element is the state's second rather than a
// new one inserted above it, and a positional merge would graft a computed
// value onto the wrong element. A set has no positions at all, so the same
// wholesale behaviour is not a compromise here -- it is the only thing the type
// can mean.
//
// DUPLICATES ARE THE ONE REAL DIFFERENCE AND THE FRAMEWORK DOES NOT RESOLVE
// THEM. types.SetValue neither deduplicates nor rejects duplicate elements; it
// checks element types and nothing else. Terraform's own set semantics collapse
// them at the protocol layer, so a controller returning two identical elements
// yields a set of one and a permanent diff against a configuration that names
// both. That is a property of the surface's data, not something this kind can
// fix, and it is recorded here so the next reader does not go looking for a
// deduplication bug in the kit.
type ObjectSetField[M any, S any, E any] struct {
	Wire  string
	Model func(*M) *types.Set
	SDK   func(*S) *[]E

	// AttrTypes types ONE element, not the set.
	AttrTypes map[string]attr.Type

	Encode func(ctx context.Context, object types.Object) (E, diag.Diagnostics)
	Decode func(ctx context.Context, element E) (types.Object, diag.Diagnostics)

	// Unmodelled enumerates wire names of ELEMENT members knowingly left to the
	// controller. See ObjectField.Unmodelled.
	Unmodelled []string

	Elide ElideZero
}

// The interface guarantee lives here rather than in a test, because it is a
// COMPILE-TIME fact and a test asserting it can never fail: if the type did not
// satisfy Field the test file would not build, so the test would not run rather
// than go red. internal/testaudit caught exactly that and was right to.
var _ Field[struct{}, struct{}] = ObjectSetField[struct{}, struct{}, struct{}]{}

func (f ObjectSetField[M, S, E]) WireName() string { return f.Wire }

func (f ObjectSetField[M, S, E]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	var diags diag.Diagnostics
	value := *f.Model(model)
	if value.IsNull() || value.IsUnknown() {
		*f.SDK(sdk) = nil
		return diags
	}
	elements := value.Elements()
	// ALLOCATED EVEN WHEN EMPTY, for the reason ObjectListField.ToSDK gives: a
	// nil slice marshals to null and an empty one to [], and the two mean
	// different things to a controller where the field carries no omitempty.
	out := make([]E, 0, len(elements))
	for _, element := range elements {
		object, ok := element.(types.Object)
		if !ok {
			diags.AddError("Converting "+f.Wire,
				"a set element is not an object, so the descriptor's Encode cannot read it")
			continue
		}
		encoded, d := f.Encode(ctx, object)
		diags.Append(d...)
		out = append(out, encoded)
	}
	*f.SDK(sdk) = out
	return diags
}

func (f ObjectSetField[M, S, E]) ToModel(ctx context.Context, sdk *S, model *M) diag.Diagnostics {
	var diags diag.Diagnostics
	objectType := types.ObjectType{AttrTypes: f.AttrTypes}
	elements := *f.SDK(sdk)
	if len(elements) == 0 && bool(f.Elide) {
		*f.Model(model) = types.SetNull(objectType)
		return diags
	}
	values := make([]attr.Value, 0, len(elements))
	for _, element := range elements {
		object, d := f.Decode(ctx, element)
		diags.Append(d...)
		values = append(values, object)
	}
	set, d := types.SetValue(objectType, values)
	diags.Append(d...)
	*f.Model(model) = set
	return diags
}

func (f ObjectSetField[M, S, E]) SetInPlan(plan *M) bool {
	value := *f.Model(plan)
	return !value.IsNull() && !value.IsUnknown()
}

// CopyPlanToState replaces the set wholesale. See the type comment: a set has
// no element identity and no positions, so there is nothing to merge against.
func (f ObjectSetField[M, S, E]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}

func (f ObjectSetField[M, S, E]) nestedProblems() []string {
	var element E
	return nestedTypeProblems(f.Wire, reflect.TypeOf(element), f.AttrTypes, f.Unmodelled)
}
