package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_client_qos_rate"
)

var _ datasource.DataSource = &clientQosRateDataSource{}

func NewClientQosRateDataSource() datasource.DataSource {
	return &clientQosRateDataSource{}
}

type clientQosRateDataSource struct {
	dataSourceWithClient
}

type clientQosRateDataSourceModel struct {
	ID             types.String   `tfsdk:"id"`
	Site           types.String   `tfsdk:"site"`
	Name           types.String   `tfsdk:"name"`
	QOSRateMaxDown types.Int64    `tfsdk:"qos_rate_max_down"`
	QOSRateMaxUp   types.Int64    `tfsdk:"qos_rate_max_up"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

func (d *clientQosRateDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_client_qos_rate"
}

func (d *clientQosRateDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_client_qos_rate.ClientQosRateDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *clientQosRateDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data clientQosRateDataSourceModel

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

	clientGroups, err := d.client.ListClientGroup(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Client QOS Rates",
			"Could not read client QOS rates: "+err.Error(),
		)
		return
	}

	var clientGroup *unifi.ClientGroup
	for _, group := range clientGroups {
		if group.Name == name {
			clientGroup = &group
			break
		}
	}

	if clientGroup == nil {
		resp.Diagnostics.AddError(
			"Client QOS Rate Not Found",
			fmt.Sprintf("Client group with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(clientGroup.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(clientGroup.Name)
	data.QOSRateMaxDown = types.Int64PointerValue(clientGroup.QOSRateMaxDown)
	data.QOSRateMaxUp = types.Int64PointerValue(clientGroup.QOSRateMaxUp)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
