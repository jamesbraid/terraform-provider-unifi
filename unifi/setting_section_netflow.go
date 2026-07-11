package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingNetflowModel is the nested netflow block: the NetFlow/IPFIX exporter
// configuration (collector, template, and sampling parameters).
type settingNetflowModel struct {
	AutoEngineIDEnabled types.Bool   `tfsdk:"auto_engine_id_enabled"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	EngineID            types.Int64  `tfsdk:"engine_id"`
	ExportFrequency     types.Int64  `tfsdk:"export_frequency"`
	NetworkIDs          types.Set    `tfsdk:"network_ids"`
	Port                types.Int64  `tfsdk:"port"`
	RefreshRate         types.Int64  `tfsdk:"refresh_rate"`
	SamplingMode        types.String `tfsdk:"sampling_mode"`
	SamplingRate        types.Int64  `tfsdk:"sampling_rate"`
	Server              types.String `tfsdk:"server"`
	Version             types.Int64  `tfsdk:"version"`
}

var netflowAttrTypes = map[string]attr.Type{
	"auto_engine_id_enabled": types.BoolType,
	"enabled":                types.BoolType,
	"engine_id":              types.Int64Type,
	"export_frequency":       types.Int64Type,
	"network_ids":            types.SetType{ElemType: types.StringType},
	"port":                   types.Int64Type,
	"refresh_rate":           types.Int64Type,
	"sampling_mode":          types.StringType,
	"sampling_rate":          types.Int64Type,
	"server":                 types.StringType,
	"version":                types.Int64Type,
}

type netflowSection struct{}

func (netflowSection) key() string { return "netflow" }

func (netflowSection) attrTypes() map[string]attr.Type { return netflowAttrTypes }

func (netflowSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "NetFlow/IPFIX exporter settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"auto_engine_id_enabled": schema.BoolAttribute{
				MarkdownDescription: "Derive the exporter engine ID automatically.",
				Optional:            true,
				Computed:            true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable NetFlow export.",
				Optional:            true,
				Computed:            true,
			},
			"engine_id": schema.Int64Attribute{
				MarkdownDescription: "Exporter engine ID (used when `auto_engine_id_enabled` is `false`).",
				Optional:            true,
				Computed:            true,
			},
			"export_frequency": schema.Int64Attribute{
				MarkdownDescription: "Flow export frequency in seconds.",
				Optional:            true,
				Computed:            true,
			},
			"network_ids": schema.SetAttribute{
				MarkdownDescription: "UniFi network IDs whose traffic is exported.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.Set{
					setplanmodifier.UseStateForUnknown(),
				},
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Collector UDP port (1024–65535).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1024, 65535),
				},
			},
			"refresh_rate": schema.Int64Attribute{
				MarkdownDescription: "Template refresh rate in packets.",
				Optional:            true,
				Computed:            true,
			},
			"sampling_mode": schema.StringAttribute{
				MarkdownDescription: "Packet sampling mode: `off`, `hash`, `random`, or `deterministic`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "hash", "random", "deterministic"),
				},
			},
			"sampling_rate": schema.Int64Attribute{
				MarkdownDescription: "Sampling rate (1-in-N packets) when sampling is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"server": schema.StringAttribute{
				MarkdownDescription: "Collector host (IP address or FQDN).",
				Optional:            true,
				Computed:            true,
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "Export format version: `5`, `9`, or `10` (IPFIX).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(5, 9, 10),
				},
			},
		},
	}
}

func (netflowSection) get(m *settingResourceModel) types.Object { return m.Netflow }

func (netflowSection) set(m *settingResourceModel, obj types.Object) { m.Netflow = obj }

func (netflowSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingNetflowModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	netflowModelToData(ctx, &m, data, &diags)
	return diags
}

// netflowModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func netflowModelToData(
	ctx context.Context,
	m *settingNetflowModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	if !m.AutoEngineIDEnabled.IsNull() && !m.AutoEngineIDEnabled.IsUnknown() {
		data["auto_engine_id_enabled"] = m.AutoEngineIDEnabled.ValueBool()
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.EngineID.IsNull() && !m.EngineID.IsUnknown() {
		data["engine_id"] = m.EngineID.ValueInt64()
	}
	if !m.ExportFrequency.IsNull() && !m.ExportFrequency.IsUnknown() {
		data["export_frequency"] = m.ExportFrequency.ValueInt64()
	}
	if !m.NetworkIDs.IsNull() && !m.NetworkIDs.IsUnknown() {
		var ids []string
		diags.Append(m.NetworkIDs.ElementsAs(ctx, &ids, false)...)
		data["network_ids"] = ids
	}
	if !m.Port.IsNull() && !m.Port.IsUnknown() {
		data["port"] = m.Port.ValueInt64()
	}
	if !m.RefreshRate.IsNull() && !m.RefreshRate.IsUnknown() {
		data["refresh_rate"] = m.RefreshRate.ValueInt64()
	}
	if !m.SamplingMode.IsNull() && !m.SamplingMode.IsUnknown() {
		data["sampling_mode"] = m.SamplingMode.ValueString()
	}
	if !m.SamplingRate.IsNull() && !m.SamplingRate.IsUnknown() {
		data["sampling_rate"] = m.SamplingRate.ValueInt64()
	}
	if !m.Server.IsNull() && !m.Server.IsUnknown() {
		data["server"] = m.Server.ValueString()
	}
	if !m.Version.IsNull() && !m.Version.IsUnknown() {
		data["version"] = m.Version.ValueInt64()
	}
}

func (netflowSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Netflow](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(netflowAttrTypes), diags
		}
		diags.AddError("Error Reading NetFlow Setting", err.Error())
		return types.ObjectNull(netflowAttrTypes), diags
	}
	model := netflowSettingToModel(ctx, setting, &diags)
	if diags.HasError() {
		return types.ObjectNull(netflowAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, netflowAttrTypes, model)
}

func netflowSettingToModel(
	ctx context.Context,
	s *settings.Netflow,
	diags *diag.Diagnostics,
) settingNetflowModel {
	ids, d := types.SetValueFrom(ctx, types.StringType, s.NetworkIDs)
	diags.Append(d...)
	return settingNetflowModel{
		AutoEngineIDEnabled: types.BoolValue(s.AutoEngineIDEnabled),
		Enabled:             types.BoolValue(s.Enabled),
		EngineID:            util.ConvertInt64FromAPIValue(s.EngineID),
		ExportFrequency:     util.ConvertInt64FromAPIValue(s.ExportFrequency),
		NetworkIDs:          ids,
		Port:                util.ConvertInt64FromAPIValue(s.Port),
		RefreshRate:         util.ConvertInt64FromAPIValue(s.RefreshRate),
		SamplingMode:        util.StringValueOrNull(s.SamplingMode),
		SamplingRate:        util.ConvertInt64FromAPIValue(s.SamplingRate),
		Server:              util.StringValueOrNull(s.Server),
		Version:             util.ConvertInt64FromAPIValue(s.Version),
	}
}
