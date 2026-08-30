package unifi

// The guest_access section descriptor: an unconditional-mirror hydration
// whose only specials are the #303 write-side OmitZero guards on
// expire_number, expire_unit and radius_disconnect_port -- the reason
// lcm's brightness/idle_timeout and syslog's port/netconsole_port carry the
// same pair (see setting_lcm_descriptor.go, setting_syslog_descriptor.go).
// Replaces nothing hand-written: guest_access never had a legacy
// writeGuestAccessSection / readGuestAccessSection, so this is new rather
// than a migration. See setting_mgmt_descriptor.go for the shape every
// section descriptor follows.
//
// This is Task 2 of a five-task rollout (.superpowers/sdd/plan-r2b-guest-access):
// the 21 core scalars named by this task's own brief -- portal access and
// mode, post-auth redirect, session and voucher expiry, the RADIUS
// guest-auth group without secrets, password_enabled without its secret,
// voucher_enabled, payment_enabled, gateway and ec_enabled. settings.GuestAccess
// carries 92 fields total (unifi/setting_guest_access_fieldsplit.go); the other
// 71 -- the 18 x_-prefixed secrets (Task 3), the four subnet/DNS-scoping
// fields (Task 4) and the 36-field portal-appearance group (Task 5) -- are
// named in provider-codegen/policy/setting.json's top-level "omitted" list
// as "GuestAccess.<field>" for now, not as omitted members of this grouping:
// that keeps this dispatch's diff to exactly its own 21 fields, and lets each
// later task's diff be exactly "move its fields from omitted to managed"
// rather than a rewrite of this file's member list. See that policy file's
// "guest_access" grouping.
//
// Three fields carry a fact worth flagging rather than assuming. The plan's
// "Known risks" names a claim for ec_enabled inherited from an abandoned
// prior design -- that it is the guest portal's TLS crypto-mode flag -- but
// that is not what this file ships: the SDK's own field carries no
// comment, and "express checkout" (below) is this task's own inference
// from the field name and its position beside payment_enabled/gateway, not
// a controller-documented fact either. Since the description states it as
// settled where a practitioner would read and act on it, the description
// itself hedges too, not just this comment. radiusprofile_id is a
// cross-resource reference into unifi_radius_profile, unrelated to this
// resource's own "radius" section despite the shared name; the provider
// does not check the ID exists, matching every other cross-resource
// reference in this codebase. A third, smaller one: redirect_https and
// redirect_to_https are two distinct SDK fields with adjacent names and no
// controller documentation distinguishing them: this file assumes the
// former governs the post-auth redirect target and the latter forces the
// portal page itself to HTTPS, purely from the field names, and echoes
// that same assumption in each one's description.
//
// auth, expire, expire_unit, gateway, portal_hostname and radius_auth_type
// carry a validator the compiler derives from the SDK's own constraint
// table (SettingGuestAccess in go-unifi's settings/validation.generated.go)
// -- see internal/providercompiler/derive_validators.go. None of the six is
// hand-written in policy/setting.json; the compiler's own redundancy gate
// refuses generation if a hand validator duplicates a derived one.
// expire_number and radius_disconnect_port carry a constraint-table entry
// too (a pattern for the former, Min/Max bounds for the latter) but get no
// derived validator: the deriver only emits int64validator.OneOf from an
// enumerated value set, never int64validator.Between from bounds, and never
// translates a pattern onto an Int64Attribute at all (regex derivation is
// string-only). That gap is a compiler limitation, not something this task
// works around -- both fields are still write-safe via OmitZero below,
// which is a wire-level guard independent of any schema-level validator.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingGuestAccessModel is guest_access's own section model, decoded out
// of settingResourceModel.GuestAccess. Named and ordered to match the
// generated schema's own attribute order (alphabetical by terraform_name),
// the same convention every other section descriptor in this package
// follows.
type settingGuestAccessModel struct {
	Auth                    types.String `tfsdk:"auth"`
	EcEnabled               types.Bool   `tfsdk:"ec_enabled"`
	Expire                  types.String `tfsdk:"expire"`
	ExpireNumber            types.Int64  `tfsdk:"expire_number"`
	ExpireUnit              types.Int64  `tfsdk:"expire_unit"`
	Gateway                 types.String `tfsdk:"gateway"`
	PasswordEnabled         types.Bool   `tfsdk:"password_enabled"`
	PaymentEnabled          types.Bool   `tfsdk:"payment_enabled"`
	PortalEnabled           types.Bool   `tfsdk:"portal_enabled"`
	PortalHostname          types.String `tfsdk:"portal_hostname"`
	PortalUseHostname       types.Bool   `tfsdk:"portal_use_hostname"`
	RADIUSAuthType          types.String `tfsdk:"radius_auth_type"`
	RADIUSDisconnectEnabled types.Bool   `tfsdk:"radius_disconnect_enabled"`
	RADIUSDisconnectPort    types.Int64  `tfsdk:"radius_disconnect_port"`
	RADIUSEnabled           types.Bool   `tfsdk:"radius_enabled"`
	RADIUSProfileID         types.String `tfsdk:"radiusprofile_id"`
	RedirectEnabled         types.Bool   `tfsdk:"redirect_enabled"`
	RedirectHttps           types.Bool   `tfsdk:"redirect_https"`
	RedirectToHttps         types.Bool   `tfsdk:"redirect_to_https"`
	RedirectUrl             types.String `tfsdk:"redirect_url"`
	VoucherEnabled          types.Bool   `tfsdk:"voucher_enabled"`
}

