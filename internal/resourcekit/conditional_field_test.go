package resourcekit

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// The two additions are tested apart, because they are two ideas.

type routeModel struct {
	Type      types.String
	Interface types.String
	NextHop   iptypes.IPAddress
}

type routeSDK struct {
	Type      string
	Interface string
	NextHop   string
}

func nextHopField() StringLikeField[routeModel, routeSDK, iptypes.IPAddress] {
	return StringLikeField[routeModel, routeSDK, iptypes.IPAddress]{
		Wire:  "static-route_nexthop",
		Model: func(m *routeModel) *iptypes.IPAddress { return &m.NextHop },
		SDK:   func(s *routeSDK) *string { return &s.NextHop },
		New:   func(v basetypes.StringValue) iptypes.IPAddress { return iptypes.IPAddress{StringValue: v} },
		Elide: NullZero,
		WriteWhen: func(m *routeModel) bool {
			return m.Type.ValueString() == "nexthop-route"
		},
	}
}

// IDEA ONE: the value-type parameter. Before it, a custom-typed attribute could
// not bind to any field kind at all.
func TestACustomTypedScalarRoundTrips(t *testing.T) {
	ctx := context.Background()
	f := nextHopField()
	model := routeModel{
		Type:    types.StringValue("nexthop-route"),
		NextHop: iptypes.NewIPAddressValue("192.0.2.1"),
	}
	var sdk routeSDK
	if d := f.ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.NextHop != "192.0.2.1" {
		t.Errorf("the custom-typed value did not reach the SDK struct: %q", sdk.NextHop)
	}
	var back routeModel
	if d := f.ToModel(ctx, &sdk, &back); d.HasError() {
		t.Fatal(d)
	}
	if back.NextHop.ValueString() != "192.0.2.1" {
		t.Errorf("round trip: %v", back.NextHop)
	}
	// And the model field is still the CUSTOM type, not a plain string --
	// which is the whole point, because the schema declares it that way and a
	// mismatch fails at plan time rather than here.
	if _, ok := any(back.NextHop).(iptypes.IPAddress); !ok {
		t.Error("the round trip lost the custom type")
	}
}

// TestAnElidedCustomTypeBecomesNullNotEmpty pins the reason New takes a
// StringValue rather than a string: NewIPAddressValue("") is an empty IP, not a
// null one, and the two are different states.
func TestAnElidedCustomTypeBecomesNullNotEmpty(t *testing.T) {
	ctx := context.Background()
	var model routeModel
	if d := nextHopField().ToModel(ctx, &routeSDK{NextHop: ""}, &model); d.HasError() {
		t.Fatal(d)
	}
	if !model.NextHop.IsNull() {
		t.Errorf("an empty wire value produced %v, want null", model.NextHop)
	}
}

// IDEA TWO: the write predicate. Independent of the type parameter -- the same
// suppression is needed on a plain types.String field.
func TestTheWritePredicateSuppressesTheWrite(t *testing.T) {
	ctx := context.Background()
	f := nextHopField()
	// A next_hop set on an INTERFACE route must not be sent.
	model := routeModel{
		Type:    types.StringValue("interface-route"),
		NextHop: iptypes.NewIPAddressValue("192.0.2.1"),
	}
	var sdk routeSDK
	if d := f.ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.NextHop != "" {
		t.Errorf("next_hop was sent on an interface-route: %q", sdk.NextHop)
	}

	// CONTROL: the same field with the matching type DOES write, or the
	// assertion above would pass for a field that never writes anything.
	model.Type = types.StringValue("nexthop-route")
	sdk = routeSDK{}
	if d := f.ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.NextHop != "192.0.2.1" {
		t.Fatal("the field never writes at all, so the suppression above proves nothing")
	}
}

// TestASuppressedFieldIsAlsoOutOfTheWireMask is the half that matters most.
//
// The mask is built from SetInPlan. A field suppressed on the write but still
// reporting SetInPlan would be NAMED on the wire while the SDK struct held
// whatever it held -- the controller would be told to set it to an empty value
// rather than left alone. Suppressing one without the other is worse than
// suppressing neither.
func TestASuppressedFieldIsAlsoOutOfTheWireMask(t *testing.T) {
	f := nextHopField()
	set := routeModel{
		Type:    types.StringValue("interface-route"),
		NextHop: iptypes.NewIPAddressValue("192.0.2.1"),
	}
	if f.SetInPlan(&set) {
		t.Error("a suppressed field reported SetInPlan, so it would be named in the wire mask")
	}
	set.Type = types.StringValue("nexthop-route")
	if !f.SetInPlan(&set) {
		t.Fatal("the field never reports SetInPlan, so the assertion above proves nothing")
	}
}

// TestThePredicateWorksOnAPlainStringField shows the two ideas are separable:
// this one needs no type parameter at all.
func TestThePredicateWorksOnAPlainStringField(t *testing.T) {
	ctx := context.Background()
	f := StringField[routeModel, routeSDK]{
		Wire:      "static-route_interface",
		Model:     func(m *routeModel) *types.String { return &m.Interface },
		SDK:       func(s *routeSDK) *string { return &s.Interface },
		Elide:     NullZero,
		WriteWhen: func(m *routeModel) bool { return m.Type.ValueString() == "interface-route" },
	}
	model := routeModel{
		Type:      types.StringValue("nexthop-route"),
		Interface: types.StringValue("WAN1"),
	}
	var sdk routeSDK
	if d := f.ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.Interface != "" {
		t.Errorf("interface was sent on a nexthop-route: %q", sdk.Interface)
	}
	if f.SetInPlan(&model) {
		t.Error("the suppressed plain field is still in the wire mask")
	}
}
