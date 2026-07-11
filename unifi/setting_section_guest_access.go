package unifi

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

// settingGuestAccessModel is the nested guest_access block: the guest
// hotspot/captive portal. Attribute names align with filipowm's
// unifi_setting_guest_access wherever fields overlap. Unlike filipowm, the
// *_enabled flags are explicit attributes (configuring a provider block does
// NOT implicitly enable it) — the raw-merge engine only ever writes fields
// the user set.
type settingGuestAccessModel struct {
	AllowedSubnet       types.String `tfsdk:"allowed_subnet"`
	RestrictedSubnet    types.String `tfsdk:"restricted_subnet"`
	Auth                types.String `tfsdk:"auth"`
	AuthUrl             types.String `tfsdk:"auth_url"`
	CustomIP            types.String `tfsdk:"custom_ip"`
	EcEnabled           types.Bool   `tfsdk:"ec_enabled"`
	Expire              types.Int64  `tfsdk:"expire"`
	ExpireNumber        types.Int64  `tfsdk:"expire_number"`
	ExpireUnit          types.Int64  `tfsdk:"expire_unit"`
	PortalCustomization types.Object `tfsdk:"portal_customization"`
	PortalEnabled       types.Bool   `tfsdk:"portal_enabled"`
	PortalHostname      types.String `tfsdk:"portal_hostname"`
	PortalUseHostname   types.Bool   `tfsdk:"portal_use_hostname"`
	Redirect            types.Object `tfsdk:"redirect"`
	RedirectEnabled     types.Bool   `tfsdk:"redirect_enabled"`
	TemplateEngine      types.String `tfsdk:"template_engine"`
	VoucherCustomized   types.Bool   `tfsdk:"voucher_customized"`
	VoucherEnabled      types.Bool   `tfsdk:"voucher_enabled"`
}

type settingGuestAccessRedirectModel struct {
	ToHttps  types.Bool   `tfsdk:"to_https"`
	URL      types.String `tfsdk:"url"`
	UseHttps types.Bool   `tfsdk:"use_https"`
}

type settingGuestAccessPortalCustomizationModel struct {
	Customized             types.Bool   `tfsdk:"customized"`
	AuthenticationText     types.String `tfsdk:"authentication_text"`
	BgColor                types.String `tfsdk:"bg_color"`
	BgImageEnabled         types.Bool   `tfsdk:"bg_image_enabled"`
	BgImageFileID          types.String `tfsdk:"bg_image_file_id"`
	BgImageTile            types.Bool   `tfsdk:"bg_image_tile"`
	BgType                 types.String `tfsdk:"bg_type"`
	BoxColor               types.String `tfsdk:"box_color"`
	BoxLinkColor           types.String `tfsdk:"box_link_color"`
	BoxOpacity             types.Int64  `tfsdk:"box_opacity"`
	BoxRadius              types.Int64  `tfsdk:"box_radius"`
	BoxTextColor           types.String `tfsdk:"box_text_color"`
	ButtonColor            types.String `tfsdk:"button_color"`
	ButtonText             types.String `tfsdk:"button_text"`
	ButtonTextColor        types.String `tfsdk:"button_text_color"`
	Languages              types.List   `tfsdk:"languages"`
	LinkColor              types.String `tfsdk:"link_color"`
	LogoEnabled            types.Bool   `tfsdk:"logo_enabled"`
	LogoFileID             types.String `tfsdk:"logo_file_id"`
	LogoPosition           types.String `tfsdk:"logo_position"`
	LogoSize               types.Int64  `tfsdk:"logo_size"`
	SuccessText            types.String `tfsdk:"success_text"`
	TextColor              types.String `tfsdk:"text_color"`
	Title                  types.String `tfsdk:"title"`
	Tos                    types.String `tfsdk:"tos"`
	TosEnabled             types.Bool   `tfsdk:"tos_enabled"`
	UnsplashAuthorName     types.String `tfsdk:"unsplash_author_name"`
	UnsplashAuthorUsername types.String `tfsdk:"unsplash_author_username"`
	WelcomeText            types.String `tfsdk:"welcome_text"`
	WelcomeTextEnabled     types.Bool   `tfsdk:"welcome_text_enabled"`
	WelcomeTextPosition    types.String `tfsdk:"welcome_text_position"`
}

