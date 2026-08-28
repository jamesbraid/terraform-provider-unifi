package unifi

import (
	"context"
	"fmt"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

type clientKitModel struct {
	ID             types.String       `tfsdk:"id"`
	Site           types.String       `tfsdk:"site"`
	MAC            hwtypes.MACAddress `tfsdk:"mac"`
	Name           types.String       `tfsdk:"name"`
	DisplayName    types.String       `tfsdk:"display_name"`
	QOSRate        types.Object       `tfsdk:"qos_rate"`
	Note           types.String       `tfsdk:"note"`
	FixedIP        types.String       `tfsdk:"fixed_ip"`
	FixedApMAC     hwtypes.MACAddress `tfsdk:"fixed_ap_mac"`
	NetworkID      types.String       `tfsdk:"network_id"`
	Groups         types.List         `tfsdk:"groups"`
	Blocked        types.Bool         `tfsdk:"blocked"`
	LocalDNSRecord types.String       `tfsdk:"local_dns_record"`

	AllowExisting       types.Bool `tfsdk:"allow_existing"`
	SkipForgetOnDestroy types.Bool `tfsdk:"skip_forget_on_destroy"`

	Hostname types.String `tfsdk:"hostname"`
	LastIP   types.String `tfsdk:"last_ip"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

type clientModel = clientKitModel

// clientStr builds a plain string Field. Every client string field elides
// KeepZero -- an empty wire value is a real value here, not "unset" -- so
// that is not a parameter.
func clientStr(
	wire string,
	model func(*clientModel) *types.String,
	sdk func(*ui.Client) *string,
) resourcekit.StringField[clientModel, ui.Client] {
	return resourcekit.StringField[clientModel, ui.Client]{
		Wire: wire, Model: model, SDK: sdk, Elide: resourcekit.KeepZero,
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
		// Delete needs the MAC but Backend.Delete only gets an id; one read
		// answers it, and the object is about to go anyway.
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
// vocabularies, fetched fresh per operation so a renamed group can't go
// stale, with no lock needed since nothing shares it.
type clientGroups struct {
	// usergroups, for qos_rate.
	byID   map[string]ui.ClientGroup
	byName map[string]ui.ClientGroup
	// network-members groups, for the groups attribute.
	memberNameByID map[string]string
	memberIDByName map[string]string
}

// clientKitPrefetch reads both vocabularies and does no writes: Prefetch also
// runs on read, so a side effect here would fire on every refresh. Creating
// the usergroup a qos_rate block asks for is BeforeSend's job.
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

// clientKitBeforeSend derives the six things no Field can carry: usergroup_id
// (may create/re-rate a usergroup), groups (names to ids), and the
// use_fixedip/fixed_ap_enabled/local_dns_record_enabled/virtual_network_override_enabled
// companion booleans.
//
// defaultSite mirrors Resource.Site's own fallback: on a first Create with no
// explicit site, ToModel hasn't run yet, so effective.Site and sdk's site are
// both still empty, and the group creates below need a real one.
//
// groupMu closes the create-on-miss race for both usergroups and
// network-members groups. Each miss path locks it inside its own
// function-scoped helper (clientResolveGroupLocked,
// clientResolveMemberGroupsLocked), so the lock always releases before the
// next branch needs it -- sync.Mutex is non-reentrant, so nothing may hold
// it across two of these helpers in the same call.
func clientKitBeforeSend(
	client *ui.ApiClient,
	defaultSite string,
	groupMu *sync.Mutex,
) func(context.Context, *clientModel, *clientModel, *ui.Client, any) diag.Diagnostics {
	return func(
		ctx context.Context,
		_, effective *clientModel,
		sdk *ui.Client,
		prefetched any,
	) diag.Diagnostics {
		var diags diag.Diagnostics
		groups, _ := prefetched.(*clientGroups)

		site := effective.Site.ValueString()
		if site == "" {
			site = defaultSite
		}

		// The companions. The practitioner sets the value; the flag follows it,
		// and the controller ignores the value without the flag.
		//
		// ValueString() returns "" for both null and unknown (neither
		// NewStringNull nor NewStringUnknown ever sets the backing value), so
		// comparing it to "" already covers those states; a separate IsNull()
		// check would be redundant.
		sdk.UseFixedIP = effective.FixedIP.ValueString() != ""
		sdk.FixedApEnabled = effective.FixedApMAC.ValueString() != ""
		sdk.LocalDNSRecordEnabled = effective.LocalDNSRecord.ValueString() != ""
		// virtual_network_override_enabled is in AlwaysWire, so this must never
		// be nil: a nil *bool serializes as JSON null, which the controller
		// rejects (api.err.InvalidValue) rather than treating as clearing the flag.
		sdk.VirtualNetworkOverrideEnabled = util.Ptr(effective.NetworkID.ValueString() != "")

		if !effective.QOSRate.IsNull() && !effective.QOSRate.IsUnknown() {
			var qos qosRateModel
			diags.Append(effective.QOSRate.As(ctx, &qos, basetypes.ObjectAsOptions{})...)
			if diags.HasError() {
				return diags
			}
			id, d := clientResolveGroup(ctx, client, site, groupMu, groups, qos)
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
			// misses only holds what this operation's own Prefetch didn't
			// already resolve, so the common case (every name already in
			// groups.memberIDByName) never locks at all.
			var misses []string
			for _, name := range names {
				if _, ok := groups.memberIDByName[name]; !ok {
					misses = append(misses, name)
				}
			}
			if len(misses) > 0 {
				resolved, d := clientResolveMemberGroupsLocked(ctx, client, site, groupMu, misses)
				diags.Append(d...)
				if diags.HasError() {
					return diags
				}
				for name, id := range resolved {
					groups.memberIDByName[name] = id
				}
			}
			ids := make([]string, 0, len(names))
			for _, name := range names {
				ids = append(ids, groups.memberIDByName[name])
			}
			sdk.NetworkMembersGroupIDs = ids
		}
		return diags
	}
}

// clientResolveMemberGroupsLocked is the network-members-group miss path:
// locked for exactly this call, it re-lists to see whatever the lock's
// previous holder just created, then creates whatever of misses is still
// absent. Function-scoped so the lock releases the moment this returns
// rather than spanning the rest of BeforeSend.
func clientResolveMemberGroupsLocked(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	groupMu *sync.Mutex,
	misses []string,
) (map[string]string, diag.Diagnostics) {
	groupMu.Lock()
	defer groupMu.Unlock()

	var diags diag.Diagnostics
	list, err := client.ListNetworkMembersGroups(ctx, site)
	if err != nil {
		diags.AddError("Error Listing Network Members Groups",
			fmt.Sprintf("Could not list network members groups: %s", err.Error()))
		return nil, diags
	}
	fresh := make(map[string]string, len(list))
	for _, g := range list {
		fresh[g.Name] = g.ID
	}

	resolved := make(map[string]string, len(misses))
	for _, name := range misses {
		if id, ok := fresh[name]; ok {
			resolved[name] = id
			continue
		}
		// A name with no network-members group behind it gets one created
		// rather than erroring; fresh is updated too so a second colliding
		// name in this same call reuses it instead of creating a duplicate.
		created, err := client.CreateNetworkMembersGroup(ctx, site,
			&ui.NetworkMembersGroup{Name: name, Members: []string{}, Type: "CLIENTS"})
		if err != nil {
			diags.AddError("Error Creating Network Members Group",
				fmt.Sprintf("Could not create network members group %q: %s", name, err.Error()))
			continue
		}
		fresh[name] = created.ID
		resolved[name] = created.ID
	}
	return resolved, diags
}

// clientKitAfterReceive fills the two attributes read back from ids, and
// corrects two more that ToModel read too literally. It does no IO: the
// vocabularies Prefetch already holds answer it.
func clientKitAfterReceive(
	_ context.Context, sdk *ui.Client, model *clientModel, _ clientModel, prefetched any,
) diag.Diagnostics {
	var diags diag.Diagnostics

	// The controller keeps echoing the last fixed IP/DNS record after its
	// enable flag turns off, so the plain read would surface a stale value
	// forever and a practitioner's fixed_ip = "" could never round-trip.
	// Mirror the disabled state as a known empty string instead.
	if !sdk.UseFixedIP {
		model.FixedIP = types.StringValue("")
	}
	if !sdk.LocalDNSRecordEnabled {
		model.LocalDNSRecord = types.StringValue("")
	}

	// allow_existing and skip_forget_on_destroy have no Field -- the controller
	// never hears of them -- so ToModel never touches them; schema Default()
	// covers Create, but ImportState seeds only id/site, leaving both null
	// through the first Read and manufacturing a spurious diff on import.
	// Default them here for that path.
	if model.AllowExisting.IsNull() || model.AllowExisting.IsUnknown() {
		model.AllowExisting = types.BoolValue(true)
	}
	if model.SkipForgetOnDestroy.IsNull() || model.SkipForgetOnDestroy.IsUnknown() {
		model.SkipForgetOnDestroy = types.BoolValue(false)
	}

	// blocked reads back false, not null, when the controller omits it -- the
	// schema defaults it to false, so null here is an inconsistent-result
	// error. BoolPtrField can't express this (a nil pointer is always null),
	// so the default lives here.
	if sdk.Blocked == nil {
		model.Blocked = types.BoolValue(false)
	}

	// A zero types.Object is null but untyped, which doesn't fit the schema,
	// so the no-prefetch path sets these explicitly rather than leaving them.
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
// re-rating the group when it has to. With neither id nor name given, the
// group is named after the rates themselves, so two clients asking for the
// same limits share one group instead of accumulating duplicates.
func clientResolveGroup(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	groupMu *sync.Mutex,
	groups *clientGroups,
	qos qosRateModel,
) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	// ValueString() returns "" for both null and unknown, so comparing it to
	// "" already covers those states without a separate IsNull()/IsUnknown()
	// check.
	if qos.ID.ValueString() != "" {
		return qos.ID.ValueString(), diags
	}

	name := qos.Name.ValueString()
	if name == "" {
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
		return clientUpdateGroupIfNeeded(ctx, client, site, existing, qos)
	}

	return clientResolveGroupLocked(ctx, client, site, groupMu, name, qos)
}

// clientResolveGroupLocked is clientResolveGroup's miss path: locked for
// exactly this call, it re-lists to see whatever the lock's previous holder
// just created, and only creates when the miss survives the re-list.
// Function-scoped so the lock releases the moment this returns rather than
// spanning the rest of BeforeSend.
func clientResolveGroupLocked(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	groupMu *sync.Mutex,
	name string,
	qos qosRateModel,
) (string, diag.Diagnostics) {
	groupMu.Lock()
	defer groupMu.Unlock()

	var diags diag.Diagnostics
	list, err := client.ListClientGroup(ctx, site)
	if err != nil {
		diags.AddError("Error Listing Client Groups",
			fmt.Sprintf("Could not list client groups: %s", err.Error()))
		return "", diags
	}
	for _, existing := range list {
		if existing.Name == name {
			return clientUpdateGroupIfNeeded(ctx, client, site, existing, qos)
		}
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

// clientUpdateGroupIfNeeded re-rates an existing usergroup when qos_rate's
// max_up/max_down disagree with what the controller already has, and
// returns its id unchanged otherwise.
func clientUpdateGroupIfNeeded(
	ctx context.Context,
	client *ui.ApiClient,
	site string,
	existing ui.ClientGroup,
	qos qosRateModel,
) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

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
				fmt.Sprintf("Could not update client group %q: %s", existing.Name, err.Error()))
			return "", diags
		}
	}
	return existing.ID, diags
}

func clientKitSpec() resourcekit.Spec[clientModel, ui.Client] {
	return resourcekit.Spec[clientModel, ui.Client]{
		TypeName: "client",
		Subject:  "Client",
		New:      func() *ui.Client { return &ui.Client{} },
		ID:       func(m *clientModel) *types.String { return &m.ID },
		Site:     func(m *clientModel) *types.String { return &m.Site },
		Timeouts: func(m *clientModel) *timeouts.Value { return &m.Timeouts },
		// Fields is one literal because an instrument parses this file rather
		// than running it; a list built via a helper would be invisible to it.
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
				func(s *ui.Client) *string { return &s.Name }),
			clientStr("display_name", func(m *clientModel) *types.String { return &m.DisplayName },
				func(s *ui.Client) *string { return &s.DisplayName }),
			clientStr("note", func(m *clientModel) *types.String { return &m.Note },
				func(s *ui.Client) *string { return &s.Note }),
			// A plain string, not iptypes.IPv4Address: that type's validator
			// rejects "", which is the documented way to clear a previously
			// assigned fixed IP. clientKitAfterReceive, not this Field, decides
			// what an empty wire value means.
			clientStr("fixed_ip", func(m *clientModel) *types.String { return &m.FixedIP },
				func(s *ui.Client) *string { return &s.FixedIP }),
			resourcekit.StringLikeField[clientModel, ui.Client, hwtypes.MACAddress]{
				Wire:  "fixed_ap_mac",
				Model: func(m *clientModel) *hwtypes.MACAddress { return &m.FixedApMAC },
				SDK:   func(s *ui.Client) *string { return &s.FixedApMAC },
				New: func(v basetypes.StringValue) hwtypes.MACAddress {
					return hwtypes.MACAddress{StringValue: v}
				},
				Elide: resourcekit.NullZero,
			},
			// network_id maps to virtual_network_override_id, not the SDK's
			// separate NetworkID field -- mapping to NetworkID would compile and
			// pass the elide/wire-name checks, but write the wrong field. Only
			// the mapping.json comparison catches it.
			clientStr("virtual_network_override_id",
				func(m *clientModel) *types.String { return &m.NetworkID },
				func(s *ui.Client) *string { return &s.VirtualNetworkOverrideID }),
			clientStr("local_dns_record",
				func(m *clientModel) *types.String { return &m.LocalDNSRecord },
				func(s *ui.Client) *string { return &s.LocalDNSRecord }),
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
			"use_fixedip", "fixed_ap_enabled",
			"local_dns_record_enabled", "virtual_network_override_enabled",
		},

		// A client is a record on the controller, and forgetting it is the
		// destructive act the schema makes opt-out.
		BeforeDelete: func(_ context.Context, model *clientModel) (bool, diag.Diagnostics) {
			return !model.SkipForgetOnDestroy.ValueBool(), nil
		},
	}
}
