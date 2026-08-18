package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// TWO INDEPENDENT ADDITIONS, IN ONE FILE BECAUSE THEY TOUCH THE SAME STRUCT.
// They are not two facets of one idea and should not be read as one.
//
// IDEA ONE: A VALUE-TYPE PARAMETER. Every field kind hard-codes its model type
// -- StringField wants *types.String -- so an attribute declared with a custom
// type cannot bind at all. static_route's next_hop is iptypes.IPAddress and
// ap_group's device_macs are hwtypes.MACAddress; both embed
// basetypes.StringValue, which is what makes one parameterised kind cover them.
// 7 surfaces carry a custom-typed scalar.
//
// IDEA TWO: A WRITE PREDICATE. The SDK accessor takes *S, so it can inspect the
// struct and decide WHICH field to write -- that is why vpn_server's four
// type-dispatched writes need nothing new. The Model accessor returns a
// pointer, so it can decide nothing; a field that must be written only when
// ANOTHER model field holds a value has no way to say so. static_route sends
// `interface` only for an interface-route and `next_hop` only for a
// nexthop-route. 9 surfaces have a model-side conditional.
//
// NO AMOUNT OF PARAMETERISING THE VALUE TYPE PROVIDES THE SECOND. That is the
// measured reason these are two ideas rather than one: the asymmetry is in
// which side of the mapping each accessor can see, not in what it returns.

// StringLikeField maps any string-backed custom type to a plain SDK string.
//
// New wraps a basetypes.StringValue rather than a raw string so a NULL survives
// the round trip: iptypes.IPAddress{StringValue: v} is a legal literal because
// the custom types embed it, and NewIPAddressValue(string) cannot express null.
type StringLikeField[M any, S any, T basetypes.StringValuable] struct {
	Wire  string
	Model func(*M) *T
	SDK   func(*S) *string
	New   func(basetypes.StringValue) T
	Elide ElideZero

	// WriteWhen suppresses the write when it returns false. Nil means always.
	WriteWhen func(*M) bool
}

func (f StringLikeField[M, S, T]) WireName() string { return f.Wire }

func (f StringLikeField[M, S, T]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	if f.WriteWhen != nil && !f.WriteWhen(model) {
		return nil
	}
	value, diags := (*f.Model(model)).ToStringValue(ctx)
	if diags.HasError() {
		return diags
	}
	if value.IsNull() || value.IsUnknown() {
		return diags
	}
	*f.SDK(sdk) = value.ValueString()
	return diags
}

func (f StringLikeField[M, S, T]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == "" && bool(f.Elide) {
		*f.Model(model) = f.New(basetypes.NewStringNull())
		return nil
	}
	*f.Model(model) = f.New(basetypes.NewStringValue(raw))
	return nil
}

// SetInPlan ALSO honours the predicate, and that is the half a careless
// implementation would miss: the field mask is built from SetInPlan, so a
// suppressed field that still reported true would be NAMED on the wire while
// carrying whatever the SDK struct happened to hold. Suppressing the write
// without suppressing the mask is worse than not suppressing at all.
func (f StringLikeField[M, S, T]) SetInPlan(plan *M) bool {
	if f.WriteWhen != nil && !f.WriteWhen(plan) {
		return false
	}
	value, diags := (*f.Model(plan)).ToStringValue(context.Background())
	if diags.HasError() {
		return false
	}
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringLikeField[M, S, T]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}
