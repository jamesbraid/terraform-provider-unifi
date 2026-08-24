package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// Adapters, not replacements: firewall_policy's mappers moved into the
// descriptor, and the tests that covered them are records of what a live
// controller accepts -- the four inversion flags, the matching_target_type
// derivation for a group reference, the connection_states round-trip.
// Rewriting those assertions to fit new call sites would be rewriting the
// evidence.
//
// So the old names survive here as thin adapters onto the descriptor. Every
// assertion in firewall_policy_resource_test.go is unchanged and now runs
// against the code that actually ships.

type firewallPolicyResource = firewallPolicyKitResource

type firewallPolicyModel = firewallPolicyKitModel

// firewallPolicyEndpointFields pulls the two ObjectFields out of the spec the
// resource actually serves, rather than rebuilding them.
//
// They used to be built by their own helper functions, which the descriptor
// mapping parser refuses: it needs a field constructor to take Wire, Model and
// SDK as its own parameters, or it cannot attribute the fields it builds. So
// the literals are inline in Fields now, and reaching them through the spec is
// what keeps these adapters exercising the shipping objects instead of copies
// that could drift from them.
func firewallPolicySourceField() resourcekit.ObjectField[
	firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicySource,
] {
	for _, field := range firewallPolicyKitSpec().Fields {
		if f, ok := field.(resourcekit.ObjectField[
			firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicySource,
		]); ok {
			return f
		}
	}
	panic("the firewall_policy spec has no source ObjectField")
}

func firewallPolicyDestinationField() resourcekit.ObjectField[
	firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicyDestination,
] {
	for _, field := range firewallPolicyKitSpec().Fields {
		if f, ok := field.(resourcekit.ObjectField[
			firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicyDestination,
		]); ok {
			return f
		}
	}
	panic("the firewall_policy spec has no destination ObjectField")
}

func modelToFirewallPolicy(
	ctx context.Context,
	model firewallPolicyModel,
) (*ui.FirewallPolicy, diag.Diagnostics) {
	spec := firewallPolicyKitSpec()
	sdk := spec.New()
	var diags diag.Diagnostics
	for _, field := range spec.Fields {
		diags.Append(field.ToSDK(ctx, &model, sdk)...)
	}
	sdk.ID = model.ID.ValueString()
	// The hand-written mapper set the create literal inline; the descriptor
	// sets it in BeforeSend, which the field loop above does not run -- so this
	// adapter has to apply the same rule BeforeSend does: a declared schedule
	// (the field loop already encoded one from the model) is left alone, and
	// only an absent one falls back to the literal. Overwriting unconditionally
	// was itself a bug hiding in the adapter: it discarded whatever the model
	// actually carried, which is invisible as long as no caller sets
	// model.Schedule -- and silently wrong the moment one does (see
	// TestFirewallPolicyScheduleRoundTripsControllerValue).
	if sdk.Schedule == nil {
		sdk.Schedule = &ui.FirewallPolicySchedule{Mode: firewallPolicyScheduleAlways}
	}
	return sdk, diags
}

func firewallPolicyToModel(
	ctx context.Context,
	fp *ui.FirewallPolicy,
	model *firewallPolicyModel,
) diag.Diagnostics {
	spec := firewallPolicyKitSpec()
	var diags diag.Diagnostics
	for _, field := range spec.Fields {
		diags.Append(field.ToModel(ctx, fp, model)...)
	}
	model.ID = types.StringValue(fp.ID)
	return diags
}

// normaliseEndpointLists gives every unset list a string element type. The
// adapter needs this and the original didn't: the hand-written mapper read
// model fields directly, so a test model that left web_domains at its
// zero value passed a zero types.List and IsNull() answered true. Going
// through types.ObjectValueFrom instead requires a typed value -- a zero
// list reports its element type as missing and the conversion is refused.
func normaliseEndpointLists(m firewallPolicyEndpointModel) firewallPolicyEndpointModel {
	for _, list := range []*types.List{&m.NetworkIDs, &m.ClientMACs, &m.IPs, &m.WebDomains} {
		if list.ElementType(context.Background()) == nil {
			*list = types.ListNull(types.StringType)
		}
	}
	return m
}

func endpointModelToSource(
	ctx context.Context,
	m firewallPolicyEndpointModel,
	diags *diag.Diagnostics,
) *ui.FirewallPolicySource {
	object, d := types.ObjectValueFrom(ctx, firewallPolicyEndpointAttrTypes(), normaliseEndpointLists(m))
	diags.Append(d...)
	if diags.HasError() {
		return nil
	}
	encoded, ed := firewallPolicySourceField().Encode(ctx, object)
	diags.Append(ed...)
	return encoded
}

func endpointModelToDestination(
	ctx context.Context,
	m firewallPolicyEndpointModel,
	diags *diag.Diagnostics,
) *ui.FirewallPolicyDestination {
	object, d := types.ObjectValueFrom(ctx, firewallPolicyEndpointAttrTypes(), normaliseEndpointLists(m))
	diags.Append(d...)
	if diags.HasError() {
		return nil
	}
	encoded, ed := firewallPolicyDestinationField().Encode(ctx, object)
	diags.Append(ed...)
	return encoded
}

func apiSourceToEndpointModel(
	ctx context.Context,
	src *ui.FirewallPolicySource,
	diags *diag.Diagnostics,
) firewallPolicyEndpointModel {
	object, d := firewallPolicySourceField().Decode(ctx, src)
	diags.Append(d...)
	var m firewallPolicyEndpointModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	return m
}

func apiDestinationToEndpointModel(
	ctx context.Context,
	dst *ui.FirewallPolicyDestination,
	diags *diag.Diagnostics,
) firewallPolicyEndpointModel {
	object, d := firewallPolicyDestinationField().Decode(ctx, dst)
	diags.Append(d...)
	var m firewallPolicyEndpointModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	return m
}

// endpointMatchingTargetType and withMatchingTargetType were the hand-written
// Update's way of writing a derived value back into the plan. The kit leaves
// matching_target_type unknown and takes the controller's answer, so neither
// has a caller in the resource -- but the tests that pinned their behaviour are
// still worth running, because the derivation itself lives on in Encode.
func endpointMatchingTargetType(
	ctx context.Context,
	obj types.Object,
	diags *diag.Diagnostics,
) types.String {
	if obj.IsNull() || obj.IsUnknown() {
		return types.StringNull()
	}
	var m firewallPolicyEndpointModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	return m.MatchingTargetType
}

func withMatchingTargetType(
	ctx context.Context,
	obj types.Object,
	mtt types.String,
	diags *diag.Diagnostics,
) types.Object {
	if obj.IsNull() || obj.IsUnknown() {
		return obj
	}
	var m firewallPolicyEndpointModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	m.MatchingTargetType = mtt
	newObj, d := types.ObjectValueFrom(ctx, firewallPolicyEndpointAttrTypes(), m)
	diags.Append(d...)
	return newObj
}

// firewallPolicyListToModel was ToModel plus the site, which is what the kit's
// list path does for every surface. Kept as an adapter so its test still runs.
func (r *firewallPolicyKitResource) firewallPolicyListToModel(
	ctx context.Context,
	api *ui.FirewallPolicy,
	model *firewallPolicyModel,
	site string,
) diag.Diagnostics {
	diags := firewallPolicyToModel(ctx, api, model)
	model.Site = types.StringValue(site)
	return diags
}
