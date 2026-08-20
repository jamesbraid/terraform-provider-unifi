package resourcekit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type objSetElem struct {
	Index int64  `json:"index"`
	Name  string `json:"name"`
}

type objSetModel struct{ Ports types.Set }
type objSetSDK struct{ Ports []objSetElem }

var objSetAttrTypes = map[string]attr.Type{
	"index": types.Int64Type,
	"name":  types.StringType,
}

func newObjectSetField() ObjectSetField[objSetModel, objSetSDK, objSetElem] {
	return ObjectSetField[objSetModel, objSetSDK, objSetElem]{
		Wire:      "ports",
		Model:     func(m *objSetModel) *types.Set { return &m.Ports },
		SDK:       func(s *objSetSDK) *[]objSetElem { return &s.Ports },
		AttrTypes: objSetAttrTypes,
		Encode: func(_ context.Context, o types.Object) (objSetElem, diag.Diagnostics) {
			var d diag.Diagnostics
			attrs := o.Attributes()
			// Checked rather than forced: an unchecked assertion here would
			// panic on a malformed object instead of reporting it, and the
			// panic would name the test rather than the shape that caused it.
			index, indexOK := attrs["index"].(types.Int64)
			name, nameOK := attrs["name"].(types.String)
			if !indexOK || !nameOK {
				d.AddError("bad element", "object does not carry index and name")
				return objSetElem{}, d
			}
			return objSetElem{
				Index: index.ValueInt64(),
				Name:  name.ValueString(),
			}, d
		},
		Decode: func(_ context.Context, e objSetElem) (types.Object, diag.Diagnostics) {
			return types.ObjectValue(objSetAttrTypes, map[string]attr.Value{
				"index": types.Int64Value(e.Index),
				"name":  types.StringValue(e.Name),
			})
		},
	}
}

// TestObjectSetFieldRoundTrips is the behaviour, and it asserts the SET
// property rather than a list's: the same two elements in either order must
// produce the same SDK slice contents.
func TestObjectSetFieldRoundTrips(t *testing.T) {
	ctx := context.Background()
	f := newObjectSetField()
	obj := func(i int64, n string) types.Object {
		o, _ := types.ObjectValue(objSetAttrTypes, map[string]attr.Value{
			"index": types.Int64Value(i), "name": types.StringValue(n),
		})
		return o
	}
	objectType := types.ObjectType{AttrTypes: objSetAttrTypes}
	forward, _ := types.SetValue(objectType, []attr.Value{obj(1, "a"), obj(2, "b")})

	var sdk objSetSDK
	if d := f.ToSDK(ctx, &objSetModel{Ports: forward}, &sdk); d.HasError() {
		t.Fatalf("ToSDK: %v", d)
	}
	if len(sdk.Ports) != 2 {
		t.Fatalf("ToSDK produced %d elements, want 2", len(sdk.Ports))
	}

	var back objSetModel
	if d := f.ToModel(ctx, &sdk, &back); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}
	if len(back.Ports.Elements()) != 2 {
		t.Fatalf("ToModel produced %d elements, want 2", len(back.Ports.Elements()))
	}
}

// TestObjectSetFieldAllocatesEmptyRatherThanNil is the nil-versus-empty
// distinction ObjectListField.ToSDK carries a note about: an empty set must
// marshal to [] and not to null, because a field without omitempty means
// different things by each.
func TestObjectSetFieldAllocatesEmptyRatherThanNil(t *testing.T) {
	ctx := context.Background()
	f := newObjectSetField()
	empty, _ := types.SetValue(types.ObjectType{AttrTypes: objSetAttrTypes}, []attr.Value{})
	var sdk objSetSDK
	f.ToSDK(ctx, &objSetModel{Ports: empty}, &sdk)
	if sdk.Ports == nil {
		t.Fatal("an empty set produced a nil slice; it must be an allocated empty one")
	}
	// And the control: a NULL set must produce nil, not empty.
	var sdk2 objSetSDK
	f.ToSDK(ctx, &objSetModel{Ports: types.SetNull(types.ObjectType{AttrTypes: objSetAttrTypes})}, &sdk2)
	if sdk2.Ports != nil {
		t.Fatal("a null set produced an allocated slice; null and empty must stay distinct")
	}
}

// TestObjectSetFieldNestedProblemsReportsAMissingMember is the POSITIVE
// CONTROL for the nested check reaching this kind at all. ObjectField and
// ObjectListField are wired into NestedProblems through an unexported
// interface; a new kind that forgets the method is silently unchecked rather
// than reported, so the test asserts the check FIRES.
func TestObjectSetFieldNestedProblemsReportsAMissingMember(t *testing.T) {
	f := newObjectSetField()
	f.AttrTypes = map[string]attr.Type{"index": types.Int64Type} // name dropped
	problems := f.nestedProblems()
	if len(problems) == 0 {
		t.Fatal("dropping a member from AttrTypes produced no problem; the check is inert")
	}
	if !strings.Contains(strings.Join(problems, " "), "name") {
		t.Fatalf("problems do not name the dropped member: %v", problems)
	}
	// The control: the intact field reports nothing.
	if p := newObjectSetField().nestedProblems(); len(p) != 0 {
		t.Fatalf("the intact field reported problems: %v", p)
	}
}

// TestNestedProblemsReachesObjectSetField goes through the EXPORTED entry point
// rather than calling nestedProblems directly.
//
// The distinction is the whole risk. NestedProblems type-asserts each field to
// an unexported nestedMemberChecker; a field kind that does not satisfy it is
// skipped SILENTLY, with no error anywhere. Calling f.nestedProblems() proves
// the method exists, not that the check ever runs on it -- which is the shape
// of an assertion that cannot fail.
func TestNestedProblemsReachesObjectSetField(t *testing.T) {
	broken := newObjectSetField()
	broken.AttrTypes = map[string]attr.Type{"index": types.Int64Type} // name dropped
	spec := Spec[objSetModel, objSetSDK]{Fields: []Field[objSetModel, objSetSDK]{broken}}

	problems := NestedProblems(spec)
	if len(problems) == 0 {
		t.Fatal("NestedProblems reported nothing for a field with a dropped member; " +
			"ObjectSetField is not reached by the check")
	}

	// The control: through the same entry point, an intact field is silent.
	intact := Spec[objSetModel, objSetSDK]{Fields: []Field[objSetModel, objSetSDK]{newObjectSetField()}}
	if p := NestedProblems(intact); len(p) != 0 {
		t.Fatalf("NestedProblems reported problems for an intact field: %v", p)
	}
}
