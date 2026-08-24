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
	if _, ok := any(back.NextHop).(iptypes.IPAddress); !ok {
		t.Error("the round trip lost the custom type; a schema declaring it " +
			"would fail at plan time against a plain string instead")
	}
}

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

func TestTheWritePredicateSuppressesTheWrite(t *testing.T) {
	ctx := context.Background()
	f := nextHopField()
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

	model.Type = types.StringValue("nexthop-route")
	sdk = routeSDK{}
	if d := f.ToSDK(ctx, &model, &sdk); d.HasError() {
		t.Fatal(d)
	}
	if sdk.NextHop != "192.0.2.1" {
		t.Fatal("the field never writes at all, so the suppression above proves nothing")
	}
}

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

type ptrModel struct {
	Plain types.String        `tfsdk:"plain"`
	IP    iptypes.IPv4Address `tfsdk:"ip"`
}

type ptrSDK struct {
	Plain *string
	IP    *string
}

func plainPtrField() StringLikePtrField[ptrModel, ptrSDK, types.String] {
	return StringLikePtrField[ptrModel, ptrSDK, types.String]{
		Wire:  "plain",
		Model: func(m *ptrModel) *types.String { return &m.Plain },
		SDK:   func(s *ptrSDK) **string { return &s.Plain },
		New:   func(v basetypes.StringValue) types.String { return v },
	}
}

func TestAPointerStringOmitsTheEmptyValue(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		model types.String
		want  *string
	}{
		{"null sends nothing", types.StringNull(), nil},
		{"unknown sends nothing", types.StringUnknown(), nil},
		{"empty sends nothing", types.StringValue(""), nil},
		{"a real value is sent", types.StringValue("ike2"), ptrTo("ike2")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var sdk ptrSDK
			model := ptrModel{Plain: testCase.model}
			if diags := plainPtrField().ToSDK(context.Background(), &model, &sdk); diags.HasError() {
				t.Fatalf("ToSDK: %v", diags)
			}
			switch {
			case testCase.want == nil && sdk.Plain != nil:
				t.Fatalf("sent a pointer to %q, want nothing", *sdk.Plain)
			case testCase.want != nil && sdk.Plain == nil:
				t.Fatalf("sent nothing, want %q", *testCase.want)
			case testCase.want != nil && *sdk.Plain != *testCase.want:
				t.Fatalf("sent %q, want %q", *sdk.Plain, *testCase.want)
			}
		})
	}
}

func ptrTo(s string) *string { return &s }

func TestAPointerStringCarriesACustomType(t *testing.T) {
	field := StringLikePtrField[ptrModel, ptrSDK, iptypes.IPv4Address]{
		Wire:  "ip",
		Model: func(m *ptrModel) *iptypes.IPv4Address { return &m.IP },
		SDK:   func(s *ptrSDK) **string { return &s.IP },
		New:   func(v basetypes.StringValue) iptypes.IPv4Address { return iptypes.IPv4Address{StringValue: v} },
	}
	var model ptrModel
	sdk := ptrSDK{IP: ptrTo("10.0.0.1")}
	if diags := field.ToModel(context.Background(), &sdk, &model); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if model.IP.ValueString() != "10.0.0.1" {
		t.Fatalf("IP = %q, want 10.0.0.1", model.IP.ValueString())
	}
	sdk.IP = nil
	if diags := field.ToModel(context.Background(), &sdk, &model); diags.HasError() {
		t.Fatalf("ToModel: %v", diags)
	}
	if !model.IP.IsNull() {
		t.Errorf("a nil pointer read back as %q, want null", model.IP.ValueString())
	}
}