var (
	guestAccessRedirectAttrTypes = map[string]attr.Type{
		"to_https":  types.BoolType,
		"url":       types.StringType,
		"use_https": types.BoolType,
	}
	guestAccessPortalCustomizationAttrTypes = map[string]attr.Type{
		"customized":               types.BoolType,
		"authentication_text":      types.StringType,
		"bg_color":                 types.StringType,
		"bg_image_enabled":         types.BoolType,
		"bg_image_file_id":         types.StringType,
		"bg_image_tile":            types.BoolType,
		"bg_type":                  types.StringType,
		"box_color":                types.StringType,
		"box_link_color":           types.StringType,
		"box_opacity":              types.Int64Type,
		"box_radius":               types.Int64Type,
		"box_text_color":           types.StringType,
		"button_color":             types.StringType,
		"button_text":              types.StringType,
		"button_text_color":        types.StringType,
		"languages":                types.ListType{ElemType: types.StringType},
		"link_color":               types.StringType,
		"logo_enabled":             types.BoolType,
		"logo_file_id":             types.StringType,
		"logo_position":            types.StringType,
		"logo_size":                types.Int64Type,
		"success_text":             types.StringType,
		"text_color":               types.StringType,
		"title":                    types.StringType,
		"tos":                      types.StringType,
		"tos_enabled":              types.BoolType,
		"unsplash_author_name":     types.StringType,
		"unsplash_author_username": types.StringType,
		"welcome_text":             types.StringType,
		"welcome_text_enabled":     types.BoolType,
		"welcome_text_position":    types.StringType,
	}
	guestAccessAttrTypes = map[string]attr.Type{
		"allowed_subnet":    types.StringType,
		"restricted_subnet": types.StringType,
		"auth":              types.StringType,
		"auth_url":          types.StringType,
		"custom_ip":         types.StringType,
		"ec_enabled":        types.BoolType,
		"expire":            types.Int64Type,
		"expire_number":     types.Int64Type,
		"expire_unit":       types.Int64Type,
		"portal_customization": types.ObjectType{
			AttrTypes: guestAccessPortalCustomizationAttrTypes,
		},
		"portal_enabled":      types.BoolType,
		"portal_hostname":     types.StringType,
		"portal_use_hostname": types.BoolType,
		"redirect":            types.ObjectType{AttrTypes: guestAccessRedirectAttrTypes},
		"redirect_enabled":    types.BoolType,
		"template_engine":     types.StringType,
		"voucher_customized":  types.BoolType,
		"voucher_enabled":     types.BoolType,
	}
)

var hexColorValidator = stringvalidator.RegexMatches(
	regexp.MustCompile(`^#[0-9a-fA-F]{6}$|^#[0-9a-fA-F]{3}$|^$`),
	"must be a hex color like #FFF or #FFFFFF",
)

type guestAccessSection struct{}

func (guestAccessSection) key() string { return "guest_access" }

func (guestAccessSection) attrTypes() map[string]attr.Type { return guestAccessAttrTypes }

