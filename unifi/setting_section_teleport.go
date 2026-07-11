package unifi

import (
	"context"
	"errors"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// teleportSubnetRegexp is the controller's own validation: an IPv4 CIDR
// with a /8–/32 prefix, or empty (auto).
var teleportSubnetRegexp = regexp.MustCompile(
	`^(([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])\.){3}` +
		`([0-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-5])` +
		`/([8-9]|[1-2][0-9]|3[0-2])$|^$`)

// settingTeleportModel is the nested teleport block: UniFi's WireGuard-based
// one-click remote access. Attribute names align with filipowm's
// unifi_setting_teleport (`subnet` is their rename of the raw subnet_cidr).
type settingTeleportModel struct {
	Enabled types.Bool   `tfsdk:"enabled"`
	Subnet  types.String `tfsdk:"subnet"`
}

var teleportAttrTypes = map[string]attr.Type{
	"enabled": types.BoolType,
	"subnet":  types.StringType,
}

type teleportSection struct{}

func (teleportSection) key() string { return "teleport" }

func (teleportSection) attrTypes() map[string]attr.Type { return teleportAttrTypes }

func (teleportSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Teleport (one-click WireGuard remote access) " +
			"settings. Requires controller version 7.2 or later.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether Teleport is enabled.",
				Optional:            true,
				Computed:            true,
			},
			"subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet CIDR used for Teleport clients " +
					"(e.g. `192.168.100.0/24`). Empty string means the " +
					"controller chooses automatically.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						teleportSubnetRegexp,
						"must be an IPv4 CIDR (/8–/32) or empty",
					),
				},
			},
		},
	}
}

func (teleportSection) get(m *settingResourceModel) types.Object { return m.Teleport }

func (teleportSection) set(m *settingResourceModel, obj types.Object) { m.Teleport = obj }

func (teleportSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingTeleportModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	teleportModelToData(&m, data)
	return diags
}

// teleportModelToData writes only the user-set fields into the raw section
// document; unset fields keep their remote values.
func teleportModelToData(m *settingTeleportModel, data map[string]any) {
	if !m.Enabled.IsNull() && !m.Enabled.IsUnknown() {
		data["enabled"] = m.Enabled.ValueBool()
	}
	if !m.Subnet.IsNull() && !m.Subnet.IsUnknown() {
		data["subnet_cidr"] = m.Subnet.ValueString()
	}
}

func (teleportSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	_, setting, err := ui.GetSetting[*settings.Teleport](client.ApiClient, ctx, site)
	if err != nil {
		var notFound *ui.NotFoundError
		if errors.As(err, &notFound) {
			return types.ObjectNull(teleportAttrTypes), diags
		}
		diags.AddError("Error Reading Teleport Setting", err.Error())
		return types.ObjectNull(teleportAttrTypes), diags
	}
	return types.ObjectValueFrom(ctx, teleportAttrTypes, teleportSettingToModel(setting))
}

func teleportSettingToModel(s *settings.Teleport) settingTeleportModel {
	return settingTeleportModel{
		Enabled: types.BoolValue(s.Enabled),
		// Deliberately StringValue, not StringValueOrNull: "" is the
		// controller's "auto" value and must round-trip as "".
		Subnet: types.StringValue(s.SubnetCidr),
	}
}