// guestAccessAttrTypes types guest_access's own object in state; it must
// match the generated schema exactly.
var guestAccessAttrTypes = map[string]attr.Type{
	"auth":                      types.StringType,
	"ec_enabled":                types.BoolType,
	"expire":                    types.StringType,
	"expire_number":             types.Int64Type,
	"expire_unit":               types.Int64Type,
	"gateway":                   types.StringType,
	"password_enabled":          types.BoolType,
	"payment_enabled":           types.BoolType,
	"portal_enabled":            types.BoolType,
	"portal_hostname":           types.StringType,
	"portal_use_hostname":       types.BoolType,
	"radius_auth_type":          types.StringType,
	"radius_disconnect_enabled": types.BoolType,
	"radius_disconnect_port":    types.Int64Type,
	"radius_enabled":            types.BoolType,
	"radiusprofile_id":          types.StringType,
	"redirect_enabled":          types.BoolType,
	"redirect_https":            types.BoolType,
	"redirect_to_https":         types.BoolType,
	"redirect_url":              types.StringType,
	"voucher_enabled":           types.BoolType,
}

// guestAccessKitSpec maps this task's 21 attributes of the generated
// guest_access schema (resource_setting/setting_resource_gen.go's
// "guest_access" SingleNestedAttribute) onto settings.GuestAccess. Elide
// judgments follow resourcekit.ElideProblems' schema-driven rule: every
// string field below is Optional+Computed, and ElideProblems' zeroIsRejected
// runs each attribute's own validators against "" to decide -- auth,
// expire, gateway and radius_auth_type each carry a derived OneOf/RegexMatches
// that rejects "", so they want NullZero; portal_hostname's derived pattern
// (^[a-zA-Z0-9.-]+$|^$) explicitly admits "" via its own alternation, and
// radiusprofile_id and redirect_url carry no validator at all, so all three
// want KeepZero. Every bool field carries no Elide at all, matching
// resourcekit's own elideExempt (a false is a value, not an absence).
// expire_number, expire_unit and radius_disconnect_port are Optional+Computed
// Int64 attributes, and zeroIsRejected only ever inspects a StringAttribute's
// validators (an Int64 range or pattern constraint can't drive it), so
// KeepZero is what the check demands for all three -- matching lcm's
// brightness/idle_timeout and syslog's port/netconsole_port. OmitZero is the
// separate, write-side #303 guard: an unknown (unset Optional+Computed)
// value's ValueInt64Pointer() resolves to a pointer to zero, which the
// controller's own validator rejects for all three (expire_number requires a
// leading 1-9 or exactly 1000000, expire_unit is the enum 1/60/1440,
// radius_disconnect_port has a minimum of 1), so a zero must never reach the
// wire.
func guestAccessKitSpec() resourcekit.Spec[settingGuestAccessModel, settings.GuestAccess] {
	return resourcekit.Spec[settingGuestAccessModel, settings.GuestAccess]{
		TypeName: "setting_guest_access",
		Subject:  "Guest Access Setting",
		New:      func() *settings.GuestAccess { return &settings.GuestAccess{} },
		Fields: []resourcekit.Field[settingGuestAccessModel, settings.GuestAccess]{
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "auth",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Auth },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Auth },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "ec_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.EcEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.EcEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "expire",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Expire },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Expire },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "expire_number",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.ExpireNumber },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.ExpireNumber },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "expire_unit",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.ExpireUnit },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.ExpireUnit },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "gateway",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.Gateway },
				SDK:   func(s *settings.GuestAccess) *string { return &s.Gateway },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "password_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PasswordEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PasswordEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "payment_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PaymentEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PaymentEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PortalEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PortalEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_hostname",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.PortalHostname },
				SDK:   func(s *settings.GuestAccess) *string { return &s.PortalHostname },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "portal_use_hostname",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.PortalUseHostname },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.PortalUseHostname },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_auth_type",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RADIUSAuthType },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RADIUSAuthType },
				Elide: resourcekit.NullZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_disconnect_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RADIUSDisconnectEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RADIUSDisconnectEnabled },
			},
			resourcekit.Int64PtrField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:     "radius_disconnect_port",
				Model:    func(m *settingGuestAccessModel) *types.Int64 { return &m.RADIUSDisconnectPort },
				SDK:      func(s *settings.GuestAccess) **int64 { return &s.RADIUSDisconnectPort },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radius_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RADIUSEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RADIUSEnabled },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "radiusprofile_id",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RADIUSProfileID },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RADIUSProfileID },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectEnabled },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_https",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectHttps },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectHttps },
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_to_https",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.RedirectToHttps },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.RedirectToHttps },
			},
			resourcekit.StringField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "redirect_url",
				Model: func(m *settingGuestAccessModel) *types.String { return &m.RedirectUrl },
				SDK:   func(s *settings.GuestAccess) *string { return &s.RedirectUrl },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.BoolField[settingGuestAccessModel, settings.GuestAccess]{
				Wire:  "voucher_enabled",
				Model: func(m *settingGuestAccessModel) *types.Bool { return &m.VoucherEnabled },
				SDK:   func(s *settings.GuestAccess) *bool { return &s.VoucherEnabled },
			},
		},
	}
}