func (guestAccessSection) schemaAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Guest hotspot / captive portal settings. Attribute names align with the " +
			"filipowm provider's `unifi_setting_guest_access` for config portability. Note: unlike " +
			"filipowm, provider blocks (`facebook`, `google`, payment gateways, …) do not implicitly " +
			"flip their `*_enabled` flag — set the flag explicitly alongside the block. Controller " +
			"fields not modeled here (e.g. `restricted_subnet_1..3`) are preserved across updates.",
		Optional: true,
		Computed: true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"allowed_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet allowed for guest access.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_subnet": schema.StringAttribute{
				MarkdownDescription: "Subnet restricted from guest access.",
				Optional:            true,
				Computed:            true,
			},
			"auth": schema.StringAttribute{
				MarkdownDescription: "Authentication method: `none`, `hotspot`, `facebook_wifi`, or `custom`. " +
					"For password/voucher/payment authentication set `auth = \"hotspot\"` plus the matching `*_enabled` flag.",
				Optional: true,
				Computed: true,
				Validators: []validator.String{
					stringvalidator.OneOf("none", "hotspot", "facebook_wifi", "custom"),
				},
			},
			"auth_url": schema.StringAttribute{
				MarkdownDescription: "External portal authentication URL (for `auth = \"custom\"`).",
				Optional:            true,
				Computed:            true,
			},
			"custom_ip": schema.StringAttribute{
				MarkdownDescription: "External portal server IPv4 address (for `auth = \"custom\"`).",
				Optional:            true,
				Computed:            true,
			},
			"ec_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable enterprise controller functionality.",
				Optional:            true,
				Computed:            true,
			},
			"expire": schema.Int64Attribute{
				MarkdownDescription: "Guest authorization lifetime in minutes (kept in sync with `expire_number` × `expire_unit` by the controller).",
				Optional:            true,
				Computed:            true,
			},
			"expire_number": schema.Int64Attribute{
				MarkdownDescription: "Number component of the authorization lifetime.",
				Optional:            true,
				Computed:            true,
			},
			"expire_unit": schema.Int64Attribute{
				MarkdownDescription: "Unit component of the authorization lifetime: `1` (minute), `60` (hour), `1440` (day), `10080` (week).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.OneOf(1, 60, 1440, 10080),
				},
			},
			"portal_customization": guestAccessPortalCustomizationSchema(),
			"portal_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the guest portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_hostname": schema.StringAttribute{
				MarkdownDescription: "Custom hostname for the captive portal.",
				Optional:            true,
				Computed:            true,
			},
			"portal_use_hostname": schema.BoolAttribute{
				MarkdownDescription: "Use `portal_hostname` for portal URLs.",
				Optional:            true,
				Computed:            true,
			},
			"redirect": schema.SingleNestedAttribute{
				MarkdownDescription: "Redirect-after-authentication settings (enable with `redirect_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"to_https": schema.BoolAttribute{
						MarkdownDescription: "Redirect HTTP requests to HTTPS.",
						Optional:            true,
						Computed:            true,
					},
					"url": schema.StringAttribute{
						MarkdownDescription: "URL to redirect to after authentication.",
						Optional:            true,
						Computed:            true,
					},
					"use_https": schema.BoolAttribute{
						MarkdownDescription: "Use HTTPS for the redirect.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"redirect_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable redirect after authentication.",
				Optional:            true,
				Computed:            true,
			},
			"template_engine": schema.StringAttribute{
				MarkdownDescription: "Portal template engine: `jsp` or `angular`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("jsp", "angular"),
				},
			},
			"voucher_customized": schema.BoolAttribute{
				MarkdownDescription: "Whether vouchers are customized.",
				Optional:            true,
				Computed:            true,
			},
			"voucher_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable voucher authentication (requires `auth = \"hotspot\"`).",
				Optional:            true,
				Computed:            true,
			},
		},
	}
}

