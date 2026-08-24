package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_firewall_zone"
)

var _ datasource.DataSource = &firewallZoneDataSource{}

func NewFirewallZoneDataSource() datasource.DataSource {
	return &firewallZoneDataSource{}
}

type firewallZoneDataSource struct {
	dataSourceWithClient
}

type firewallZoneDataSourceModel struct {
	ID         types.String `tfsdk:"id"`
	Site       types.String `tfsdk:"site"`
	Name       types.String `tfsdk:"name"`
	ZoneKey    types.String `tfsdk:"zone_key"`
	NetworkIDs types.List   `tfsdk:"network_ids"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *firewallZoneDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_firewall_zone"
}

func (d *firewallZoneDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_firewall_zone.FirewallZoneDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *firewallZoneDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data firewallZoneDataSourceModel

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

	name := data.Name.ValueString()

	zones, err := d.client.ListFirewallZone(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Firewall Zones",
			"Could not list firewall zones: "+err.Error(),
		)
		return
	}

	var zone *unifi.FirewallZone
	for _, z := range zones {
		if z.Name == name {
			zone = &z
			break
		}
	}

	if zone == nil {
		resp.Diagnostics.AddError(
			"Firewall Zone Not Found",
			fmt.Sprintf("No firewall zone with name %q found on site %q.", name, site),
		)
		return
	}

	data.ID = types.StringValue(zone.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(zone.Name)
	data.ZoneKey = types.StringValue(zone.ZoneKey)

	networkIDs, diags := types.ListValueFrom(ctx, types.StringType, zone.NetworkIDs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.NetworkIDs = networkIDs

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
