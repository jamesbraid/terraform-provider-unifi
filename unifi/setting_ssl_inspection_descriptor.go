package unifi

// The ssl_inspection section descriptor: an unconditional-mirror hydration
// with no specials, shaped exactly like setting_country_descriptor.go. See
// setting_mgmt_descriptor.go for the shape every section descriptor
// follows.

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

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
		Fields:   settingSslInspectionGenFields(),
	}
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