func guestAccessPortalCustomizationSchema() schema.SingleNestedAttribute {
	hexColor := []validator.String{hexColorValidator}
	return schema.SingleNestedAttribute{
		MarkdownDescription: "Portal look & feel. `bg_image_enabled`/`logo_enabled` are a superset over filipowm.",
		Optional:            true,
		Computed:            true,
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseStateForUnknown(),
		},
		Attributes: map[string]schema.Attribute{
			"customized": schema.BoolAttribute{
				MarkdownDescription: "Whether the portal is customized.",
				Optional:            true,
				Computed:            true,
			},
			"authentication_text": schema.StringAttribute{
				MarkdownDescription: "Custom authentication text.",
				Optional:            true,
				Computed:            true,
			},
			"bg_color": schema.StringAttribute{
				MarkdownDescription: "Background color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"bg_image_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_image_file_id": schema.StringAttribute{
				MarkdownDescription: "Portal file ID of the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_image_tile": schema.BoolAttribute{
				MarkdownDescription: "Tile the background image.",
				Optional:            true,
				Computed:            true,
			},
			"bg_type": schema.StringAttribute{
				MarkdownDescription: "Background type: `color`, `image`, or `gallery`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("color", "image", "gallery"),
				},
			},
			"box_color": schema.StringAttribute{
				MarkdownDescription: "Login box color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"box_link_color": schema.StringAttribute{
				MarkdownDescription: "Login box link color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"box_opacity": schema.Int64Attribute{
				MarkdownDescription: "Login box opacity (0-100).",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.Between(0, 100),
				},
			},
			"box_radius": schema.Int64Attribute{
				MarkdownDescription: "Login box border radius in pixels.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"box_text_color": schema.StringAttribute{
				MarkdownDescription: "Login box text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"button_color": schema.StringAttribute{
				MarkdownDescription: "Button color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"button_text": schema.StringAttribute{
				MarkdownDescription: "Login button text.",
				Optional:            true,
				Computed:            true,
			},
			"button_text_color": schema.StringAttribute{
				MarkdownDescription: "Button text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"languages": schema.ListAttribute{
				MarkdownDescription: "Enabled portal languages, in display order.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				MarkdownDescription: "Link color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"logo_enabled": schema.BoolAttribute{
				MarkdownDescription: "Show the logo.",
				Optional:            true,
				Computed:            true,
			},
			"logo_file_id": schema.StringAttribute{
				MarkdownDescription: "Portal file ID of the logo image.",
				Optional:            true,
				Computed:            true,
			},
			"logo_position": schema.StringAttribute{
				MarkdownDescription: "Logo position: `left`, `center`, or `right`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("left", "center", "right"),
				},
			},
			"logo_size": schema.Int64Attribute{
				MarkdownDescription: "Logo size in pixels.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.Int64{
					int64validator.AtLeast(0),
				},
			},
			"success_text": schema.StringAttribute{
				MarkdownDescription: "Text shown after successful authentication.",
				Optional:            true,
				Computed:            true,
			},
			"text_color": schema.StringAttribute{
				MarkdownDescription: "Main text color (hex).",
				Optional:            true,
				Computed:            true,
				Validators:          hexColor,
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Portal page title.",
				Optional:            true,
				Computed:            true,
			},
			"tos": schema.StringAttribute{
				MarkdownDescription: "Terms of service text.",
				Optional:            true,
				Computed:            true,
			},
			"tos_enabled": schema.BoolAttribute{
				MarkdownDescription: "Require terms of service acceptance.",
				Optional:            true,
				Computed:            true,
			},
			"unsplash_author_name": schema.StringAttribute{
				MarkdownDescription: "Unsplash author name for gallery backgrounds.",
				Optional:            true,
				Computed:            true,
			},
			"unsplash_author_username": schema.StringAttribute{
				MarkdownDescription: "Unsplash author username for gallery backgrounds.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text": schema.StringAttribute{
				MarkdownDescription: "Welcome text.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text_enabled": schema.BoolAttribute{
				MarkdownDescription: "Show the welcome text.",
				Optional:            true,
				Computed:            true,
			},
			"welcome_text_position": schema.StringAttribute{
				MarkdownDescription: "Welcome text position: `under_logo` or `above_boxes`.",
				Optional:            true,
				Computed:            true,
				Validators: []validator.String{
					stringvalidator.OneOf("under_logo", "above_boxes"),
				},
			},
		},
	}
}

func (guestAccessSection) get(m *settingResourceModel) types.Object { return m.GuestAccess }

func (guestAccessSection) set(m *settingResourceModel, obj types.Object) { m.GuestAccess = obj }

func (guestAccessSection) overlay(
	ctx context.Context,
	obj types.Object,
	data map[string]any,
) diag.Diagnostics {
	var diags diag.Diagnostics
	var m settingGuestAccessModel
	diags.Append(obj.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return diags
	}
	guestAccessModelToData(ctx, &m, data, &diags)
	return diags
}

