package unifi

import (
	"context"
	"errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// settingSnmpModel is the nested snmp block: the site SNMP v1/v2c community
// and SNMPv3 agent credentials. community and password are Sensitive;
// password maps to the controller's write-only x_password and is never read
// back (the configured value is preserved, mirroring mgmt.ssh_password).
type settingSnmpModel struct {
	Community types.String `tfsdk:"community"`
	Enabled   types.Bool   `tfsdk:"enabled"`
	EnabledV3 types.Bool   `tfsdk:"enabled_v3"`
	Username  types.String `tfsdk:"username"`
	Password  types.String `tfsdk:"password"`
}

var snmpAttrTypes = map[string]attr.Type{
	"community":  types.StringType,
	"enabled":    types.BoolType,
	"enabled_v3": types.BoolType,
	"username":   types.StringType,
	"password":   types.StringType,
}

type snmpSection struct{}

func (snmpSection) key() string { return "snmp" }

func (snmpSection) attrTypes() map[string]attr.Type { return snmpAttrTypes }

func (snmpSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Site SNMP agent settings (v1/v2c community and SNMPv3 credentials).",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"community": schema.StringAttribute{
				MarkdownDescription: "SNMP v1/v2c community string.",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the SNMP v1/v2c agent.",
				Optional:            true,
				Computed:            true,
			},
			"enabled_v3": schema.BoolAttribute{
				MarkdownDescription: "Enable the SNMPv3 agent.",
				Optional:            true,
				Computed:            true,
			},
			"username": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 username.",
				Optional:            true,
				Computed:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "SNMPv3 password (8–32 characters). Sensitive — the controller " +
					"treats this as write-only, so the value is kept from configuration and not read back.",
				Optional:  true,
				Sensitive: true,
			},
		},
	}
}

func (snmpSection) get(m *settingResourceModel) types.Object { return m.Snmp }

// set installs a freshly-read snmp object, carrying the configured password
// forward: x_password is write-only on the controller, so the read value can
// never be trusted to round-trip.
func (snmpSection) set(m *settingResourceModel, obj types.Object) {
	m.Snmp = preserveSnmpPassword(m.Snmp, obj)
}

// preserveSnmpPassword returns fresh with the password attribute carried over
// from prior (the plan or prior state). If prior has no password, fresh
// passes through unchanged.
func preserveSnmpPassword(prior, fresh types.Object) types.Object {
	if prior.IsNull() || prior.IsUnknown() || fresh.IsNull() || fresh.IsUnknown() {
		return fresh
	}
	pv, ok := prior.Attributes()["password"]
	if !ok || pv.IsNull() || pv.IsUnknown() {
		return fresh
	}
	attrs := make(map[string]attr.Value, len(fresh.Attributes()))
	for k, v := range fresh.Attributes() {
		attrs[k] = v
	}
	attrs["password"] = pv
	merged, d := types.ObjectValue(snmpAttrTypes, attrs)
	if d.HasError() {
		return fresh
	}
	return merged
}

func (snmpSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingSnmpModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	snmpModelToData(&m, data)
	return diags
}

// snmpModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values. Note the controller's key
// names: enabledV3 (camelCase) and x_password (write-only secret prefix).
func snmpModelToData(m *settingSnmpModel, data map[string]any) {
	if !m.Community.IsNull() && !m.Community.IsUnknown() {
		data["community"] = m.Community.ValueString()
	}
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.EnabledV3.IsNull() && !m.EnabledV3.IsUnknown() {
		data["enabledV3"] = m.EnabledV3.ValueBool()
	}
	if !m.Username.IsNull() && !m.Username.IsUnknown() {
		data["username"] = m.Username.ValueString()
	}
	if !m.Password.IsNull() && !m.Password.IsUnknown() {
		data["x_password"] = m.Password.ValueString()
	}
}

func (snmpSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Snmp](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(snmpAttrTypes), diags
		}
		diags.AddError("Error Reading SNMP Setting", err.Error())
		return types.ObjectNull(snmpAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, snmpAttrTypes, snmpSettingToModel(setting))
}

func snmpSettingToModel(s *settings.Snmp) settingSnmpModel {
	return settingSnmpModel{
		Community: util.StringValueOrNull(s.Community),
		Enabled:   types.BoolValue(s.Enabled),
		EnabledV3: types.BoolValue(s.EnabledV3),
		Username:  util.StringValueOrNull(s.Username),
		// x_password is write-only; never surface what the controller returns.
		Password: types.StringNull(),
	}
}
