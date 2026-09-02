package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dhcp_option"
	resource_dhcp_option "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dhcp_option"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type dhcpOptionKitModel struct {
	ID       types.String   `tfsdk:"id"`
	Site     types.String   `tfsdk:"site"`
	Code     types.String   `tfsdk:"code"`
	Name     types.String   `tfsdk:"name"`
	Signed   types.Bool     `tfsdk:"signed"`
	Type     types.String   `tfsdk:"type"`
	Width    types.Int64    `tfsdk:"width"`
	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

func dhcpOptionKitSpec() resourcekit.Spec[dhcpOptionKitModel, ui.DHCPOption] {
	return resourcekit.Spec[dhcpOptionKitModel, ui.DHCPOption]{
		TypeName: "dhcp_option",
		Subject:  "DHCP Option",
		IDWire:   "_id",
		New:      func() *ui.DHCPOption { return &ui.DHCPOption{} },
		ID:       func(m *dhcpOptionKitModel) *types.String { return &m.ID },
		Site:     func(m *dhcpOptionKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dhcpOptionKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[dhcpOptionKitModel, ui.DHCPOption]{
			// The controller stores the code as a string; the SDK follows it,
			// so this does too rather than inventing a numeric attribute the
			// wire would immediately re-quote.
			resourcekit.StringField[dhcpOptionKitModel, ui.DHCPOption]{
				Wire:  "code",
				Model: func(m *dhcpOptionKitModel) *types.String { return &m.Code },
				SDK:   func(s *ui.DHCPOption) *string { return &s.Code },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[dhcpOptionKitModel, ui.DHCPOption]{
				Wire:  "name",
				Model: func(m *dhcpOptionKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.DHCPOption) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// signed is the SDK's one field without omitempty, so a create
			// always sends it; the schema default (false) makes the value the
			// provider's own, and a false read back stays false rather than
			// turning into an absence.
			resourcekit.BoolField[dhcpOptionKitModel, ui.DHCPOption]{
				Wire:  "signed",
				Model: func(m *dhcpOptionKitModel) *types.Bool { return &m.Signed },
				SDK:   func(s *ui.DHCPOption) *bool { return &s.Signed },
			},
			resourcekit.StringField[dhcpOptionKitModel, ui.DHCPOption]{
				Wire:  "type",
				Model: func(m *dhcpOptionKitModel) *types.String { return &m.Type },
				SDK:   func(s *ui.DHCPOption) *string { return &s.Type },
				Elide: resourcekit.KeepZero,
			},
			// Pointer int64, and the bootstrap is what says so: width is the
			// one field of the eleven marked pointer. Its legal values are 8,
			// 16 and 32, so a zero can only mean "the controller did not say".
			resourcekit.Int64PtrField[dhcpOptionKitModel, ui.DHCPOption]{
				Wire:  "width",
				Model: func(m *dhcpOptionKitModel) *types.Int64 { return &m.Width },
				SDK:   func(s *ui.DHCPOption) **int64 { return &s.Width },
				Elide: resourcekit.NullZero,
				// The controller's own pattern (^(8|16|32)$) rejects a zero,
				// so an unset width must be omitted, never sent as 0.
				OmitZero: true,
			},
		},
		// Seeded here as well as in dhcpOptionKitBackend, because Configure
		// binds the real Backend and a unit test calling ToModel on an
		// unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.DHCPOption]{
			GetID: func(s *ui.DHCPOption) string { return s.ID },
			SetID: func(s *ui.DHCPOption, id string) { s.ID = id },
		},
	}
}

func dhcpOptionKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_dhcp_option.DhcpOptionResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func dhcpOptionKitList() resourcekit.ListSpec[ui.DHCPOption] {
	return resourcekit.ListSpec[ui.DHCPOption]{
		ConfigSchema: listresource_dhcp_option.DhcpOptionListResourceSchema,
		DisplayName: func(s *ui.DHCPOption) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.DHCPOption) string{
			"name": func(s *ui.DHCPOption) string { return s.Name },
		},
	}
}

func dhcpOptionKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.DHCPOption] {
	return resourcekit.Backend[ui.DHCPOption]{
		Create: func(ctx context.Context, site string, in *ui.DHCPOption) (*ui.DHCPOption, error) {
			return client.CreateDHCPOption(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.DHCPOption, error) {
			return client.GetDHCPOption(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.DHCPOption, fields ...string,
		) (*ui.DHCPOption, error) {
			return client.UpdateDHCPOptionFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteDHCPOption(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.DHCPOption, error) {
			return client.ListDHCPOption(ctx, site)
		},
		GetID: func(s *ui.DHCPOption) string { return s.ID },
		SetID: func(s *ui.DHCPOption, id string) { s.ID = id },
	}
}
