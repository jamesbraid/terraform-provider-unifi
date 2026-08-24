package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_radius_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

var _ datasource.DataSource = &radiusProfileDataSource{}

func NewRadiusProfileDataSource() datasource.DataSource {
	return &radiusProfileDataSource{}
}

type radiusProfileDataSource struct {
	dataSourceWithClient
}

type radiusProfileDataSourceModel struct {
	ID                    types.String         `tfsdk:"id"`
	Site                  types.String         `tfsdk:"site"`
	Name                  types.String         `tfsdk:"name"`
	AccountingEnabled     types.Bool           `tfsdk:"accounting_enabled"`
	InterimUpdateEnabled  types.Bool           `tfsdk:"interim_update_enabled"`
	InterimUpdateInterval timetypes.GoDuration `tfsdk:"interim_update_interval"`
	UseUSGAcctServer      types.Bool           `tfsdk:"use_usg_acct_server"`
	UseUSGAuthServer      types.Bool           `tfsdk:"use_usg_auth_server"`
	VlanEnabled           types.Bool           `tfsdk:"vlan_enabled"`
	VlanWlanMode          types.String         `tfsdk:"vlan_wlan_mode"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *radiusProfileDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_radius_profile"
}

func (d *radiusProfileDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_radius_profile.RadiusProfileDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *radiusProfileDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data radiusProfileDataSourceModel

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

	radiusProfiles, err := d.client.ListRADIUSProfile(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading RADIUS Profiles",
			"Could not read RADIUS profiles: "+err.Error(),
		)
		return
	}

	var radiusProfile *unifi.RADIUSProfile
	for _, profile := range radiusProfiles {
		if profile.Name == name {
			radiusProfile = &profile
			break
		}
	}

	if radiusProfile == nil {
		resp.Diagnostics.AddError(
			"RADIUS Profile Not Found",
			fmt.Sprintf("RADIUS profile with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(radiusProfile.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(radiusProfile.Name)
	data.AccountingEnabled = types.BoolValue(radiusProfile.AccountingEnabled)
	data.InterimUpdateEnabled = types.BoolValue(radiusProfile.InterimUpdateEnabled)
	data.InterimUpdateInterval = util.DurationPtrValue(
		radiusProfile.InterimUpdateInterval,
		time.Second,
	)
	data.UseUSGAcctServer = types.BoolValue(radiusProfile.UseUsgAcctServer)
	data.UseUSGAuthServer = types.BoolValue(radiusProfile.UseUsgAuthServer)
	data.VlanEnabled = types.BoolValue(radiusProfile.VLANEnabled)
	data.VlanWlanMode = types.StringValue(radiusProfile.VLANWLANMode)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
