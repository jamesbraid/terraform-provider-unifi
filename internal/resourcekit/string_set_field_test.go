package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type setModel struct {
	Members types.Set
	Ordered types.List
}

type setSDK struct {
	Members []string
	Ordered []string
}

func memberField() StringSetField[setModel, setSDK] {
	return StringSetField[setModel, setSDK]{
		Wire:  "group_members",
		Model: func(m *setModel) *types.Set { return &m.Members },
		SDK:   func(s *setSDK) *[]string { return &s.Members },
	}
}

func orderedField() StringListField[setModel, setSDK] {
	return StringListField[setModel, setSDK]{
		Wire:  "ordered",
		Model: func(m *setModel) *types.List { return &m.Ordered },
		SDK:   func(s *setSDK) *[]string { return &s.Ordered },
	}
}

func TestStringSetFieldRoundTrips(t *testing.T) {
	ctx := context.Background()
	set, diags := types.SetValueFrom(ctx, types.StringType, []string{"a", "b", "c"})
	if diags.HasError() {
		t.Fatal(diags)
	}
	model := setModel{Members: set}

	var sdk setSDK
	if d := memberField().ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if len(sdk.Members) != 3 {
		t.Fatalf("ToSDK wrote %d member(s), want 3: %v", len(sdk.Members), sdk.Members)
	}

	var back setModel
	if d := memberField().ToModel(ctx, &sdk, &back); d.HasError() {
		t.Fatal(d)
	}
	if !back.Members.Equal(set) {
		t.Errorf("round trip changed the set: %v -> %v", set, back.Members)
	}
}

// TestAReorderedSetIsEqualAndAReorderedListIsNot is the property the type
// exists for, and the list beside it is the control.
//
// A set with the same members in a different order is the SAME set, so a
// controller returning group members in its own sequence produces no diff. The
// identical data through StringListField is a DIFFERENT list, which would be a
// permanent change the practitioner cannot suppress. If this test ever shows
// both equal, or both unequal, the two field kinds have stopped differing and
// one of them is pointless.
func TestAReorderedSetIsEqualAndAReorderedListIsNot(t *testing.T) {
	ctx := context.Background()

	original, _ := types.SetValueFrom(ctx, types.StringType, []string{"a", "b", "c"})
	originalList, _ := types.ListValueFrom(ctx, types.StringType, []string{"a", "b", "c"})
	model := setModel{Members: original, Ordered: originalList}

	// The controller answers with the same members, reordered.
	reordered := setSDK{Members: []string{"c", "a", "b"}, Ordered: []string{"c", "a", "b"}}

	var back setModel
	if d := memberField().ToModel(ctx, &reordered, &back); d.HasError() {
		t.Fatal(d)
	}
	if d := orderedField().ToModel(ctx, &reordered, &back); d.HasError() {
		t.Fatal(d)
	}

	if !back.Members.Equal(model.Members) {
		t.Errorf("a reordered SET should be equal, got %v want %v", back.Members, model.Members)
	}
	if back.Ordered.Equal(model.Ordered) {
		t.Error("a reordered LIST compared equal, so this control proves nothing " +
			"and the set case above is not evidence of anything either")
	}
}

// TestAnAbsentSetEmptiesTheSDKSlice pins the nil-versus-empty distinction the
// list field documents: absent and present-and-empty are different requests.
func TestAnAbsentSetEmptiesTheSDKSlice(t *testing.T) {
	ctx := context.Background()
	model := setModel{Members: types.SetNull(types.StringType)}
	sdk := setSDK{Members: []string{"stale"}}

	if d := memberField().ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.Members == nil {
		t.Error("the SDK slice is nil; absent and present-and-empty serialise differently")
	}
	if len(sdk.Members) != 0 {
		t.Errorf("a null set left %v on the SDK struct", sdk.Members)
	}
}
