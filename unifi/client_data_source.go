package unifi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-nettypes/iptypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_client"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ datasource.DataSource = &clientDataSource{}

func NewClientDataSource() datasource.DataSource {
	return &clientDataSource{}
}

// clientDataSource defines the data source implementation.
type clientDataSource struct {
	dataSourceWithClient
}

// clientDataSourceModel describes the data source data model.
type clientDataSourceModel struct {
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
	Hostname       types.String        `tfsdk:"hostname"`
	Timeouts       timeouts.Value      `tfsdk:"timeouts"`
}

func (d *clientDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client"
}

func (d *clientDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_client.ClientDsDataSourceSchema(ctx)
	// Grafted rather than generated, as everywhere else: timeouts.Attributes
	// is a call, not a literal, so the code specification cannot carry it.
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *clientDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var config clientDataSourceModel

	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	readTimeout, timeoutDiags := config.Timeouts.Read(ctx, 20*time.Minute)
	resp.Diagnostics.Append(timeoutDiags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	site := config.Site.ValueString()
	if site == "" {
		site = d.client.Site
	}

	mac := config.MAC.ValueString()

	macResp, err := d.client.GetClientByMAC(ctx, site, strings.ToLower(mac))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Client by MAC",
			"Could not read client with MAC "+mac+": "+err.Error(),
		)
		return
	}

	client, err := d.client.GetClient(ctx, site, macResp.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Client",
			"Could not read client with ID "+macResp.ID+": "+err.Error(),
		)
		return
	}

	// For some reason the IP address is only on the MAC endpoint
	client.LastSeen = macResp.LastSeen

	var state clientDataSourceModel

	state.Timeouts = config.Timeouts

	state.ID = types.StringValue(client.ID)
	state.Site = types.StringValue(site)
	state.MAC = util.MACValueOrNull(client.MAC)

	if client.Name != "" {
		state.Name = types.StringValue(client.Name)
	} else {
		state.Name = types.StringNull()
	}

	if client.DisplayName != "" {
		state.DisplayName = types.StringValue(client.DisplayName)
	} else {
		state.DisplayName = types.StringNull()
	}

	if client.UserGroupID != "" {
		group, err := d.client.GetClientGroup(ctx, site, client.UserGroupID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading Client Group",
				fmt.Sprintf("Could not read client group %q: %s", client.UserGroupID, err.Error()),
			)
			return
		}
		qos := qosRateModel{
			ID:      types.StringValue(group.ID),
			Name:    types.StringValue(group.Name),
			MaxUp:   types.Int64PointerValue(group.QOSRateMaxUp),
			MaxDown: types.Int64PointerValue(group.QOSRateMaxDown),
		}
		var objDiags diag.Diagnostics
		state.QOSRate, objDiags = types.ObjectValueFrom(ctx, qosRateModel{}.AttributeTypes(), qos)
		resp.Diagnostics.Append(objDiags...)
	} else {
		state.QOSRate = types.ObjectNull(qosRateModel{}.AttributeTypes())
	}

	if client.Note != "" {
		state.Note = types.StringValue(client.Note)
	} else {
		state.Note = types.StringNull()
	}

	state.FixedIP = util.IPv4ValueOrNull(client.FixedIP)

	state.FixedApMAC = util.MACValueOrNull(client.FixedApMAC)

	state.NetworkID = networkIDValue(client)

	if len(client.NetworkMembersGroupIDs) > 0 {
		groups, err := d.client.ListNetworkMembersGroups(ctx, site)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Listing Groups",
				"Could not list network members groups: "+err.Error(),
			)
			return
		}
		idToName := make(map[string]string, len(groups))
		for _, g := range groups {
			idToName[g.ID] = g.Name
		}
		elements := make([]attr.Value, 0, len(client.NetworkMembersGroupIDs))
		for _, id := range client.NetworkMembersGroupIDs {
			if name, ok := idToName[id]; ok {
				elements = append(elements, types.StringValue(name))
			}
		}
		if len(elements) > 0 {
			var listDiags diag.Diagnostics
			state.Groups, listDiags = types.ListValue(types.StringType, elements)
			resp.Diagnostics.Append(listDiags...)
		} else {
			state.Groups = types.ListNull(types.StringType)
		}
	} else {
		state.Groups = types.ListNull(types.StringType)
	}

	state.Blocked = types.BoolPointerValue(client.Blocked)

	if client.LocalDNSRecord != "" {
		state.LocalDNSRecord = types.StringValue(client.LocalDNSRecord)
	} else {
		state.LocalDNSRecord = types.StringNull()
	}

	if client.Hostname != "" {
		state.Hostname = types.StringValue(client.Hostname)
	} else {
		state.Hostname = types.StringNull()
	}

	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}
