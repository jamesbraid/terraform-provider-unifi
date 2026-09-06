package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dynamic_dns"
	resource_dynamic_dns "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dynamic_dns"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dynamicDNSKitModel struct {
	ID        types.String   `tfsdk:"id"`
	Site      types.String   `tfsdk:"site"`
	HostName  types.String   `tfsdk:"host_name"`
	Interface types.String   `tfsdk:"interface"`
	Login     types.String   `tfsdk:"login"`
	Password  types.String   `tfsdk:"password"`
	Server    types.String   `tfsdk:"server"`
	Service   types.String   `tfsdk:"service"`
	Timeouts  timeouts.Value `tfsdk:"timeouts"`
}

func dynamicDNSKitSpec() resourcekit.Spec[dynamicDNSKitModel, ui.DynamicDNS] {
	return resourcekit.Spec[dynamicDNSKitModel, ui.DynamicDNS]{
		TypeName: "dynamic_dns",
		Subject:  "Dynamic DNS",
		IDWire:   "_id",
		New:      func() *ui.DynamicDNS { return &ui.DynamicDNS{} },
		ID:       func(m *dynamicDNSKitModel) *types.String { return &m.ID },
		Site:     func(m *dynamicDNSKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dynamicDNSKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[dynamicDNSKitModel, ui.DynamicDNS]{
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "host_name",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.HostName },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.HostName },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "interface",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.Interface },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.Interface },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "login",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.Login },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.Login },
				Elide: resourcekit.NullZero,
			},
			// The controller stores the password under x_password, but echoes
			// it back on read; NullZero keeps an omitted one null rather than
			// turning it into an empty string in a sensitive attribute.
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "x_password",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.Password },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.Password },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "server",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.Server },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.Server },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[dynamicDNSKitModel, ui.DynamicDNS]{
				Wire:  "service",
				Model: func(m *dynamicDNSKitModel) *types.String { return &m.Service },
				SDK:   func(s *ui.DynamicDNS) *string { return &s.Service },
				Elide: resourcekit.KeepZero,
			},
		},
		// Seeded here as well as in dynamicDNSKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.DynamicDNS]{
			GetID: func(s *ui.DynamicDNS) string { return s.ID },
			SetID: func(s *ui.DynamicDNS, id string) { s.ID = id },
		},
	}
}

func dynamicDNSKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_dynamic_dns.DynamicDnsResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func dynamicDNSKitList() resourcekit.ListSpec[ui.DynamicDNS] {
	return resourcekit.ListSpec[ui.DynamicDNS]{
		ConfigSchema: listresource_dynamic_dns.DynamicDnsListResourceSchema,
		DisplayName: func(s *ui.DynamicDNS) string {
			if s.HostName != "" {
				return s.HostName
			}
			return s.ID
		},
		Filters: map[string]func(*ui.DynamicDNS) string{
			"host_name": func(s *ui.DynamicDNS) string { return s.HostName },
			"service":   func(s *ui.DynamicDNS) string { return s.Service },
		},
	}
}

func dynamicDNSKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.DynamicDNS] {
	return resourcekit.Backend[ui.DynamicDNS]{
		Create: func(ctx context.Context, site string, in *ui.DynamicDNS) (*ui.DynamicDNS, error) {
			return client.CreateDynamicDNS(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.DynamicDNS, error) {
			return client.GetDynamicDNS(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.DynamicDNS, fields ...string,
		) (*ui.DynamicDNS, error) {
			return client.UpdateDynamicDNSFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteDynamicDNS(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.DynamicDNS, error) {
			return client.ListDynamicDNS(ctx, site)
		},
		GetID: func(s *ui.DynamicDNS) string { return s.ID },
		SetID: func(s *ui.DynamicDNS, id string) { s.ID = id },
	}
}
