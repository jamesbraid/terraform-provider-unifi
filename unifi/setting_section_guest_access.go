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

	Facebook             types.Object `tfsdk:"facebook"`
	FacebookEnabled      types.Bool   `tfsdk:"facebook_enabled"`
	FacebookWifi         types.Object `tfsdk:"facebook_wifi"`
	Google               types.Object `tfsdk:"google"`
	GoogleEnabled        types.Bool   `tfsdk:"google_enabled"`
	Password             types.String `tfsdk:"password"`
	PasswordEnabled      types.Bool   `tfsdk:"password_enabled"`
	Radius               types.Object `tfsdk:"radius"`
	RadiusEnabled        types.Bool   `tfsdk:"radius_enabled"`
	RestrictedDNSEnabled types.Bool   `tfsdk:"restricted_dns_enabled"`
	RestrictedDNSServers types.List   `tfsdk:"restricted_dns_servers"`
	Wechat               types.Object `tfsdk:"wechat"`
	WechatEnabled        types.Bool   `tfsdk:"wechat_enabled"`
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

type settingGuestAccessFacebookModel struct {
	AppID      types.String `tfsdk:"app_id"`
	AppSecret  types.String `tfsdk:"app_secret"`
	ScopeEmail types.Bool   `tfsdk:"scope_email"`
}

type settingGuestAccessFacebookWifiModel struct {
	BlockHttps    types.Bool   `tfsdk:"block_https"`
	GatewayID     types.String `tfsdk:"gateway_id"`
	GatewayName   types.String `tfsdk:"gateway_name"`
	GatewaySecret types.String `tfsdk:"gateway_secret"`
}

type settingGuestAccessGoogleModel struct {
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	Domain       types.String `tfsdk:"domain"`
	ScopeEmail   types.Bool   `tfsdk:"scope_email"`
}

type settingGuestAccessRadiusModel struct {
	AuthType          types.String `tfsdk:"auth_type"`
	DisconnectEnabled types.Bool   `tfsdk:"disconnect_enabled"`
	DisconnectPort    types.Int64  `tfsdk:"disconnect_port"`
	ProfileID         types.String `tfsdk:"profile_id"`
}

