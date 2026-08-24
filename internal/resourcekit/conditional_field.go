package resourcekit

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// Two independent additions, in one file because they touch the same struct:
// a value-type parameter, since every field kind hard-codes its model type
// and a custom-typed attribute (static_route's next_hop, ap_group's
// device_macs) can't bind to StringField; and a write predicate, since the
// SDK accessor takes *S and can inspect the struct to decide which field to
// write, but the Model accessor returns a pointer and can decide nothing --
// a field written only when another model field holds a value has no other
// way to say so.

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

// SetInPlan also honours the predicate: the mask is built from this, so a
// suppressed field still reporting true would be named on the wire carrying
// whatever the SDK struct held -- worse than not suppressing at all.
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

// StringLikePtrField maps any string-backed value to a **string, sending
// nothing for a null, an unknown, or an empty string. The empty case is the
// point, not tidiness: site_to_site_vpn's controller rejects "" for its IP
// and enum fields, so a pointer to an empty string is a failed request, not
// a weaker way of omitting the field.
//
// One kind covers both the plain and the custom-typed case, since
// types.String satisfies basetypes.StringValuable just as a custom type does.
type StringLikePtrField[M any, S any, T basetypes.StringValuable] struct {
	Wire  string
	Model func(*M) *T
	SDK   func(*S) **string
	New   func(basetypes.StringValue) T
}

func (f StringLikePtrField[M, S, T]) WireName() string { return f.Wire }

func (f StringLikePtrField[M, S, T]) ToSDK(ctx context.Context, model *M, sdk *S) diag.Diagnostics {
	value, diags := (*f.Model(model)).ToStringValue(ctx)
	if diags.HasError() {
		return diags
	}
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return diags
	}
	raw := value.ValueString()
	*f.SDK(sdk) = &raw
	return diags
}

// ToModel reads nil back as null. There is no Elide: a pointer that is nil and
// a pointer to "" both mean absent here, because ToSDK never produces the
// latter, so there is no third state for a setting to choose between.
func (f StringLikePtrField[M, S, T]) ToModel(_ context.Context, sdk *S, model *M) diag.Diagnostics {
	raw := *f.SDK(sdk)
	if raw == nil || *raw == "" {
		*f.Model(model) = f.New(basetypes.NewStringNull())
		return nil
	}
	*f.Model(model) = f.New(basetypes.NewStringValue(*raw))
	return nil
}

func (f StringLikePtrField[M, S, T]) SetInPlan(plan *M) bool {
	value, diags := (*f.Model(plan)).ToStringValue(context.Background())
	if diags.HasError() {
		return false
	}
	return !value.IsNull() && !value.IsUnknown()
}

func (f StringLikePtrField[M, S, T]) CopyPlanToState(plan, state *M) {
	if f.SetInPlan(plan) {
		*f.Model(state) = *f.Model(plan)
	}
}
