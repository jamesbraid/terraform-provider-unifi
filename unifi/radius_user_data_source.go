package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_radius_user"
)

var _ datasource.DataSource = &radiusUserDataSource{}

func NewRadiusUserDataSource() datasource.DataSource {
	return &radiusUserDataSource{}
}

type radiusUserDataSource struct {
	dataSourceWithClient
}

type radiusUserDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Site             types.String `tfsdk:"site"`
	Name             types.String `tfsdk:"name"`
	Password         types.String `tfsdk:"password"`
	TunnelType       types.Int64  `tfsdk:"tunnel_type"`
	TunnelMediumType types.Int64  `tfsdk:"tunnel_medium_type"`
	NetworkID        types.String `tfsdk:"network_id"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *radiusUserDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_radius_user"
}

func (d *radiusUserDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_radius_user.RadiusUserDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *radiusUserDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data radiusUserDataSourceModel

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

	accounts, err := d.client.ListAccount(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Radius Users",
			"Could not read radius users: "+err.Error(),
		)
		return
	}

	var account *unifi.Account
	for _, a := range accounts {
		if a.Name == name {
			account = &a
			break
		}
	}

	if account == nil {
		resp.Diagnostics.AddError(
			"Radius User Not Found",
			fmt.Sprintf("Radius user with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(account.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(account.Name)
	data.Password = types.StringValue(account.Password)
	data.TunnelType = types.Int64PointerValue(account.TunnelType)
	data.TunnelMediumType = types.Int64PointerValue(account.TunnelMediumType)
	data.NetworkID = types.StringValue(account.NetworkID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