// guestAccessModelToData writes only the user-set fields into the raw
// section document; unset fields — including controller fields go-unifi
// does not model, like restricted_subnet_1..3 — keep their remote values.
// Secret fields use the controller's x_ key prefix.
func guestAccessModelToData(
	ctx context.Context,
	m *settingGuestAccessModel,
	data map[string]any,
	diags *diag.Diagnostics,
) {
	setRawString(data, "allowed_subnet_", m.AllowedSubnet)
	setRawString(data, "restricted_subnet_", m.RestrictedSubnet)
	setRawString(data, "auth", m.Auth)
	setRawString(data, "auth_url", m.AuthUrl)
	setRawString(data, "custom_ip", m.CustomIP)
	setRawBool(data, "ec_enabled", m.EcEnabled)
	setRawInt(data, "expire", m.Expire)
	setRawInt(data, "expire_number", m.ExpireNumber)
	setRawInt(data, "expire_unit", m.ExpireUnit)
	setRawBool(data, "portal_enabled", m.PortalEnabled)
	setRawString(data, "portal_hostname", m.PortalHostname)
	setRawBool(data, "portal_use_hostname", m.PortalUseHostname)
	setRawBool(data, "redirect_enabled", m.RedirectEnabled)
	setRawString(data, "template_engine", m.TemplateEngine)
	setRawBool(data, "voucher_customized", m.VoucherCustomized)
	setRawBool(data, "voucher_enabled", m.VoucherEnabled)

	if !m.Redirect.IsNull() && !m.Redirect.IsUnknown() {
		var r settingGuestAccessRedirectModel
		diags.Append(m.Redirect.As(ctx, &r, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "redirect_https", r.UseHttps)
		setRawBool(data, "redirect_to_https", r.ToHttps)
		setRawString(data, "redirect_url", r.URL)
	}

	if !m.PortalCustomization.IsNull() && !m.PortalCustomization.IsUnknown() {
		var pc settingGuestAccessPortalCustomizationModel
		diags.Append(m.PortalCustomization.As(ctx, &pc, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "portal_customized", pc.Customized)
		setRawString(data, "portal_customized_authentication_text", pc.AuthenticationText)
		setRawString(data, "portal_customized_bg_color", pc.BgColor)
		setRawBool(data, "portal_customized_bg_image_enabled", pc.BgImageEnabled)
		setRawString(data, "portal_customized_bg_image_filename", pc.BgImageFileID)
		setRawBool(data, "portal_customized_bg_image_tile", pc.BgImageTile)
		setRawString(data, "portal_customized_bg_type", pc.BgType)
		setRawString(data, "portal_customized_box_color", pc.BoxColor)
		setRawString(data, "portal_customized_box_link_color", pc.BoxLinkColor)
		setRawInt(data, "portal_customized_box_opacity", pc.BoxOpacity)
		setRawInt(data, "portal_customized_box_radius", pc.BoxRadius)
		setRawString(data, "portal_customized_box_text_color", pc.BoxTextColor)
		setRawString(data, "portal_customized_button_color", pc.ButtonColor)
		setRawString(data, "portal_customized_button_text", pc.ButtonText)
		setRawString(data, "portal_customized_button_text_color", pc.ButtonTextColor)
		if !pc.Languages.IsNull() && !pc.Languages.IsUnknown() {
			var langs []string
			diags.Append(pc.Languages.ElementsAs(ctx, &langs, false)...)
			data["portal_customized_languages"] = langs
		}
		setRawString(data, "portal_customized_link_color", pc.LinkColor)
		setRawBool(data, "portal_customized_logo_enabled", pc.LogoEnabled)
		setRawString(data, "portal_customized_logo_filename", pc.LogoFileID)
		setRawString(data, "portal_customized_logo_position", pc.LogoPosition)
		setRawInt(data, "portal_customized_logo_size", pc.LogoSize)
		setRawString(data, "portal_customized_success_text", pc.SuccessText)
		setRawString(data, "portal_customized_text_color", pc.TextColor)
		setRawString(data, "portal_customized_title", pc.Title)
		setRawString(data, "portal_customized_tos", pc.Tos)
		setRawBool(data, "portal_customized_tos_enabled", pc.TosEnabled)
		setRawString(data, "portal_customized_unsplash_author_name", pc.UnsplashAuthorName)
		setRawString(data, "portal_customized_unsplash_author_username", pc.UnsplashAuthorUsername)
		setRawString(data, "portal_customized_welcome_text", pc.WelcomeText)
		setRawBool(data, "portal_customized_welcome_text_enabled", pc.WelcomeTextEnabled)
		setRawString(data, "portal_customized_welcome_text_position", pc.WelcomeTextPosition)
	}
}

// read pulls the guest_access section out of the raw settings list.
//
// TODO(go-unifi): use ui.GetSetting[*settings.GuestAccess] once upstream
// models `expire` as a number — the generated struct declares it string,
// so the typed unmarshal fails on live controllers that send `"expire": 480`.
func (guestAccessSection) read(
	ctx context.Context,
	client *Client,
	site string,
) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	raws, err := client.ListSettings(ctx, site)
	if err != nil {
		diags.AddError("Error Reading Guest Access Setting", err.Error())
		return types.ObjectNull(guestAccessAttrTypes), diags
	}
	for _, raw := range raws {
		if raw.GetKey() != "guest_access" {
			continue
		}
		model := guestAccessDataToModel(ctx, raw.Data, &diags)
		if diags.HasError() {
			return types.ObjectNull(guestAccessAttrTypes), diags
		}
		return types.ObjectValueFrom(ctx, guestAccessAttrTypes, model)
	}
	return types.ObjectNull(guestAccessAttrTypes), diags
}

// guestAccessDataToModel converts the raw section document to the model.
// Nested blocks materialize only when at least one of their keys exists.
func guestAccessDataToModel(
	ctx context.Context,
	data map[string]any,
	diags *diag.Diagnostics,
) settingGuestAccessModel {
	m := settingGuestAccessModel{
		AllowedSubnet:     rawString(data, "allowed_subnet_"),
		RestrictedSubnet:  rawString(data, "restricted_subnet_"),
		Auth:              rawString(data, "auth"),
		AuthUrl:           rawString(data, "auth_url"),
		CustomIP:          rawString(data, "custom_ip"),
		EcEnabled:         rawBool(data, "ec_enabled"),
		Expire:            rawInt(data, "expire"),
		ExpireNumber:      rawInt(data, "expire_number"),
		ExpireUnit:        rawInt(data, "expire_unit"),
		PortalEnabled:     rawBool(data, "portal_enabled"),
		PortalHostname:    rawString(data, "portal_hostname"),
		PortalUseHostname: rawBool(data, "portal_use_hostname"),
		RedirectEnabled:   rawBool(data, "redirect_enabled"),
		TemplateEngine:    rawString(data, "template_engine"),
		VoucherCustomized: rawBool(data, "voucher_customized"),
		VoucherEnabled:    rawBool(data, "voucher_enabled"),
	}

	m.Redirect = types.ObjectNull(guestAccessRedirectAttrTypes)
	if anyRawKey(data, "redirect_https", "redirect_to_https", "redirect_url") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessRedirectAttrTypes,
			settingGuestAccessRedirectModel{
				ToHttps:  rawBool(data, "redirect_to_https"),
				URL:      rawString(data, "redirect_url"),
				UseHttps: rawBool(data, "redirect_https"),
			})
		diags.Append(d...)
		m.Redirect = obj
	}

	m.PortalCustomization = types.ObjectNull(guestAccessPortalCustomizationAttrTypes)
	if anyRawKey(data, "portal_customized") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessPortalCustomizationAttrTypes,
			settingGuestAccessPortalCustomizationModel{
				Customized:             rawBool(data, "portal_customized"),
				AuthenticationText:     rawString(data, "portal_customized_authentication_text"),
				BgColor:                rawString(data, "portal_customized_bg_color"),
				BgImageEnabled:         rawBool(data, "portal_customized_bg_image_enabled"),
				BgImageFileID:          rawString(data, "portal_customized_bg_image_filename"),
				BgImageTile:            rawBool(data, "portal_customized_bg_image_tile"),
				BgType:                 rawString(data, "portal_customized_bg_type"),
				BoxColor:               rawString(data, "portal_customized_box_color"),
				BoxLinkColor:           rawString(data, "portal_customized_box_link_color"),
				BoxOpacity:             rawInt(data, "portal_customized_box_opacity"),
				BoxRadius:              rawInt(data, "portal_customized_box_radius"),
				BoxTextColor:           rawString(data, "portal_customized_box_text_color"),
				ButtonColor:            rawString(data, "portal_customized_button_color"),
				ButtonText:             rawString(data, "portal_customized_button_text"),
				ButtonTextColor:        rawString(data, "portal_customized_button_text_color"),
				Languages:              rawStringList(data, "portal_customized_languages"),
				LinkColor:              rawString(data, "portal_customized_link_color"),
				LogoEnabled:            rawBool(data, "portal_customized_logo_enabled"),
				LogoFileID:             rawString(data, "portal_customized_logo_filename"),
				LogoPosition:           rawString(data, "portal_customized_logo_position"),
				LogoSize:               rawInt(data, "portal_customized_logo_size"),
				SuccessText:            rawString(data, "portal_customized_success_text"),
				TextColor:              rawString(data, "portal_customized_text_color"),
				Title:                  rawString(data, "portal_customized_title"),
				Tos:                    rawString(data, "portal_customized_tos"),
				TosEnabled:             rawBool(data, "portal_customized_tos_enabled"),
				UnsplashAuthorName:     rawString(data, "portal_customized_unsplash_author_name"),
				UnsplashAuthorUsername: rawString(data, "portal_customized_unsplash_author_username"),
				WelcomeText:            rawString(data, "portal_customized_welcome_text"),
				WelcomeTextEnabled:     rawBool(data, "portal_customized_welcome_text_enabled"),
				WelcomeTextPosition:    rawString(data, "portal_customized_welcome_text_position"),
			})
		diags.Append(d...)
		m.PortalCustomization = obj
	}

	return m
}
