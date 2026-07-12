package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// netflowSection is the settingSection implementation for the "netflow"
// settings section: NetFlow/IPFIX exporter configuration. 11 flat scalar
// leaves (2 always-present bools, 6 nullable ints, 2 strings including one
// closed enum, 1 string list), no nested objects/lists and no secrets.
type netflowSection struct{}

func init() {
	registerSection(netflowSection{})
}

func (netflowSection) key() string      { return "netflow" }
func (netflowSection) attrName() string { return "netflow" }

func (netflowSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "NetFlow/IPFIX exporter settings.",
		Optional:            true,
		Computed:            true,
		Attributes: map[string]schema.Attribute{
			"auto_engine_id_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the NetFlow engine ID is assigned automatically.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether NetFlow export is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"engine_id": schema.Int64Attribute{
				MarkdownDescription: "NetFlow engine ID.",
				Optional:            true,
				Computed:            true,
			},
			"export_frequency": schema.Int64Attribute{
				MarkdownDescription: "Export frequency, in seconds.",
				Optional:            true,
				Computed:            true,
			},
			"network_ids": schema.ListAttribute{
				MarkdownDescription: "IDs of networks whose traffic is exported.",
				ElementType:         types.StringType,
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Destination UDP port for the NetFlow collector.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(1024, 65535),
				},
			},
			"refresh_rate": schema.Int64Attribute{
				MarkdownDescription: "Template refresh rate, in seconds.",
				Optional:            true,
				Computed:            true,
			},
			"sampling_mode": schema.StringAttribute{
				MarkdownDescription: "Sampling mode.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("off", "hash", "random", "deterministic"),
				},
			},
			"sampling_rate": schema.Int64Attribute{
				MarkdownDescription: "Sampling rate (1-in-N packets).",
				Optional:            true,
				Computed:            true,
			},
			"server": schema.StringAttribute{
				MarkdownDescription: "NetFlow collector hostname or IP address.",
				Optional:            true,
				Computed:            true,
			},
			"version": schema.Int64Attribute{
				MarkdownDescription: "NetFlow protocol version.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(5, 9, 10),
				},
			},
		},
	}
}

func (s netflowSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingNetflowModel
	if !prior.Netflow.IsNull() && !prior.Netflow.IsUnknown() {
		diags.Append(prior.Netflow.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	autoEngineIDEnabled, d := decodeBool(data, "auto_engine_id_enabled", priorModel.AutoEngineIDEnabled)
	diags.Append(d...)
	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	engineID, d := decodeInt64(data, "engine_id", priorModel.EngineID)
	diags.Append(d...)
	exportFrequency, d := decodeInt64(data, "export_frequency", priorModel.ExportFrequency)
	diags.Append(d...)
	networkIDs, d := decodeStringList(ctx, data, "network_ids", priorModel.NetworkIDs)
	diags.Append(d...)
	port, d := decodeInt64(data, "port", priorModel.Port)
	diags.Append(d...)
	refreshRate, d := decodeInt64(data, "refresh_rate", priorModel.RefreshRate)
	diags.Append(d...)
	samplingMode, d := decodeString(data, "sampling_mode", priorModel.SamplingMode)
	diags.Append(d...)
	samplingRate, d := decodeInt64(data, "sampling_rate", priorModel.SamplingRate)
	diags.Append(d...)
	server, d := decodeString(data, "server", priorModel.Server)
	diags.Append(d...)
	version, d := decodeInt64(data, "version", priorModel.Version)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingNetflowModel{
		AutoEngineIDEnabled: autoEngineIDEnabled,
		Enabled:             enabled,
		EngineID:            engineID,
		ExportFrequency:     exportFrequency,
		NetworkIDs:          networkIDs,
		Port:                port,
		RefreshRate:         refreshRate,
		SamplingMode:        samplingMode,
		SamplingRate:        samplingRate,
		Server:              server,
		Version:             version,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, netflowAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Netflow = obj
	return diags
}

func (s netflowSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Netflow.IsNull() || model.Netflow.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingNetflowModel
	diags.Append(model.Netflow.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "auto_engine_id_enabled", m.AutoEngineIDEnabled)
	overlayBool(base, "enabled", m.Enabled)
	overlayInt64(base, "engine_id", m.EngineID)
	overlayInt64(base, "export_frequency", m.ExportFrequency)
	diags.Append(overlayStringList(ctx, base, "network_ids", m.NetworkIDs)...)
	overlayInt64(base, "port", m.Port)
	overlayInt64(base, "refresh_rate", m.RefreshRate)
	overlayString(base, "sampling_mode", m.SamplingMode)
	overlayInt64(base, "sampling_rate", m.SamplingRate)
	overlayString(base, "server", m.Server)
	overlayInt64(base, "version", m.Version)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

func (netflowSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Netflow = plan.Netflow
	return nil
}

func (netflowSection) isConfigured(m settingResourceModel) bool {
	return !m.Netflow.IsNull() && !m.Netflow.IsUnknown()
}
