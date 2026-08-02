package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// syslogSection is the settingSection implementation for the "syslog"
// settings section. It is the list-shape worked template (Task 16): the
// scalar template (auto_speedtest) plus one List<String> leaf (contents).
//
// It is also the first section where the controller storage key diverges
// from the Terraform attribute name: the controller stores this section
// under "rsyslogd" while the attribute is "syslog". key() (snapshot/PUT
// routing) and attrName() (Terraform attribute) intentionally return
// different strings; all snapshot access below uses s.key() ("rsyslogd").
type syslogSection struct{}

func init() {
	registerSection(syslogSection{})
}

func (syslogSection) key() string      { return "rsyslogd" }
func (syslogSection) attrName() string { return "syslog" }

// schemaAttribute is byte-identical to the inline "syslog" block in
// setting_resource.go's schema (Task 24a enforces this with a
// schema-equivalence golden).
func (syslogSection) schemaAttribute() schema.Attribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Remote syslog (rsyslogd) settings.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether remote syslog is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"ip": schema.StringAttribute{
				MarkdownDescription: "Remote syslog server IP address.",
				Optional:            true,
				Computed:            true,
			},
			"port": schema.Int64Attribute{
				MarkdownDescription: "Remote syslog server port (1-65535).",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
			},
			"contents": schema.ListAttribute{
				MarkdownDescription: "Logged facilities (e.g. `device`, `client`, `firewall_default_policy`, `triggers`, `updates`, `admin_activity`, `critical`, `security_detections`, `vpn`).",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"log_all_contents": schema.BoolAttribute{
				MarkdownDescription: "Log all available facilities.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"debug": schema.BoolAttribute{
				MarkdownDescription: "Enable debug logging.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"this_controller": schema.BoolAttribute{
				MarkdownDescription: "Also log this controller's events.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"this_controller_encrypted_only": schema.BoolAttribute{
				MarkdownDescription: "Only send this controller's logs over an encrypted channel.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"netconsole_enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether netconsole logging is enabled.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
			},
			"netconsole_host": schema.StringAttribute{
				MarkdownDescription: "Netconsole host.",
				Optional:            true,
				Computed:            true,
			},
			"netconsole_port": schema.Int64Attribute{
				MarkdownDescription: "Netconsole port (1-65535).",
				Optional:            true,
				Computed:            true,
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
			},
		},
	}
}

// decode populates model.Syslog from snap's "rsyslogd" section data.
func (s syslogSection) decode(ctx context.Context, snap rawSettings, prior settingResourceModel, model *settingResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	var priorModel settingSyslogModel
	if !prior.Syslog.IsNull() && !prior.Syslog.IsUnknown() {
		diags.Append(prior.Syslog.As(ctx, &priorModel, basetypes.ObjectAsOptions{})...)
	}

	sec, _ := snap.section(s.key())
	data := sec.Data

	enabled, d := decodeBool(data, "enabled", priorModel.Enabled)
	diags.Append(d...)
	contents, d := decodeStringList(ctx, data, "contents", priorModel.Contents)
	diags.Append(d...)
	debug, d := decodeBool(data, "debug", priorModel.Debug)
	diags.Append(d...)
	ip, d := decodeString(data, "ip", priorModel.IP)
	diags.Append(d...)
	port, d := decodeInt64(data, "port", priorModel.Port)
	diags.Append(d...)
	logAllContents, d := decodeBool(data, "log_all_contents", priorModel.LogAllContents)
	diags.Append(d...)
	netconsoleEnabled, d := decodeBool(data, "netconsole_enabled", priorModel.NetconsoleEnabled)
	diags.Append(d...)
	netconsoleHost, d := decodeString(data, "netconsole_host", priorModel.NetconsoleHost)
	diags.Append(d...)
	netconsolePort, d := decodeInt64(data, "netconsole_port", priorModel.NetconsolePort)
	diags.Append(d...)
	thisController, d := decodeBool(data, "this_controller", priorModel.ThisController)
	diags.Append(d...)
	thisControllerEncryptedOnly, d := decodeBool(data, "this_controller_encrypted_only", priorModel.ThisControllerEncryptedOnly)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m := settingSyslogModel{
		Enabled:                     enabled,
		Contents:                    contents,
		Debug:                       debug,
		IP:                          ip,
		Port:                        port,
		LogAllContents:              logAllContents,
		NetconsoleEnabled:           netconsoleEnabled,
		NetconsoleHost:              netconsoleHost,
		NetconsolePort:              netconsolePort,
		ThisController:              thisController,
		ThisControllerEncryptedOnly: thisControllerEncryptedOnly,
	}

	obj, objDiags := types.ObjectValueFrom(ctx, syslogAttrTypes, m)
	diags.Append(objDiags...)
	if diags.HasError() {
		return diags
	}

	model.Syslog = obj
	return diags
}

// overlay computes the "rsyslogd" PUT body from model.Syslog, starting from
// a deep copy of the snapshot's current section data so any unmodeled key
// already present on the controller is preserved. Returns configured ==
// false (no write) when the section is not configured (null/unknown) in
// model.
func (s syslogSection) overlay(ctx context.Context, model, prior settingResourceModel, snap rawSettings) (settings.RawSetting, bool, diag.Diagnostics) {
	var diags diag.Diagnostics

	if model.Syslog.IsNull() || model.Syslog.IsUnknown() {
		return settings.RawSetting{}, false, diags
	}

	var m settingSyslogModel
	diags.Append(model.Syslog.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	base := snap.dataCopy(s.key())
	overlayBool(base, "enabled", m.Enabled)
	diags.Append(overlayStringList(ctx, base, "contents", m.Contents)...)
	overlayBool(base, "debug", m.Debug)
	overlayString(base, "ip", m.IP)
	overlayInt64(base, "port", m.Port)
	overlayBool(base, "log_all_contents", m.LogAllContents)
	overlayBool(base, "netconsole_enabled", m.NetconsoleEnabled)
	overlayString(base, "netconsole_host", m.NetconsoleHost)
	overlayInt64(base, "netconsole_port", m.NetconsolePort)
	overlayBool(base, "this_controller", m.ThisController)
	overlayBool(base, "this_controller_encrypted_only", m.ThisControllerEncryptedOnly)
	if diags.HasError() {
		return settings.RawSetting{}, false, diags
	}

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: s.key()},
		Data:        base,
	}
	return rs, true, diags
}

// carryBestEffort copies the plan's syslog value onto dst. This section
// holds no secret leaves, so it is a straight copy with no per-leaf
// plan/prior choice needed.
func (syslogSection) carryBestEffort(dst *settingResourceModel, plan settingResourceModel) diag.Diagnostics {
	dst.Syslog = plan.Syslog
	return nil
}

// isConfigured reports whether m.Syslog is set (non-null, non-unknown),
// gating whether Create/Update push this section to the controller
// (wire key "rsyslogd") at all.
func (syslogSection) isConfigured(m settingResourceModel) bool {
	return !m.Syslog.IsNull() && !m.Syslog.IsUnknown()
}
