package unifi

// client's descriptor.
//
// The surface is small in attributes and large in CRUD -- 741 of its 1,358
// lines -- and most of what is interesting about it is DERIVED rather than
// mapped. Three attributes have no wire of their own: qos_rate resolves to a
// usergroup id and may CREATE the group, groups translates network-members
// group ids to names and back, and fixed_ip and local_dns_record each carry a
// companion boolean the practitioner never sets.
//
// THE IDEMPOTENT HALF IS IN Prefetch AND THE REST IS IN BeforeSend, which is
// the line the kit already draws: Prefetch runs on READ as well as write, and
// BeforeSend does not. Listing the site's groups is a GET and belongs in the
// first; creating a usergroup is not, and belongs in the second. Putting the
// resolution in Prefetch would create a usergroup on every terraform refresh.

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type clientKitModel struct {
	ID             types.String        `tfsdk:"id"`
	Site           types.String        `tfsdk:"site"`
	MAC            hwtypes.MACAddress  `tfsdk:"mac"`
	Name           types.String        `tfsdk:"name"`
	DisplayName    types.String        `tfsdk:"display_name"`
	QOSRate        types.Object        `tfsdk:"qos_rate"`
	Note           types.String        `tfsdk:"note"`
	FixedIP        iptypes.IPv4Address `tfsdk:"fixed_ip"`
	FixedApMAC     hwtypes.MACAddress  `tfsdk:"fixed_ap_mac"`
	NetworkID      types.String        `tfsdk:"network_id"`
	Groups         types.List          `tfsdk:"groups"`
	Blocked        types.Bool          `tfsdk:"blocked"`
	LocalDNSRecord types.String        `tfsdk:"local_dns_record"`

	AllowExisting       types.Bool `tfsdk:"allow_existing"`
	SkipForgetOnDestroy types.Bool `tfsdk:"skip_forget_on_destroy"`

	Hostname types.String `tfsdk:"hostname"`
	LastIP   types.String `tfsdk:"last_ip"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

type clientModel = clientKitModel

func clientStr(
	wire string,
	model func(*clientModel) *types.String,
	sdk func(*ui.Client) *string,
	elide resourcekit.ElideZero,
) resourcekit.StringField[clientModel, ui.Client] {
	return resourcekit.StringField[clientModel, ui.Client]{
		Wire: wire, Model: model, SDK: sdk, Elide: elide,
	}
}

func clientKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.Client] {
	return resourcekit.Backend[ui.Client]{
		Create: func(ctx context.Context, site string, in *ui.Client) (*ui.Client, error) {
			return client.CreateClient(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.Client, error) {
			return client.GetClient(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.Client, fields ...string,
		) (*ui.Client, error) {
			return client.UpdateClientFields(ctx, site, in, fields...)
		},
		// THE DELETE IS KEYED BY MAC AND Backend.Delete IS HANDED AN ID, the
		// same narrowness network's delete has. One read answers it, and the
		// object is about to go anyway.
		Delete: func(ctx context.Context, site, id string) error {
			existing, err := client.GetClient(ctx, site, id)
			if err != nil {
				return err
			}
			return client.DeleteClientByMAC(ctx, site, existing.MAC)
		},
		List: func(ctx context.Context, site string) ([]ui.Client, error) {
			return client.ListClient(ctx, site)
		},
		GetID: func(s *ui.Client) string { return s.ID },
		SetID: func(s *ui.Client, id string) { s.ID = id },
	}
}

// clientGroups is what Prefetch hands the other hooks: the site's two group
// vocabularies, each fetched ONCE per operation.
//
// It replaces a mutex-guarded map on the resource struct that was populated on
// first use and never invalidated, so a group renamed on the controller stayed
// wrong for the life of the process. A value fetched per operation cannot go
// stale that way, and it needs no lock because it is not shared.
type clientGroups struct {
	// usergroups, for qos_rate.
	byID   map[string]ui.ClientGroup
	byName map[string]ui.ClientGroup
	// network-members groups, for the groups attribute.
	memberNameByID map[string]string
	memberIDByName map[string]string
}

// clientKitPrefetch reads both vocabularies. IT DOES NOT WRITE ANYTHING, and
// that is the whole reason it can live here: Prefetch runs on READ as well as
// on create and update, so a side effect here would fire on every refresh.
// Creating the usergroup a qos_rate block asks for is BeforeSend's job.
func clientKitPrefetch(client *ui.ApiClient) func(context.Context, string) (any, diag.Diagnostics) {
	return func(ctx context.Context, site string) (any, diag.Diagnostics) {
		var diags diag.Diagnostics
		groups := &clientGroups{
			byID:           map[string]ui.ClientGroup{},
			byName:         map[string]ui.ClientGroup{},
			memberNameByID: map[string]string{},
			memberIDByName: map[string]string{},
		}
		userGroups, err := client.ListClientGroup(ctx, site)
		if err != nil {
			diags.AddError("Error Listing Client Groups", err.Error())
			return groups, diags
		}
		for _, g := range userGroups {
			groups.byID[g.ID] = g
			groups.byName[g.Name] = g
		}
		memberGroups, err := client.ListNetworkMembersGroups(ctx, site)
		if err != nil {
			diags.AddError("Error Listing Network Members Groups", err.Error())
			return groups, diags
		}
		for _, g := range memberGroups {
			groups.memberNameByID[g.ID] = g.Name
			groups.memberIDByName[g.Name] = g.ID
		}
		return groups, diags
	}
}

// clientKitBeforeSend derives the four things no Field can carry.
//
// ALL FOUR ARE WRITES OR DEPEND ON ONE, which is why they are here rather than
// in Prefetch. usergroup_id may require CREATING or re-rating a usergroup;
// groups translates names the practitioner wrote into ids; and use_fixedip and
// local_dns_record_enabled are companion booleans derived from whether their
// partner attribute is set at all.
func clientKitBeforeSend(
	client *ui.ApiClient,
) func(context.Context, *clientModel, *clientModel, *ui.Client, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		_, effective *clientModel,
		sdk *ui.Client,
		prefetched any,
	) diag.Diagnostics {
		var diags diag.Diagnostics
		groups, _ := prefetched.(*clientGroups)

		// The companions. The practitioner sets the value; the flag follows it,
		// and the controller ignores the value without the flag.
		sdk.UseFixedIP = !effective.FixedIP.IsNull() && effective.FixedIP.ValueString() != ""
		sdk.LocalDNSRecordEnabled = !effective.LocalDNSRecord.IsNull() &&
			effective.LocalDNSRecord.ValueString() != ""
		if !effective.NetworkID.IsNull() && effective.NetworkID.ValueString() != "" {
			sdk.VirtualNetworkOverrideEnabled = util.Ptr(true)
		}

		if !effective.QOSRate.IsNull() && !effective.QOSRate.IsUnknown() {
			var qos qosRateModel
			diags.Append(effective.QOSRate.As(ctx, &qos, basetypes.ObjectAsOptions{})...)
			if diags.HasError() {
				return diags
			}
			id, d := clientResolveGroup(ctx, client, sdk.SiteID, groups, qos)
			diags.Append(d...)
			if diags.HasError() {
				return diags
			}
			sdk.UserGroupID = id
		}

		if !effective.Groups.IsNull() && !effective.Groups.IsUnknown() {
			var names []string
			diags.Append(effective.Groups.ElementsAs(ctx, &names, false)...)
			if diags.HasError() {
				return diags
			}
			ids := make([]string, 0, len(names))
			for _, name := range names {
				id, ok := groups.memberIDByName[name]
				if !ok {
					diags.AddError("Unknown Network Members Group",
						"No network members group on this site is named "+name+
							". Groups are referenced by name and must already exist.")
					continue
				}
				ids = append(ids, id)
			}
			if diags.HasError() {
				return diags
			}
			sdk.NetworkMembersGroupIDs = ids
		}
		return diags
	}
}

// clientKitAfterReceive fills the two attributes read back from ids.
//
// It does NO IO. The hand-written read issued a GetClientGroup per client to
// turn usergroup_id into a qos_rate block; the vocabularies Prefetch already
// holds answer it without another call.
func clientKitAfterReceive(
	_ context.Context, sdk *ui.Client, model *clientModel, _ clientModel, prefetched any,
) diag.Diagnostics {
	var diags diag.Diagnostics

	// BLOCKED READS BACK AS false WHEN THE CONTROLLER OMITS IT, not as null.
	// The schema documents false as the default and the plan carries it, so a
	// null here is an inconsistent-result error on an attribute the
	// practitioner never touched. BoolPtrField cannot say this -- a nil pointer
	// is always null there, by its own documentation -- so the default lands
	// here instead.
	if sdk.Blocked == nil {
		model.Blocked = types.BoolValue(false)
	}

	// TYPED NULLS EVEN WITHOUT THE VOCABULARIES. A zero types.Object is null
	// but UNTYPED, and an untyped null does not fit the schema -- so the
	// no-prefetch path has to set them explicitly rather than leave them.
	groups, ok := prefetched.(*clientGroups)
	if !ok {
		model.QOSRate = types.ObjectNull(qosRateModel{}.AttributeTypes())
		model.Groups = types.ListNull(types.StringType)
		return diags
	}

	if group, found := groups.byID[sdk.UserGroupID]; found {
		object, d := types.ObjectValueFrom(
			context.Background(), qosRateModel{}.AttributeTypes(), qosRateModel{
				ID:      types.StringValue(group.ID),
				Name:    types.StringValue(group.Name),
				MaxUp:   types.Int64PointerValue(group.QOSRateMaxUp),
				MaxDown: types.Int64PointerValue(group.QOSRateMaxDown),
			})
		diags.Append(d...)
		model.QOSRate = object
	} else {
		model.QOSRate = types.ObjectNull(qosRateModel{}.AttributeTypes())
	}

	if len(sdk.NetworkMembersGroupIDs) == 0 {
		model.Groups = types.ListNull(types.StringType)
		return diags
	}
	names := make([]string, 0, len(sdk.NetworkMembersGroupIDs))
	for _, id := range sdk.NetworkMembersGroupIDs {
		if name, found := groups.memberNameByID[id]; found {
			names = append(names, name)
			continue
		}
		// A group the site no longer lists. Reporting the id is more use than
		// dropping it silently, which would read as the client having left it.
		names = append(names, id)
	}
	list, d := types.ListValueFrom(context.Background(), types.StringType, names)
	diags.Append(d...)
	model.Groups = list
	return diags
}

// clientResolveGroup turns a qos_rate block into a usergroup id, creating or
// re-rating the group when it has to.
//
// Transcribed from the hand-written resolveClientGroup, with the site's groups
// coming from Prefetch rather than a list of its own. THE WRITES STAY HERE
// rather than moving up: creating a group on every refresh is what putting this
// in Prefetch would do.
//
// Three cases, and the third is the one worth reading: with neither id nor name
// the group is named after the rates themselves, so two clients asking for the
// same limits share one group rather than accumulating duplicates.
func clientResolveGroup(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	groups *clientGroups,
	qos qosRateModel,
) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	if !qos.ID.IsNull() && !qos.ID.IsUnknown() && qos.ID.ValueString() != "" {
		return qos.ID.ValueString(), diags
	}

	name := qos.Name.ValueString()
	if qos.Name.IsNull() || qos.Name.IsUnknown() || name == "" {
		maxUp, maxDown := int64(-1), int64(-1)
		if !qos.MaxUp.IsNull() && !qos.MaxUp.IsUnknown() {
			maxUp = qos.MaxUp.ValueInt64()
		}
		if !qos.MaxDown.IsNull() && !qos.MaxDown.IsUnknown() {
			maxDown = qos.MaxDown.ValueInt64()
		}
		name = fmt.Sprintf("qos-up%d-down%d", maxUp, maxDown)
	}

	if existing, found := groups.byName[name]; found {
		update := false
		if !qos.MaxUp.IsNull() && !qos.MaxUp.IsUnknown() {
			desired := qos.MaxUp.ValueInt64()
			if existing.QOSRateMaxUp == nil || *existing.QOSRateMaxUp != desired {
				existing.QOSRateMaxUp = &desired
				update = true
			}
		}
		if !qos.MaxDown.IsNull() && !qos.MaxDown.IsUnknown() {
			desired := qos.MaxDown.ValueInt64()
			if existing.QOSRateMaxDown == nil || *existing.QOSRateMaxDown != desired {
				existing.QOSRateMaxDown = &desired
				update = true
			}
		}
		if update {
			if _, err := client.UpdateClientGroup(ctx, site, &existing); err != nil {
				diags.AddError("Error Updating Client Group",
					fmt.Sprintf("Could not update client group %q: %s", name, err.Error()))
				return "", diags
			}
		}
		return existing.ID, diags
	}

	created := &ui.ClientGroup{Name: name}
	if !qos.MaxUp.IsNull() && !qos.MaxUp.IsUnknown() {
		v := qos.MaxUp.ValueInt64()
		created.QOSRateMaxUp = &v
	}
	if !qos.MaxDown.IsNull() && !qos.MaxDown.IsUnknown() {
		v := qos.MaxDown.ValueInt64()
		created.QOSRateMaxDown = &v
	}
	made, err := client.CreateClientGroup(ctx, site, created)
	if err != nil {
		diags.AddError("Error Creating Client Group",
			fmt.Sprintf("Could not create client group %q: %s", name, err.Error()))
		return "", diags
	}
	return made.ID, diags
}

func clientKitSpec() resourcekit.Spec[clientModel, ui.Client] {
	return resourcekit.Spec[clientModel, ui.Client]{
		TypeName: "client",
		Subject:  "Client",
		New:      func() *ui.Client { return &ui.Client{} },
		ID:       func(m *clientModel) *types.String { return &m.ID },
		Site:     func(m *clientModel) *types.String { return &m.Site },
		Timeouts: func(m *clientModel) *timeouts.Value { return &m.Timeouts },
		// ONE LITERAL BECAUSE AN INSTRUMENT READS IT: the descriptor checks
		// parse this file rather than run it, so a list assembled from a helper
		// call is invisible and every field in it reads as missing.
		Fields: []resourcekit.Field[clientModel, ui.Client]{
			resourcekit.StringLikeField[clientModel, ui.Client, hwtypes.MACAddress]{
				Wire:  "mac",
				Model: func(m *clientModel) *hwtypes.MACAddress { return &m.MAC },
				SDK:   func(s *ui.Client) *string { return &s.MAC },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
			},
			clientStr("name", func(m *clientModel) *types.String { return &m.Name },
				func(s *ui.Client) *string { return &s.Name }, resourcekit.KeepZero),
			clientStr("display_name", func(m *clientModel) *types.String { return &m.DisplayName },
				func(s *ui.Client) *string { return &s.DisplayName }, resourcekit.KeepZero),
			clientStr("note", func(m *clientModel) *types.String { return &m.Note },
				func(s *ui.Client) *string { return &s.Note }, resourcekit.KeepZero),
			// NullZero ON BOTH, MEASURED BEFORE IT WAS WRITTEN: a client with
			// no fixed IP comes back from the controller as "", and
			// iptypes.IPv4Address refuses "" in its own ValidateAttribute --
			// so KeepZero made every read of such a client fail. The MAC twin
			// has the same shape.
			resourcekit.StringLikeField[clientModel, ui.Client, iptypes.IPv4Address]{
				Wire:  "fixed_ip",
				Model: func(m *clientModel) *iptypes.IPv4Address { return &m.FixedIP },
				SDK:   func(s *ui.Client) *string { return &s.FixedIP },
				New: func(v basetypes.StringValue) iptypes.IPv4Address {
					return iptypes.IPv4Address{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringLikeField[clientModel, ui.Client, hwtypes.MACAddress]{
				Wire:  "fixed_ap_mac",
				Model: func(m *clientModel) *hwtypes.MACAddress { return &m.FixedApMAC },
				SDK:   func(s *ui.Client) *string { return &s.FixedApMAC },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},
			// network_id IS virtual_network_override_id, NOT the SDK's NetworkID.
			// unifi.Client carries both and they are different things; the
			// released attribute has always meant the override. Mapping it to
			// NetworkID compiles and passes the elide and wire-name checks --
			// the name is a real json tag on the same struct -- and writes the
			// wrong field. Only the mapping.json comparison catches it.
			clientStr("virtual_network_override_id",
				func(m *clientModel) *types.String { return &m.NetworkID },
				func(s *ui.Client) *string { return &s.VirtualNetworkOverrideID },
				resourcekit.KeepZero),
			clientStr("local_dns_record",
				func(m *clientModel) *types.String { return &m.LocalDNSRecord },
				func(s *ui.Client) *string { return &s.LocalDNSRecord }, resourcekit.KeepZero),
			resourcekit.BoolPtrField[clientModel, ui.Client]{
				Wire:  "blocked",
				Model: func(m *clientModel) *types.Bool { return &m.Blocked },
				SDK:   func(s *ui.Client) **bool { return &s.Blocked },
			},
			resourcekit.StringField[clientModel, ui.Client]{
				Wire:  "hostname",
				Model: func(m *clientModel) *types.String { return &m.Hostname },
				SDK:   func(s *ui.Client) *string { return &s.Hostname },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[clientModel, ui.Client]{
				Wire:  "last_ip",
				Model: func(m *clientModel) *types.String { return &m.LastIP },
				SDK:   func(s *ui.Client) *string { return &s.LastIP },
				Elide: resourcekit.KeepZero,
			},
		},

		Backend: resourcekit.Backend[ui.Client]{
			// Seeded so ToModel does not nil-dereference in a test binary that
			// never calls Configure.
			GetID: func(s *ui.Client) string { return s.ID },
			SetID: func(s *ui.Client, id string) { s.ID = id },
		},

		AfterReceive: clientKitAfterReceive,

		// Every one of these is set by BeforeSend from something that is not a
		// Field, so nothing in the plan can put them in the mask.
		AlwaysWire: []string{
			"usergroup_id", "network_members_group_ids",
			"use_fixedip", "local_dns_record_enabled", "virtual_network_override_enabled",
		},

		// A client is a record on the controller, and forgetting it is the
		// destructive act the schema makes opt-out.
		BeforeDelete: func(_ context.Context, model *clientModel) (bool, diag.Diagnostics) {
			return !model.SkipForgetOnDestroy.ValueBool(), nil
		},
	}
}
