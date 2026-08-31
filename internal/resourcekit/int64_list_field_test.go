package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

type int64ListModel struct {
	Channels types.List
}

type int64ListSDK struct {
	Channels []int64
}

func channelsField() Int64ListField[int64ListModel, int64ListSDK] {
	return Int64ListField[int64ListModel, int64ListSDK]{
		Wire:  "channels",
		Model: func(m *int64ListModel) *types.List { return &m.Channels },
		SDK:   func(s *int64ListSDK) *[]int64 { return &s.Channels },
	}
}

// TestInt64ListFieldRoundTrips is this kind's own positive control: radio_ai
// is the first consumer of Int64ListField, so this proves the mechanism
// (ToSDK then ToModel reproduces the original value) before anything else
// depends on it, the same shape TestStringSetFieldRoundTrips gives
// StringSetField.
func TestInt64ListFieldRoundTrips(t *testing.T) {
	ctx := context.Background()
	list, diags := types.ListValueFrom(ctx, types.Int64Type, []int64{36, 40, 44})
	if diags.HasError() {
		t.Fatal(diags)
	}
	model := int64ListModel{Channels: list}

	var sdk int64ListSDK
	if d := channelsField().ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if len(sdk.Channels) != 3 {
		t.Fatalf("ToSDK wrote %d channel(s), want 3: %v", len(sdk.Channels), sdk.Channels)
	}

	var back int64ListModel
	if d := channelsField().ToModel(ctx, &sdk, &back); d.HasError() {
		t.Fatal(d)
	}
	if !back.Channels.Equal(list) {
		t.Errorf("round trip changed the list: %v -> %v", list, back.Channels)
	}
}

// TestAnAbsentInt64ListEmptiesTheSDKSlice matches
// TestAnAbsentSetEmptiesTheSDKSlice's own reasoning: a null list must clear
// whatever stale value the SDK struct held, and must clear it to an
// allocated empty slice ([]int64{}), not nil -- nil and empty serialise
// differently and the controller reads them as different requests.
func TestAnAbsentInt64ListEmptiesTheSDKSlice(t *testing.T) {
	ctx := context.Background()
	model := int64ListModel{Channels: types.ListNull(types.Int64Type)}
	sdk := int64ListSDK{Channels: []int64{100}}

	if d := channelsField().ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.Channels == nil {
		t.Error("the SDK slice is nil; absent and present-and-empty serialise differently")
	}
	if len(sdk.Channels) != 0 {
		t.Errorf("a null list left %v on the SDK struct", sdk.Channels)
	}
}

// TestInt64ListFieldElideNullsAnEmptyRead proves the Elide=NullZero branch a
// KeepZero-only round trip test can't reach: an empty SDK slice reads back
// as a null list, not an empty one, when the field opts in.
func TestInt64ListFieldElideNullsAnEmptyRead(t *testing.T) {
	ctx := context.Background()
	field := Int64ListField[int64ListModel, int64ListSDK]{
		Wire:  "channels",
		Model: func(m *int64ListModel) *types.List { return &m.Channels },
		SDK:   func(s *int64ListSDK) *[]int64 { return &s.Channels },
		Elide: NullZero,
	}
	sdk := int64ListSDK{Channels: nil}

	var model int64ListModel
	if d := field.ToModel(ctx, &sdk, &model); d.HasError() {
		t.Fatal(d)
	}
	if !model.Channels.IsNull() {
		t.Errorf("Channels = %v, want null", model.Channels)
	}
}
