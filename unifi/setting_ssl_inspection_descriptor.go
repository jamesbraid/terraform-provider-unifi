package unifi

// The ssl_inspection section descriptor: an unconditional-mirror hydration
// with no specials, shaped exactly like setting_country_descriptor.go. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.

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

// settingSslInspectionModel is ssl_inspection's own section model, decoded
// out of settingResourceModel.SslInspection.
type settingSslInspectionModel struct {
	State types.String `tfsdk:"state"`
}

// sslInspectionAttrTypes types ssl_inspection's own object in state; it
// must match the generated schema exactly.
var sslInspectionAttrTypes = map[string]attr.Type{
	"state": types.StringType,
}

// sslInspectionKitSpec maps the one attribute of the generated
// ssl_inspection schema (resource_setting/setting_resource_gen.go's
// "ssl_inspection" SingleNestedAttribute) onto settings.SslInspection.
// state carries a OneOf("off", "simple", "advanced") validator that
// rejects "", so resourcekit.ElideProblems' schema-driven rule demands
// NullZero.
func sslInspectionKitSpec() resourcekit.Spec[settingSslInspectionModel, settings.SslInspection] {
	return resourcekit.Spec[settingSslInspectionModel, settings.SslInspection]{
		TypeName: "setting_ssl_inspection",
		Subject:  "SSL Inspection Setting",
		New:      func() *settings.SslInspection { return &settings.SslInspection{} },
		Fields: []resourcekit.Field[settingSslInspectionModel, settings.SslInspection]{
			resourcekit.StringField[settingSslInspectionModel, settings.SslInspection]{
				Wire:  "state",
				Model: func(m *settingSslInspectionModel) *types.String { return &m.State },
				SDK:   func(s *settings.SslInspection) *string { return &s.State },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// sslInspectionNestedSchema is the ssl_inspection SingleNestedAttribute's
// own Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func sslInspectionNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	sslInspection := built.Attributes["ssl_inspection"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // ssl_inspection is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: sslInspection.Attributes}
}

// sslInspectionKitBackend binds sslInspectionKitSpec to a client: Read is
// GetSetting[*SslInspection], UpdateFields is the masked
// UpdateSettingFields -- naming only the fields the plan set.
func sslInspectionKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.SslInspection] {
	return resourcekit.Backend[settings.SslInspection]{
		Read: func(ctx context.Context, site, _ string) (*settings.SslInspection, error) {
			_, sslInspection, err := ui.GetSetting[*settings.SslInspection](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return sslInspection, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.SslInspection, fields ...string,
		) (*settings.SslInspection, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// sslInspectionKitSection builds the ssl_inspection entry for
// settingResource's Sections, bound to client via settingKitSections,
// which calls it with r.client.ApiClient.
func sslInspectionKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := sslInspectionKitSpec()
	spec.Backend = sslInspectionKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingSslInspectionModel, settings.SslInspection]{
		SectionName: "ssl_inspection",
		Get:         func(m *settingResourceModel) *types.Object { return &m.SslInspection },
		Set:         func(m *settingResourceModel, o types.Object) { m.SslInspection = o },
		AttrTypes:   sslInspectionAttrTypes,
		Spec:        spec,
	}
}