// guestAccessNestedSchema is the guest_access SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance checks
// -- built for a whole resource's top-level schema -- can run against one
// section of unifi_setting instead.
func guestAccessNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	guestAccess := built.Attributes["guest_access"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // guest_access is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: guestAccess.Attributes}
}

// guestAccessKitBackend binds guestAccessKitSpec to a client: Read is
// GetSetting[*GuestAccess], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set instead of a read-modify-write
// whole-document PUT.
func guestAccessKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.GuestAccess] {
	return resourcekit.Backend[settings.GuestAccess]{
		Read: func(ctx context.Context, site, _ string) (*settings.GuestAccess, error) {
			_, guestAccess, err := ui.GetSetting[*settings.GuestAccess](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return guestAccess, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.GuestAccess, fields ...string,
		) (*settings.GuestAccess, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// guestAccessKitSection builds the guest_access entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func guestAccessKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := guestAccessKitSpec()
	spec.Backend = guestAccessKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingGuestAccessModel, settings.GuestAccess]{
		SectionName: "guest_access",
		Get:         func(m *settingResourceModel) *types.Object { return &m.GuestAccess },
		Set:         func(m *settingResourceModel, o types.Object) { m.GuestAccess = o },
		AttrTypes:   guestAccessAttrTypes,
		Spec:        spec,
	}
}
