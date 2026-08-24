package unifi

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	listresource_firewall_policy "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_firewall_policy"
	resource_firewall_policy "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_policy"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// firewallPolicyScheduleAlways is the schedule a new policy gets.
const firewallPolicyScheduleAlways = "ALWAYS"

// app_ids/app_category_ids (go-unifi v1.105.0's application-based firewall
// matching) are deliberately unmodeled here: matching_target's validator
// still rejects APP/APP_CATEGORY, since there's no catalog attribute to
// populate them from.

type firewallPolicyKitModel struct {
	ID                  types.String   `tfsdk:"id"`
	Site                types.String   `tfsdk:"site"`
	Name                types.String   `tfsdk:"name"`
	Action              types.String   `tfsdk:"action"`
	Enabled             types.Bool     `tfsdk:"enabled"`
	Protocol            types.String   `tfsdk:"protocol"`
	Description         types.String   `tfsdk:"description"`
	Logging             types.Bool     `tfsdk:"logging"`
	Index               types.Int64    `tfsdk:"index"`
	CreateAllowRespond  types.Bool     `tfsdk:"create_allow_respond"`
	IPVersion           types.String   `tfsdk:"ip_version"`
	ConnectionStateType types.String   `tfsdk:"connection_state_type"`
	ConnectionStates    types.List     `tfsdk:"connection_states"`
	ICMPTypename        types.String   `tfsdk:"icmp_typename"`
	ICMPV6Typename      types.String   `tfsdk:"icmp_v6_typename"`
	Schedule            types.Object   `tfsdk:"schedule"`
	Source              types.Object   `tfsdk:"source"`
	Destination         types.Object   `tfsdk:"destination"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

// firewallPolicyEndpointValues is the shape FirewallPolicySource and
// FirewallPolicyDestination both have, converted separately since Go can't
// reach a struct field generically.
type firewallPolicyEndpointValues struct {
	ZoneID                string
	MatchingTarget        string
	MatchingTargetType    string
	Port                  string
	PortGroupID           string
	IPGroupID             string
	PortMatchingType      string
	MatchMAC              bool
	MatchOppositeIPs      bool
	MatchOppositeNetworks bool
	MatchOppositePorts    bool
	NetworkIDs            []string
	ClientMACs            []string
	IPs                   []string
	WebDomains            []string
}

func firewallPolicyEndpointAttrTypes() map[string]attr.Type {
	return firewallPolicyEndpointModel{}.AttributeTypes()
}

// firewallPolicyEndpointValuesFrom reads an endpoint object and derives
// matching_target_type on the way out: it's Computed-only, and the controller
// rejects a specific match with an empty type, or a group reference sent as
// SPECIFIC.
func firewallPolicyEndpointValuesFrom(
	ctx context.Context,
	object types.Object,
) (firewallPolicyEndpointValues, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m firewallPolicyEndpointModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return firewallPolicyEndpointValues{}, diags
	}

	values := firewallPolicyEndpointValues{
		ZoneID:         m.ZoneID.ValueString(),
		MatchingTarget: m.MatchingTarget.ValueString(),
		MatchingTargetType: firewallPolicyMatchingTargetType(
			m.MatchingTarget.ValueString(),
			m.MatchingTargetType.ValueString(),
			m.IPGroupID.ValueString(),
		),
		Port:                  m.Port.ValueString(),
		PortGroupID:           m.PortGroupID.ValueString(),
		IPGroupID:             m.IPGroupID.ValueString(),
		PortMatchingType:      m.PortMatchingType.ValueString(),
		MatchMAC:              m.MatchMAC.ValueBool(),
		MatchOppositeIPs:      m.MatchOppositeIPs.ValueBool(),
		MatchOppositeNetworks: m.MatchOppositeNetworks.ValueBool(),
		MatchOppositePorts:    m.MatchOppositePorts.ValueBool(),
	}
	for _, list := range []struct {
		from types.List
		into *[]string
	}{
		{m.NetworkIDs, &values.NetworkIDs},
		{m.ClientMACs, &values.ClientMACs},
		{m.IPs, &values.IPs},
		{m.WebDomains, &values.WebDomains},
	} {
		if list.from.IsNull() || list.from.IsUnknown() {
			continue
		}
		diags.Append(list.from.ElementsAs(ctx, list.into, false)...)
	}
	return values, diags
}

// firewallPolicyEndpointObjectFrom builds the model's object from what the
// controller returned. The controller's matching_target_type is taken as given:
// it owns the attribute, and preferring a locally derived value here is what
// makes an apply report a change nobody asked for.
func firewallPolicyEndpointObjectFrom(
	ctx context.Context,
	values firewallPolicyEndpointValues,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	m := firewallPolicyEndpointModel{
		ZoneID:                types.StringValue(values.ZoneID),
		MatchingTarget:        types.StringValue(values.MatchingTarget),
		MatchingTargetType:    types.StringValue(values.MatchingTargetType),
		Port:                  portToStringValue(values.Port),
		PortGroupID:           types.StringValue(values.PortGroupID),
		IPGroupID:             types.StringValue(values.IPGroupID),
		PortMatchingType:      types.StringValue(values.PortMatchingType),
		MatchMAC:              types.BoolValue(values.MatchMAC),
		MatchOppositeIPs:      types.BoolValue(values.MatchOppositeIPs),
		MatchOppositeNetworks: types.BoolValue(values.MatchOppositeNetworks),
		MatchOppositePorts:    types.BoolValue(values.MatchOppositePorts),
	}
	for _, list := range []struct {
		from []string
		into *types.List
	}{
		{values.NetworkIDs, &m.NetworkIDs},
		{values.ClientMACs, &m.ClientMACs},
		{values.IPs, &m.IPs},
		{values.WebDomains, &m.WebDomains},
	} {
		value, d := types.ListValueFrom(ctx, types.StringType, list.from)
		diags.Append(d...)
		*list.into = value
	}
	object, d := types.ObjectValueFrom(ctx, firewallPolicyEndpointAttrTypes(), m)
	diags.Append(d...)
	return object, diags
}

// firewallPolicyCarrySchedule handles the create/update asymmetry: the
// controller rejects a null or absent schedule on every write, so a value
// must always be sent. schedule is a real ObjectField now, so this hook is
// only the fallback for the two cases the model carries nothing for -- a
// create (gets the always-on literal) and an update whose state predates the
// attribute (re-reads the controller's own schedule). Once state holds it,
// Computed+UseStateForUnknown take over and this hook stops mattering. If the
// read fails, the literal stands rather than failing an otherwise-successful
// apply.
func firewallPolicyCarrySchedule(
	read func(context.Context, string, string) (*ui.FirewallPolicy, error),
	defaultSite string,
) func(context.Context, *firewallPolicyKitModel, *firewallPolicyKitModel, *ui.FirewallPolicy, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		_, effective *firewallPolicyKitModel,
		sdk *ui.FirewallPolicy,
		_ any,
	) diag.Diagnostics {
		// ToSDK already encoded a declared schedule; only fill in when nothing
		// did, or this would make the attribute unsettable.
		if sdk.Schedule != nil {
			return nil
		}

		sdk.Schedule = &ui.FirewallPolicySchedule{Mode: firewallPolicyScheduleAlways}

		// An empty id means create (Computed id is unknown in the plan);
		// getting this branch wrong is destructive and silent, which is what
		// TestBeforeSendSeesAnEmptyIDOnCreateAndTheRealOneOnUpdate pins.
		id := effective.ID.ValueString()
		if id == "" || read == nil {
			return nil
		}
		// Falls back to defaultSite: getting the site wrong makes the read
		// fail silently, leaving the literal in place and resetting the
		// practitioner's schedule with no diff shown.
		site := effective.Site.ValueString()
		if site == "" {
			site = defaultSite
		}
		current, err := read(ctx, site, id)
		if err != nil || current == nil || current.Schedule == nil {
			return nil
		}
		sdk.Schedule = current.Schedule
		return nil
	}
}

// firewallPolicyScheduleAttrTypes types the schedule block in state.
func firewallPolicyScheduleAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"mode":             types.StringType,
		"date":             types.StringType,
		"date_start":       types.StringType,
		"date_end":         types.StringType,
		"repeat_on_days":   types.ListType{ElemType: types.StringType},
		"time_all_day":     types.BoolType,
		"time_range_start": types.StringType,
		"time_range_end":   types.StringType,
	}
}

type firewallPolicyScheduleModel struct {
	Mode           types.String `tfsdk:"mode"`
	Date           types.String `tfsdk:"date"`
	DateStart      types.String `tfsdk:"date_start"`
	DateEnd        types.String `tfsdk:"date_end"`
	RepeatOnDays   types.List   `tfsdk:"repeat_on_days"`
	TimeAllDay     types.Bool   `tfsdk:"time_all_day"`
	TimeRangeStart types.String `tfsdk:"time_range_start"`
	TimeRangeEnd   types.String `tfsdk:"time_range_end"`
}

func firewallPolicyKitSpec() resourcekit.Spec[firewallPolicyKitModel, ui.FirewallPolicy] {
	return resourcekit.Spec[firewallPolicyKitModel, ui.FirewallPolicy]{
		TypeName: "firewall_policy",
		Subject:  "Firewall Policy",
		New:      func() *ui.FirewallPolicy { return &ui.FirewallPolicy{} },
		ID:       func(m *firewallPolicyKitModel) *types.String { return &m.ID },
		Site:     func(m *firewallPolicyKitModel) *types.String { return &m.Site },
		Timeouts: func(m *firewallPolicyKitModel) *timeouts.Value { return &m.Timeouts },

		// schedule joins the mask without being a Field, because no Field could
		// put it there: nothing in the model carries it. AlwaysWire is the
		// mechanism built for exactly a hook-supplied value.
		AlwaysWire: []string{"schedule"},

		Fields: []resourcekit.Field[firewallPolicyKitModel, ui.FirewallPolicy]{
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "name",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "action",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.Action },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.Action },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "enabled",
				Model: func(m *firewallPolicyKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.FirewallPolicy) *bool { return &s.Enabled },
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "protocol",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.Protocol },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.Protocol },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "description",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.Description },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.Description },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "logging",
				Model: func(m *firewallPolicyKitModel) *types.Bool { return &m.Logging },
				SDK:   func(s *ui.FirewallPolicy) *bool { return &s.Logging },
			},
			resourcekit.BoolField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "create_allow_respond",
				Model: func(m *firewallPolicyKitModel) *types.Bool { return &m.CreateAllowRespond },
				SDK:   func(s *ui.FirewallPolicy) *bool { return &s.CreateAllowRespond },
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "ip_version",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.IPVersion },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.Version },
				Elide: resourcekit.NullZero,
			},
			// The controller requires these back on every PUT: an omitted
			// connection_state_type or icmp_typename fails the write with
			// HTTP 400. They are Computed-only, so nothing a practitioner
			// writes reaches them.
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "connection_state_type",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.ConnectionStateType },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.ConnectionStateType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringListField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "connection_states",
				Model: func(m *firewallPolicyKitModel) *types.List { return &m.ConnectionStates },
				SDK:   func(s *ui.FirewallPolicy) *[]string { return &s.ConnectionStates },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "icmp_typename",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.ICMPTypename },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.ICMPTypename },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[firewallPolicyKitModel, ui.FirewallPolicy]{
				Wire:  "icmp_v6_typename",
				Model: func(m *firewallPolicyKitModel) *types.String { return &m.ICMPV6Typename },
				SDK:   func(s *ui.FirewallPolicy) *string { return &s.ICMPV6Typename },
				Elide: resourcekit.KeepZero,
			},
			// Read-only, so it never joins the mask: index is
			// controller-assigned, and UniFi ignores a client-supplied value.
			resourcekit.ReadOnly[firewallPolicyKitModel, ui.FirewallPolicy](
				resourcekit.Int64PtrField[firewallPolicyKitModel, ui.FirewallPolicy]{
					Wire:  "index",
					Model: func(m *firewallPolicyKitModel) *types.Int64 { return &m.Index },
					SDK:   func(s *ui.FirewallPolicy) **int64 { return &s.Index },
				},
			),
			resourcekit.ObjectField[
				firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicySchedule,
			]{
				Wire:      "schedule",
				Model:     func(m *firewallPolicyKitModel) *types.Object { return &m.Schedule },
				SDK:       func(s *ui.FirewallPolicy) **ui.FirewallPolicySchedule { return &s.Schedule },
				AttrTypes: firewallPolicyScheduleAttrTypes(),
				Encode: func(ctx context.Context, object types.Object) (*ui.FirewallPolicySchedule, diag.Diagnostics) {
					var diags diag.Diagnostics
					var m firewallPolicyScheduleModel
					diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
					if diags.HasError() {
						return nil, diags
					}
					out := &ui.FirewallPolicySchedule{
						Mode:           m.Mode.ValueString(),
						Date:           m.Date.ValueString(),
						DateStart:      m.DateStart.ValueString(),
						DateEnd:        m.DateEnd.ValueString(),
						TimeRangeStart: m.TimeRangeStart.ValueString(),
						TimeRangeEnd:   m.TimeRangeEnd.ValueString(),
						TimeAllDay:     m.TimeAllDay.ValueBoolPointer(),
					}
					if !m.RepeatOnDays.IsNull() && !m.RepeatOnDays.IsUnknown() {
						diags.Append(m.RepeatOnDays.ElementsAs(ctx, &out.RepeatOnDays, false)...)
					}
					// An undeclared mode would be refused by the controller as
					// null; default to always-on rather than leave it invalid.
					if out.Mode == "" {
						out.Mode = firewallPolicyScheduleAlways
					}
					return out, diags
				},
				Decode: func(ctx context.Context, sdk *ui.FirewallPolicySchedule) (types.Object, diag.Diagnostics) {
					var diags diag.Diagnostics
					days, d := types.ListValueFrom(ctx, types.StringType, sdk.RepeatOnDays)
					diags.Append(d...)
					object, d := types.ObjectValueFrom(ctx, firewallPolicyScheduleAttrTypes(),
						firewallPolicyScheduleModel{
							Mode:           types.StringValue(sdk.Mode),
							Date:           types.StringValue(sdk.Date),
							DateStart:      types.StringValue(sdk.DateStart),
							DateEnd:        types.StringValue(sdk.DateEnd),
							RepeatOnDays:   days,
							TimeAllDay:     types.BoolPointerValue(sdk.TimeAllDay),
							TimeRangeStart: types.StringValue(sdk.TimeRangeStart),
							TimeRangeEnd:   types.StringValue(sdk.TimeRangeEnd),
						})
					diags.Append(d...)
					return object, diags
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectField[
				firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicySource,
			]{
				Wire:      "source",
				Model:     func(m *firewallPolicyKitModel) *types.Object { return &m.Source },
				SDK:       func(s *ui.FirewallPolicy) **ui.FirewallPolicySource { return &s.Source },
				AttrTypes: firewallPolicyEndpointAttrTypes(),
				Encode: func(ctx context.Context, object types.Object) (*ui.FirewallPolicySource, diag.Diagnostics) {
					v, diags := firewallPolicyEndpointValuesFrom(ctx, object)
					if diags.HasError() {
						return nil, diags
					}
					return &ui.FirewallPolicySource{
						ZoneID: v.ZoneID, MatchingTarget: v.MatchingTarget,
						MatchingTargetType: v.MatchingTargetType,
						Port:               v.Port, PortGroupID: v.PortGroupID, IPGroupID: v.IPGroupID,
						PortMatchingType: v.PortMatchingType,
						MatchMAC:         v.MatchMAC, MatchOppositeIPs: v.MatchOppositeIPs,
						MatchOppositeNetworks: v.MatchOppositeNetworks,
						MatchOppositePorts:    v.MatchOppositePorts,
						NetworkIDs:            v.NetworkIDs, ClientMACs: v.ClientMACs,
						IPs: v.IPs, WebDomains: v.WebDomains,
					}, diags
				},
				Decode: func(ctx context.Context, sdk *ui.FirewallPolicySource) (types.Object, diag.Diagnostics) {
					return firewallPolicyEndpointObjectFrom(ctx, firewallPolicyEndpointValues{
						ZoneID: sdk.ZoneID, MatchingTarget: sdk.MatchingTarget,
						MatchingTargetType: sdk.MatchingTargetType,
						Port:               sdk.Port, PortGroupID: sdk.PortGroupID, IPGroupID: sdk.IPGroupID,
						PortMatchingType: sdk.PortMatchingType,
						MatchMAC:         sdk.MatchMAC, MatchOppositeIPs: sdk.MatchOppositeIPs,
						MatchOppositeNetworks: sdk.MatchOppositeNetworks,
						MatchOppositePorts:    sdk.MatchOppositePorts,
						NetworkIDs:            sdk.NetworkIDs, ClientMACs: sdk.ClientMACs,
						IPs: sdk.IPs, WebDomains: sdk.WebDomains,
					})
				},
				Elide: resourcekit.KeepZero,
			},
			resourcekit.ObjectField[
				firewallPolicyKitModel, ui.FirewallPolicy, ui.FirewallPolicyDestination,
			]{
				Wire:      "destination",
				Model:     func(m *firewallPolicyKitModel) *types.Object { return &m.Destination },
				SDK:       func(s *ui.FirewallPolicy) **ui.FirewallPolicyDestination { return &s.Destination },
				AttrTypes: firewallPolicyEndpointAttrTypes(),
				Encode: func(ctx context.Context, object types.Object) (*ui.FirewallPolicyDestination, diag.Diagnostics) {
					v, diags := firewallPolicyEndpointValuesFrom(ctx, object)
					if diags.HasError() {
						return nil, diags
					}
					return &ui.FirewallPolicyDestination{
						ZoneID: v.ZoneID, MatchingTarget: v.MatchingTarget,
						MatchingTargetType: v.MatchingTargetType,
						Port:               v.Port, PortGroupID: v.PortGroupID, IPGroupID: v.IPGroupID,
						PortMatchingType: v.PortMatchingType,
						MatchMAC:         v.MatchMAC, MatchOppositeIPs: v.MatchOppositeIPs,
						MatchOppositeNetworks: v.MatchOppositeNetworks,
						MatchOppositePorts:    v.MatchOppositePorts,
						NetworkIDs:            v.NetworkIDs, ClientMACs: v.ClientMACs,
						IPs: v.IPs, WebDomains: v.WebDomains,
					}, diags
				},
				Decode: func(ctx context.Context, sdk *ui.FirewallPolicyDestination) (types.Object, diag.Diagnostics) {
					return firewallPolicyEndpointObjectFrom(ctx, firewallPolicyEndpointValues{
						ZoneID: sdk.ZoneID, MatchingTarget: sdk.MatchingTarget,
						MatchingTargetType: sdk.MatchingTargetType,
						Port:               sdk.Port, PortGroupID: sdk.PortGroupID, IPGroupID: sdk.IPGroupID,
						PortMatchingType: sdk.PortMatchingType,
						MatchMAC:         sdk.MatchMAC, MatchOppositeIPs: sdk.MatchOppositeIPs,
						MatchOppositeNetworks: sdk.MatchOppositeNetworks,
						MatchOppositePorts:    sdk.MatchOppositePorts,
						NetworkIDs:            sdk.NetworkIDs, ClientMACs: sdk.ClientMACs,
						IPs: sdk.IPs, WebDomains: sdk.WebDomains,
					})
				},
				Elide: resourcekit.KeepZero,
			},
		},
	}
}

func firewallPolicyKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.FirewallPolicy] {
	return resourcekit.Backend[ui.FirewallPolicy]{
		GetID: func(s *ui.FirewallPolicy) string { return s.ID },
		SetID: func(s *ui.FirewallPolicy, id string) { s.ID = id },
		Create: func(ctx context.Context, site string, in *ui.FirewallPolicy) (*ui.FirewallPolicy, error) {
			return client.CreateFirewallPolicy(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.FirewallPolicy, error) {
			return client.GetFirewallPolicy(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.FirewallPolicy, fields ...string) (*ui.FirewallPolicy, error) {
			return client.UpdateFirewallPolicyFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteFirewallPolicy(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.FirewallPolicy, error) {
			return client.ListFirewallPolicy(ctx, site)
		},
	}
}

func firewallPolicyKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_firewall_policy.FirewallPolicyResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func firewallPolicyKitList() resourcekit.ListSpec[ui.FirewallPolicy] {
	return resourcekit.ListSpec[ui.FirewallPolicy]{
		ConfigSchema: listresource_firewall_policy.FirewallPolicyListResourceSchema,
		DisplayName: func(s *ui.FirewallPolicy) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.FirewallPolicy) string{
			"name":    func(s *ui.FirewallPolicy) string { return s.Name },
			"action":  func(s *ui.FirewallPolicy) string { return s.Action },
			"enabled": func(s *ui.FirewallPolicy) string { return strconv.FormatBool(s.Enabled) },
		},
	}
}
