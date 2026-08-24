package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_ap_group"
)

var _ datasource.DataSource = &apGroupDataSource{}

func NewAPGroupDataSource() datasource.DataSource {
	return &apGroupDataSource{}
}

type apGroupDataSource struct {
	dataSourceWithClient
}

type apGroupDataSourceModel struct {
	ID         types.String   `tfsdk:"id"`
	Site       types.String   `tfsdk:"site"`
	Name       types.String   `tfsdk:"name"`
	DeviceMacs types.List     `tfsdk:"device_macs"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func (d *apGroupDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_ap_group"
}

func (d *apGroupDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_ap_group.ApGroupDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *apGroupDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data apGroupDataSourceModel

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

	apGroups, err := d.client.ListAPGroup(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading AP Groups",
			"Could not read AP groups: "+err.Error(),
		)
		return
	}

	var apGroup *unifi.APGroup
	for _, group := range apGroups {
		if group.Name == name {
			apGroup = &group
			break
		}
	}

	if apGroup == nil {
		resp.Diagnostics.AddError(
			"AP Group Not Found",
			fmt.Sprintf("AP group with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(apGroup.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(apGroup.Name)
	deviceMacList := make([]types.String, len(apGroup.DeviceMacs))
	for i, v := range apGroup.DeviceMacs {
		deviceMacList[i] = types.StringValue(v)
	}
	deviceMacs, diags := types.ListValueFrom(ctx, types.StringType, deviceMacList)
	if diags.HasError() {
		resp.Diagnostics.Append(diags...)
		return
	}
	data.DeviceMacs = deviceMacs

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