type settingGuestAccessWechatModel struct {
	AppID     types.String `tfsdk:"app_id"`
	AppSecret types.String `tfsdk:"app_secret"`
	SecretKey types.String `tfsdk:"secret_key"`
	ShopID    types.String `tfsdk:"shop_id"`
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
	guestAccessFacebookAttrTypes = map[string]attr.Type{
		"app_id":      types.StringType,
		"app_secret":  types.StringType,
		"scope_email": types.BoolType,
	}
	guestAccessFacebookWifiAttrTypes = map[string]attr.Type{
		"block_https":    types.BoolType,
		"gateway_id":     types.StringType,
		"gateway_name":   types.StringType,
		"gateway_secret": types.StringType,
	}
	guestAccessGoogleAttrTypes = map[string]attr.Type{
		"client_id":     types.StringType,
		"client_secret": types.StringType,
		"domain":        types.StringType,
		"scope_email":   types.BoolType,
	}
	guestAccessRadiusAttrTypes = map[string]attr.Type{
		"auth_type":          types.StringType,
		"disconnect_enabled": types.BoolType,
		"disconnect_port":    types.Int64Type,
		"profile_id":         types.StringType,
	}
	guestAccessWechatAttrTypes = map[string]attr.Type{
		"app_id":     types.StringType,
		"app_secret": types.StringType,
		"secret_key": types.StringType,
		"shop_id":    types.StringType,
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

		"facebook":               types.ObjectType{AttrTypes: guestAccessFacebookAttrTypes},
		"facebook_enabled":       types.BoolType,
		"facebook_wifi":          types.ObjectType{AttrTypes: guestAccessFacebookWifiAttrTypes},
		"google":                 types.ObjectType{AttrTypes: guestAccessGoogleAttrTypes},
		"google_enabled":         types.BoolType,
		"password":               types.StringType,
		"password_enabled":       types.BoolType,
		"radius":                 types.ObjectType{AttrTypes: guestAccessRadiusAttrTypes},
		"radius_enabled":         types.BoolType,
		"restricted_dns_enabled": types.BoolType,
		"restricted_dns_servers": types.ListType{ElemType: types.StringType},
		"wechat":                 types.ObjectType{AttrTypes: guestAccessWechatAttrTypes},
		"wechat_enabled":         types.BoolType,
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
			"facebook": schema.SingleNestedAttribute{
				MarkdownDescription: "Facebook authentication settings (enable with `facebook_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"app_id": schema.StringAttribute{
						MarkdownDescription: "Facebook application ID.",
						Optional:            true,
						Computed:            true,
					},
					"app_secret": schema.StringAttribute{
						MarkdownDescription: "Facebook application secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"scope_email": schema.BoolAttribute{
						MarkdownDescription: "Request the email scope.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"facebook_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Facebook authentication.",
				Optional:            true,
				Computed:            true,
			},
			"facebook_wifi": schema.SingleNestedAttribute{
				MarkdownDescription: "Facebook WiFi settings (used with `auth = \"facebook_wifi\"`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"block_https": schema.BoolAttribute{
						MarkdownDescription: "Block HTTPS traffic before authentication.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_id": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway ID.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_name": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway name.",
						Optional:            true,
						Computed:            true,
					},
					"gateway_secret": schema.StringAttribute{
						MarkdownDescription: "Facebook WiFi gateway secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
				},
			},
			"google": schema.SingleNestedAttribute{
				MarkdownDescription: "Google authentication settings (enable with `google_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"client_id": schema.StringAttribute{
						MarkdownDescription: "Google OAuth client ID.",
						Optional:            true,
						Computed:            true,
					},
					"client_secret": schema.StringAttribute{
						MarkdownDescription: "Google OAuth client secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"domain": schema.StringAttribute{
						MarkdownDescription: "Restrict Google authentication to a domain.",
						Optional:            true,
						Computed:            true,
					},
					"scope_email": schema.BoolAttribute{
						MarkdownDescription: "Request the email scope.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"google_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable Google authentication.",
				Optional:            true,
				Computed:            true,
			},
			"password": schema.StringAttribute{
				MarkdownDescription: "Guest portal password (used with `password_enabled`).",
				Optional:            true,
				Computed:            true,
				Sensitive:           true,
			},
			"password_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable simple password authentication.",
				Optional:            true,
				Computed:            true,
			},
			"radius": schema.SingleNestedAttribute{
				MarkdownDescription: "RADIUS authentication settings (enable with `radius_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"auth_type": schema.StringAttribute{
						MarkdownDescription: "RADIUS auth type: `chap` or `mschapv2`.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.String{
							stringvalidator.OneOf("chap", "mschapv2"),
						},
					},
					"disconnect_enabled": schema.BoolAttribute{
						MarkdownDescription: "Enable RADIUS disconnect messages.",
						Optional:            true,
						Computed:            true,
					},
					"disconnect_port": schema.Int64Attribute{
						MarkdownDescription: "RADIUS disconnect port.",
						Optional:            true,
						Computed:            true,
						Validators: []validator.Int64{
							int64validator.Between(1, 65535),
						},
					},
					"profile_id": schema.StringAttribute{
						MarkdownDescription: "RADIUS profile ID.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"radius_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable RADIUS authentication.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_enabled": schema.BoolAttribute{
				MarkdownDescription: "Restrict guest DNS to `restricted_dns_servers`.",
				Optional:            true,
				Computed:            true,
			},
			"restricted_dns_servers": schema.ListAttribute{
				MarkdownDescription: "Allowed DNS servers for guests, in priority order.",
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"wechat": schema.SingleNestedAttribute{
				MarkdownDescription: "WeChat authentication settings (enable with `wechat_enabled`).",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"app_id": schema.StringAttribute{
						MarkdownDescription: "WeChat app ID.",
						Optional:            true,
						Computed:            true,
					},
					"app_secret": schema.StringAttribute{
						MarkdownDescription: "WeChat app secret.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"secret_key": schema.StringAttribute{
						MarkdownDescription: "WeChat secret key.",
						Optional:            true,
						Computed:            true,
						Sensitive:           true,
					},
					"shop_id": schema.StringAttribute{
						MarkdownDescription: "WeChat shop ID.",
						Optional:            true,
						Computed:            true,
					},
				},
			},
			"wechat_enabled": schema.BoolAttribute{
				MarkdownDescription: "Enable WeChat authentication.",
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

// set installs a freshly-read guest_access object, carrying the six secret
// fields forward from the prior state/plan when the controller didn't echo
// them back (the x_-prefixed fields are often write-only). Evaluation-order
// note: this must run before m.GuestAccess is overwritten — readSections
// calls s.get(m) to capture prior *before* calling s.set(m, value), so
// m.GuestAccess here is still the pre-read value when preserve reads it.
func (guestAccessSection) set(m *settingResourceModel, obj types.Object) {
	m.GuestAccess = preserveGuestAccessSecrets(m.GuestAccess, obj)
}

// preserveGuestAccessSecrets returns fresh with each of the six write-only
// secret fields carried over from prior (the plan or prior state) wherever
// the freshly-read value is null but prior is non-null. If the controller DID
// echo a secret, the fresh (echoed) value wins — this only fills gaps left by
// non-echoed reads, mirroring preserveSnmpPassword in
// setting_section_snmp.go. Three fields live on the top-level object
// (password, and the flattened nested blocks below); three live inside
// nested objects (facebook.app_secret, facebook_wifi.gateway_secret,
// google.client_secret, wechat.app_secret, wechat.secret_key) and require
// descending into those objects to carry the value forward.
func preserveGuestAccessSecrets(prior, fresh types.Object) types.Object {
	if prior.IsNull() || prior.IsUnknown() || fresh.IsNull() || fresh.IsUnknown() {
		return fresh
	}

	attrs := make(map[string]attr.Value, len(fresh.Attributes()))
	for k, v := range fresh.Attributes() {
		attrs[k] = v
	}

	attrs["password"] = preserveStringAttr(prior, fresh, "password")
	attrs["facebook"] = preserveNestedSecretAttr(
		prior, fresh, "facebook", guestAccessFacebookAttrTypes, "app_secret",
	)
	attrs["facebook_wifi"] = preserveNestedSecretAttr(
		prior, fresh, "facebook_wifi", guestAccessFacebookWifiAttrTypes, "gateway_secret",
	)
	attrs["google"] = preserveNestedSecretAttr(
		prior, fresh, "google", guestAccessGoogleAttrTypes, "client_secret",
	)
	attrs["wechat"] = preserveNestedSecretAttr(
		prior, fresh, "wechat", guestAccessWechatAttrTypes, "app_secret", "secret_key",
	)

	merged, d := types.ObjectValue(guestAccessAttrTypes, attrs)
	if d.HasError() {
		return fresh
	}
	return merged
}

// preserveStringAttr returns the value of attrName on fresh, unless it is
// null and prior's value is non-null, in which case prior's value is
// carried forward.
func preserveStringAttr(prior, fresh types.Object, attrName string) attr.Value {
	fv, ok := fresh.Attributes()[attrName]
	if !ok {
		return fresh.Attributes()[attrName]
	}
	fs, ok := fv.(types.String)
	if !ok || !fs.IsNull() {
		return fv
	}
	pv, ok := prior.Attributes()[attrName]
	if !ok || pv.IsNull() || pv.IsUnknown() {
		return fv
	}
	return pv
}

// preserveNestedSecretAttr descends into the nested object attribute
// blockName on both prior and fresh and carries forward each named secret
// field (secretNames) that is null on fresh but non-null on prior. Returns
// the (possibly merged) nested object to install back on the parent. If
// either side's block is null/unknown, fresh's block is returned unchanged —
// there is nothing to preserve or merge into.
func preserveNestedSecretAttr(
	prior, fresh types.Object,
	blockName string,
	blockAttrTypes map[string]attr.Type,
	secretNames ...string,
) attr.Value {
	freshBlockVal, ok := fresh.Attributes()[blockName]
	if !ok {
		return freshBlockVal
	}
	freshBlock, ok := freshBlockVal.(types.Object)
	if !ok || freshBlock.IsNull() || freshBlock.IsUnknown() {
		return freshBlockVal
	}
	priorBlockVal, ok := prior.Attributes()[blockName]
	if !ok {
		return freshBlockVal
	}
	priorBlock, ok := priorBlockVal.(types.Object)
	if !ok || priorBlock.IsNull() || priorBlock.IsUnknown() {
		return freshBlockVal
	}

	blockAttrs := make(map[string]attr.Value, len(freshBlock.Attributes()))
	for k, v := range freshBlock.Attributes() {
		blockAttrs[k] = v
	}
	for _, secretName := range secretNames {
		blockAttrs[secretName] = preserveStringAttr(priorBlock, freshBlock, secretName)
	}
	merged, d := types.ObjectValue(blockAttrTypes, blockAttrs)
	if d.HasError() {
		return freshBlockVal
	}
	return merged
}

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

	setRawString(data, "x_password", m.Password)
	setRawBool(data, "password_enabled", m.PasswordEnabled)
	setRawBool(data, "facebook_enabled", m.FacebookEnabled)
	setRawBool(data, "google_enabled", m.GoogleEnabled)
	setRawBool(data, "radius_enabled", m.RadiusEnabled)
	setRawBool(data, "wechat_enabled", m.WechatEnabled)
	setRawBool(data, "restricted_dns_enabled", m.RestrictedDNSEnabled)
	if !m.RestrictedDNSServers.IsNull() && !m.RestrictedDNSServers.IsUnknown() {
		var servers []string
		diags.Append(m.RestrictedDNSServers.ElementsAs(ctx, &servers, false)...)
		data["restricted_dns_servers"] = servers
	}

	if !m.Facebook.IsNull() && !m.Facebook.IsUnknown() {
		var fb settingGuestAccessFacebookModel
		diags.Append(m.Facebook.As(ctx, &fb, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "facebook_app_id", fb.AppID)
		setRawString(data, "x_facebook_app_secret", fb.AppSecret)
		setRawBool(data, "facebook_scope_email", fb.ScopeEmail)
	}
	if !m.FacebookWifi.IsNull() && !m.FacebookWifi.IsUnknown() {
		var fw settingGuestAccessFacebookWifiModel
		diags.Append(m.FacebookWifi.As(ctx, &fw, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawBool(data, "facebook_wifi_block_https", fw.BlockHttps)
		setRawString(data, "facebook_wifi_gw_id", fw.GatewayID)
		setRawString(data, "facebook_wifi_gw_name", fw.GatewayName)
		setRawString(data, "x_facebook_wifi_gw_secret", fw.GatewaySecret)
	}
	if !m.Google.IsNull() && !m.Google.IsUnknown() {
		var g settingGuestAccessGoogleModel
		diags.Append(m.Google.As(ctx, &g, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "google_client_id", g.ClientID)
		setRawString(data, "x_google_client_secret", g.ClientSecret)
		setRawString(data, "google_domain", g.Domain)
		setRawBool(data, "google_scope_email", g.ScopeEmail)
	}
	if !m.Radius.IsNull() && !m.Radius.IsUnknown() {
		var r settingGuestAccessRadiusModel
		diags.Append(m.Radius.As(ctx, &r, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "radius_auth_type", r.AuthType)
		setRawBool(data, "radius_disconnect_enabled", r.DisconnectEnabled)
		setRawInt(data, "radius_disconnect_port", r.DisconnectPort)
		setRawString(data, "radiusprofile_id", r.ProfileID)
	}
	if !m.Wechat.IsNull() && !m.Wechat.IsUnknown() {
		var w settingGuestAccessWechatModel
		diags.Append(m.Wechat.As(ctx, &w, basetypes.ObjectAsOptions{})...)
		if diags.HasError() {
			return
		}
		setRawString(data, "wechat_app_id", w.AppID)
		setRawString(data, "x_wechat_app_secret", w.AppSecret)
		setRawString(data, "x_wechat_secret_key", w.SecretKey)
		setRawString(data, "wechat_shop_id", w.ShopID)
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

		Password:             rawString(data, "x_password"),
		PasswordEnabled:      rawBool(data, "password_enabled"),
		FacebookEnabled:      rawBool(data, "facebook_enabled"),
		GoogleEnabled:        rawBool(data, "google_enabled"),
		RadiusEnabled:        rawBool(data, "radius_enabled"),
		WechatEnabled:        rawBool(data, "wechat_enabled"),
		RestrictedDNSEnabled: rawBool(data, "restricted_dns_enabled"),
		RestrictedDNSServers: rawStringList(data, "restricted_dns_servers"),
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

	m.Facebook = types.ObjectNull(guestAccessFacebookAttrTypes)
	if anyRawKey(data, "facebook_app_id", "x_facebook_app_secret", "facebook_scope_email") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessFacebookAttrTypes,
			settingGuestAccessFacebookModel{
				AppID:      rawString(data, "facebook_app_id"),
				AppSecret:  rawString(data, "x_facebook_app_secret"),
				ScopeEmail: rawBool(data, "facebook_scope_email"),
			})
		diags.Append(d...)
		m.Facebook = obj
	}

	m.FacebookWifi = types.ObjectNull(guestAccessFacebookWifiAttrTypes)
	if anyRawKey(data, "facebook_wifi_gw_id", "facebook_wifi_gw_name",
		"x_facebook_wifi_gw_secret", "facebook_wifi_block_https") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessFacebookWifiAttrTypes,
			settingGuestAccessFacebookWifiModel{
				BlockHttps:    rawBool(data, "facebook_wifi_block_https"),
				GatewayID:     rawString(data, "facebook_wifi_gw_id"),
				GatewayName:   rawString(data, "facebook_wifi_gw_name"),
				GatewaySecret: rawString(data, "x_facebook_wifi_gw_secret"),
			})
		diags.Append(d...)
		m.FacebookWifi = obj
	}

	m.Google = types.ObjectNull(guestAccessGoogleAttrTypes)
	if anyRawKey(data, "google_client_id", "x_google_client_secret",
		"google_domain", "google_scope_email") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessGoogleAttrTypes,
			settingGuestAccessGoogleModel{
				ClientID:     rawString(data, "google_client_id"),
				ClientSecret: rawString(data, "x_google_client_secret"),
				Domain:       rawString(data, "google_domain"),
				ScopeEmail:   rawBool(data, "google_scope_email"),
			})
		diags.Append(d...)
		m.Google = obj
	}

	m.Radius = types.ObjectNull(guestAccessRadiusAttrTypes)
	if anyRawKey(data, "radius_auth_type", "radiusprofile_id",
		"radius_disconnect_enabled", "radius_disconnect_port") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessRadiusAttrTypes,
			settingGuestAccessRadiusModel{
				AuthType:          rawString(data, "radius_auth_type"),
				DisconnectEnabled: rawBool(data, "radius_disconnect_enabled"),
				DisconnectPort:    rawInt(data, "radius_disconnect_port"),
				ProfileID:         rawString(data, "radiusprofile_id"),
			})
		diags.Append(d...)
		m.Radius = obj
	}

	m.Wechat = types.ObjectNull(guestAccessWechatAttrTypes)
	if anyRawKey(data, "wechat_app_id", "x_wechat_app_secret",
		"x_wechat_secret_key", "wechat_shop_id") {
		obj, d := types.ObjectValueFrom(ctx, guestAccessWechatAttrTypes,
			settingGuestAccessWechatModel{
				AppID:     rawString(data, "wechat_app_id"),
				AppSecret: rawString(data, "x_wechat_app_secret"),
				SecretKey: rawString(data, "x_wechat_secret_key"),
				ShopID:    rawString(data, "wechat_shop_id"),
			})
		diags.Append(d...)
		m.Wechat = obj
	}

	return m
}
