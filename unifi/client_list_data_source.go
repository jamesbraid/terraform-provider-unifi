package unifi

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	gounifi "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_client_list"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

var _ datasource.DataSource = &clientListDataSource{}

func NewClientListDataSource() datasource.DataSource {
	return &clientListDataSource{}
}

type clientListDataSource struct {
	dataSourceWithClient

	// Cache group name → ID lookups per site to avoid repeated API calls.
	groupCacheMu sync.Mutex
	groupCache   map[string]map[string]string // site → (name → id)
}

type clientListDataSourceModel struct {
	Site     types.String   `tfsdk:"site"`
	Group    types.String   `tfsdk:"group"`
	Wired    types.Bool     `tfsdk:"wired"`
	Blocked  types.Bool     `tfsdk:"blocked"`
	OUI      types.String   `tfsdk:"oui"`
	Clients  types.List     `tfsdk:"clients"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func clientListEntryAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		// From Client (user REST API)
		"id":                        types.StringType,
		"mac":                       types.StringType,
		"name":                      types.StringType,
		"display_name":              types.StringType,
		"group_id":                  types.StringType,
		"note":                      types.StringType,
		"fixed_ip":                  types.StringType,
		"fixed_ap_mac":              types.StringType,
		"network_id":                types.StringType,
		"network_members_group_ids": types.ListType{ElemType: types.StringType},
		"blocked":                   types.BoolType,
		"local_dns_record":          types.StringType,
		"hostname":                  types.StringType,

		// From ClientInfo (active/history enrichment)
		"ip":                           types.StringType,
		"status":                       types.StringType,
		"uptime":                       types.Int64Type,
		"first_seen":                   types.Int64Type,
		"last_seen":                    types.Int64Type,
		"is_wired":                     types.BoolType,
		"is_guest":                     types.BoolType,
		"authorized":                   types.BoolType,
		"oui":                          types.StringType,
		"ap_mac":                       types.StringType,
		"channel":                      types.Int64Type,
		"radio":                        types.StringType,
		"radio_name":                   types.StringType,
		"essid":                        types.StringType,
		"bssid":                        types.StringType,
		"signal":                       types.Int64Type,
		"rssi":                         types.Int64Type,
		"noise":                        types.Int64Type,
		"tx_rate":                      types.Int64Type,
		"rx_rate":                      types.Int64Type,
		"tx_bytes":                     types.Int64Type,
		"rx_bytes":                     types.Int64Type,
		"wired_rate_mbps":              types.Int64Type,
		"sw_port":                      types.Int64Type,
		"last_uplink_mac":              types.StringType,
		"last_uplink_name":             types.StringType,
		"network_name":                 types.StringType,
		"last_connection_network_id":   types.StringType,
		"last_connection_network_name": types.StringType,
	}
}

func (d *clientListDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client_list"
}

func (d *clientListDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_client_list.ClientListDsDataSourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

// resolveGroupID looks up a network members group by name and returns its ID.
// Results are cached per site to avoid repeated API calls.
func (d *clientListDataSource) resolveGroupID(
	ctx context.Context,
	site, groupName string,
) (string, error) {
	d.groupCacheMu.Lock()
	defer d.groupCacheMu.Unlock()

	if d.groupCache == nil {
		d.groupCache = make(map[string]map[string]string)
	}

	if siteCache, ok := d.groupCache[site]; ok {
		if id, ok := siteCache[groupName]; ok {
			return id, nil
		}
	}

	groups, err := d.client.ListNetworkMembersGroups(ctx, site)
	if err != nil {
		return "", fmt.Errorf("listing network members groups: %w", err)
	}

	siteCache := make(map[string]string, len(groups))
	for _, g := range groups {
		siteCache[g.Name] = g.ID
	}
	d.groupCache[site] = siteCache

	id, ok := siteCache[groupName]
	if !ok {
		return "", fmt.Errorf("network members group %q not found", groupName)
	}
	return id, nil
}

func (d *clientListDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data clientListDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := data.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := data.Site.ValueString()
	if site == "" {
		site = d.client.Site
	}

	filters := make(map[string]string)

	if !data.Group.IsNull() {
		groupID, err := d.resolveGroupID(ctx, site, data.Group.ValueString())
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Resolving Group",
				err.Error(),
			)
			return
		}
		filters["network_members_group_ids"] = groupID
	}

	if !data.Wired.IsNull() {
		val := "false"
		if data.Wired.ValueBool() {
			val = "true"
		}
		filters["is_wired"] = val
	}

	if !data.Blocked.IsNull() {
		val := "false"
		if data.Blocked.ValueBool() {
			val = "true"
		}
		filters["blocked"] = val
	}

	if !data.OUI.IsNull() {
		filters["oui"] = data.OUI.ValueString()
	}

	clients, err := d.client.ListClientFiltered(ctx, site, filters)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Clients",
			"Could not read clients: "+err.Error(),
		)
		return
	}

	// Fetch active client info and history for enrichment.
	// Build lookup maps keyed by user_id (which maps to Client.ID).
	infoByUserID := make(map[string]*gounifi.ClientInfo)

	activeClients, err := d.client.ListClientInfo(ctx, site)
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Unable to Fetch Active Client Info",
			"Client info enrichment will be skipped: "+err.Error(),
		)
	} else {
		for i := range activeClients {
			ci := &activeClients[i]
			if ci.UserId != "" {
				infoByUserID[ci.UserId] = ci
			}
		}
	}

	// Also fetch history to cover offline clients.
	historyClients, err := d.client.ListClientHistory(ctx, site, 0)
	if err != nil {
		resp.Diagnostics.AddWarning(
			"Unable to Fetch Client History",
			"Historical client enrichment will be skipped: "+err.Error(),
		)
	} else {
		for i := range historyClients {
			ci := &historyClients[i]
			// Active data takes precedence — only add if not already present.
			if ci.UserId != "" {
				if _, exists := infoByUserID[ci.UserId]; !exists {
					infoByUserID[ci.UserId] = ci
				}
			}
		}
	}

	clientObjects := make([]basetypes.ObjectValue, 0, len(clients))
	for _, c := range clients {
		info := infoByUserID[c.ID]
		v := clientListEntryValues(&c, info)

		o, diags := types.ObjectValue(clientListEntryAttrTypes(), v)
		if resp.Diagnostics.Append(diags...); resp.Diagnostics.HasError() {
			return
		}
		clientObjects = append(clientObjects, o)
	}

	clist, diags := types.ListValueFrom(
		ctx,
		types.ObjectType{AttrTypes: clientListEntryAttrTypes()},
		clientObjects,
	)
	if resp.Diagnostics.Append(diags...); resp.Diagnostics.HasError() {
		return
	}
	data.Clients = clist
	data.Site = types.StringValue(site)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// clientListEntryValues builds the attribute value map for a single client entry,
// merging Client data with optional ClientInfo enrichment.
func clientListEntryValues(c *gounifi.Client, info *gounifi.ClientInfo) map[string]attr.Value {
	v := map[string]attr.Value{
		// Client fields
		"id":           types.StringValue(c.ID),
		"mac":          types.StringValue(c.MAC),
		"name":         util.StringValueOrNull(c.Name),
		"display_name": util.StringValueOrNull(c.DisplayName),
		"group_id":     util.StringValueOrNull(c.UserGroupID),
		"note":         util.StringValueOrNull(c.Note),
		"fixed_ip":     util.StringValueOrNull(c.FixedIP),
		"fixed_ap_mac": util.StringValueOrNull(c.FixedApMAC),
		"hostname":     util.StringValueOrNull(c.Hostname),
		"blocked":      types.BoolPointerValue(c.Blocked),

		// Network ID: prefer virtual network override
		"network_id":       networkIDValue(c),
		"local_dns_record": util.StringValueOrNull(c.LocalDNSRecord),
	}

	v["network_members_group_ids"] = stringSliceToList(c.NetworkMembersGroupIDs)

	// ClientInfo enrichment fields — null when no info available
	if info != nil {
		v["ip"] = util.StringValueOrNull(info.IP)
		v["status"] = util.StringValueOrNull(info.Status)
		v["uptime"] = int64PointerValueOrNull(info.Uptime)
		v["first_seen"] = int64PointerValueOrNull(info.FirstSeen)
		v["last_seen"] = int64PointerValueOrNull(info.LastSeen)
		v["is_wired"] = types.BoolValue(info.IsWired)
		v["is_guest"] = types.BoolValue(info.IsGuest)
		v["authorized"] = types.BoolValue(info.Authorized)
		v["oui"] = util.StringValueOrNull(info.Oui)
		v["ap_mac"] = util.StringValueOrNull(info.ApMac)
		v["channel"] = int64PointerValueOrNull(info.Channel)
		v["radio"] = util.StringValueOrNull(info.Radio)
		v["radio_name"] = util.StringValueOrNull(info.RadioName)
		v["essid"] = util.StringValueOrNull(info.Essid)
		v["bssid"] = util.StringValueOrNull(info.Bssid)
		v["signal"] = int64PointerValueOrNull(info.Signal)
		v["rssi"] = int64PointerValueOrNull(info.Rssi)
		v["noise"] = int64PointerValueOrNull(info.Noise)
		v["tx_rate"] = int64PointerValueOrNull(info.TxRate)
		v["rx_rate"] = int64PointerValueOrNull(info.RxRate)
		v["tx_bytes"] = int64PointerValueOrNull(info.TxBytes)
		v["rx_bytes"] = int64PointerValueOrNull(info.RxBytes)
		v["wired_rate_mbps"] = int64PointerValueOrNull(info.WiredRateMbps)
		v["sw_port"] = int64PointerValueOrNull(info.SwPort)
		v["last_uplink_mac"] = util.StringValueOrNull(info.LastUplinkMac)
		v["last_uplink_name"] = util.StringValueOrNull(info.LastUplinkName)
		v["network_name"] = util.StringValueOrNull(info.NetworkName)
		v["last_connection_network_id"] = util.StringValueOrNull(info.LastConnectionNetworkId)
		v["last_connection_network_name"] = util.StringValueOrNull(info.LastConnectionNetworkName)
	} else {
		v["ip"] = types.StringNull()
		v["status"] = types.StringNull()
		v["uptime"] = types.Int64Null()
		v["first_seen"] = types.Int64Null()
		v["last_seen"] = types.Int64Null()
		v["is_wired"] = types.BoolNull()
		v["is_guest"] = types.BoolNull()
		v["authorized"] = types.BoolNull()
		v["oui"] = types.StringNull()
		v["ap_mac"] = types.StringNull()
		v["channel"] = types.Int64Null()
		v["radio"] = types.StringNull()
		v["radio_name"] = types.StringNull()
		v["essid"] = types.StringNull()
		v["bssid"] = types.StringNull()
		v["signal"] = types.Int64Null()
		v["rssi"] = types.Int64Null()
		v["noise"] = types.Int64Null()
		v["tx_rate"] = types.Int64Null()
		v["rx_rate"] = types.Int64Null()
		v["tx_bytes"] = types.Int64Null()
		v["rx_bytes"] = types.Int64Null()
		v["wired_rate_mbps"] = types.Int64Null()
		v["sw_port"] = types.Int64Null()
		v["last_uplink_mac"] = types.StringNull()
		v["last_uplink_name"] = types.StringNull()
		v["network_name"] = types.StringNull()
		v["last_connection_network_id"] = types.StringNull()
		v["last_connection_network_name"] = types.StringNull()
	}

	return v
}

func networkIDValue(c *gounifi.Client) types.String {
	if c.VirtualNetworkOverrideID != "" {
		return types.StringValue(c.VirtualNetworkOverrideID)
	}
	return util.StringValueOrNull(c.NetworkID)
}

func stringSliceToList(s []string) basetypes.ListValue {
	if len(s) == 0 {
		return types.ListNull(types.StringType)
	}
	elements := make([]attr.Value, len(s))
	for i, v := range s {
		elements[i] = types.StringValue(v)
	}
	list, diags := types.ListValue(types.StringType, elements)
	if diags.HasError() {
		return types.ListNull(types.StringType)
	}
	return list
}

func int64PointerValueOrNull(v *int64) types.Int64 {
	if v == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*v)
}
