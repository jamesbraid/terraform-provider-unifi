package unifi

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/datasource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/datasource_port_profile"
)

var _ datasource.DataSource = &portProfileDataSource{}

func NewPortProfileDataSource() datasource.DataSource {
	return &portProfileDataSource{}
}

type portProfileDataSource struct {
	dataSourceWithClient
}

type portProfileDataSourceModel struct {
	ID                     types.String `tfsdk:"id"`
	Site                   types.String `tfsdk:"site"`
	Name                   types.String `tfsdk:"name"`
	Forward                types.String `tfsdk:"forward"`
	NativeNetworkconfID    types.String `tfsdk:"native_networkconf_id"`
	TaggedNetworkconfIDs   types.Set    `tfsdk:"tagged_networkconf_ids"`
	ExcludedNetworkconfIDs types.Set    `tfsdk:"excluded_networkconf_ids"`
	TaggedVLANMgmt         types.String `tfsdk:"tagged_vlan_mgmt"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func (d *portProfileDataSource) Metadata(
	ctx context.Context,
	req datasource.MetadataRequest,
	resp *datasource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_port_profile"
}

func (d *portProfileDataSource) Schema(
	ctx context.Context,
	req datasource.SchemaRequest,
	resp *datasource.SchemaResponse,
) {
	resp.Schema = datasource_port_profile.PortProfileDsDataSourceSchema(ctx)
	resp.Schema.Attributes["timeouts"] = timeouts.Attributes(ctx)
}

func (d *portProfileDataSource) Read(
	ctx context.Context,
	req datasource.ReadRequest,
	resp *datasource.ReadResponse,
) {
	var data portProfileDataSourceModel

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

	portProfiles, err := d.client.ListPortProfile(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Port Profiles",
			"Could not read port profiles: "+err.Error(),
		)
		return
	}

	var portProfile *unifi.PortProfile
	for _, profile := range portProfiles {
		if profile.Name == name {
			portProfile = &profile
			break
		}
	}

	if portProfile == nil {
		resp.Diagnostics.AddError(
			"Port Profile Not Found",
			fmt.Sprintf("Port profile with name %s not found", name),
		)
		return
	}

	data.ID = types.StringValue(portProfile.ID)
	data.Site = types.StringValue(site)
	data.Name = types.StringValue(portProfile.Name)

	if portProfile.Forward == "" {
		data.Forward = types.StringValue("native")
	} else {
		data.Forward = types.StringValue(portProfile.Forward)
	}

	if portProfile.NATiveNetworkID != "" {
		data.NativeNetworkconfID = types.StringValue(portProfile.NATiveNetworkID)
	} else {
		data.NativeNetworkconfID = types.StringNull()
	}

	networks, err := d.client.ListNetwork(ctx, site)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Networks for Port Profile",
			"Could not read the site network inventory: "+err.Error(),
		)
		return
	}
	universe := portProfileTaggedNetworkUniverse(networks, portProfile.NATiveNetworkID)
	tagged := portProfileActualTaggedNetworkIDs(
		portProfile.TaggedVLANMgmt,
		universe,
		portProfile.ExcludedNetworkIDs,
	)
	if tagged == nil {
		data.TaggedNetworkconfIDs = types.SetNull(types.StringType)
	} else {
		value, d := types.SetValueFrom(ctx, types.StringType, tagged)
		resp.Diagnostics.Append(d...)
		data.TaggedNetworkconfIDs = value
	}
	if portProfile.TaggedVLANMgmt == "custom" {
		excluded := portProfile.ExcludedNetworkIDs
		if excluded == nil {
			excluded = []string{}
		}
		value, d := types.SetValueFrom(
			ctx,
			types.StringType,
			excluded,
		)
		resp.Diagnostics.Append(d...)
		data.ExcludedNetworkconfIDs = value
	} else {
		data.ExcludedNetworkconfIDs = types.SetNull(types.StringType)
	}
	if portProfile.TaggedVLANMgmt == "" {
		data.TaggedVLANMgmt = types.StringNull()
	} else {
		data.TaggedVLANMgmt = types.StringValue(portProfile.TaggedVLANMgmt)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
