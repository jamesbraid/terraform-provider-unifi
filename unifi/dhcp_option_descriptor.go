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

func dhcpOptionKitSpec() resourcekit.Spec[dhcpOptionKitModel, ui.DHCPOption] {
	return resourcekit.Spec[dhcpOptionKitModel, ui.DHCPOption]{
		TypeName: "dhcp_option",
		Subject:  "DHCP Option",
		IDWire:   "_id",
		New:      func() *ui.DHCPOption { return &ui.DHCPOption{} },
		ID:       func(m *dhcpOptionKitModel) *types.String { return &m.ID },
		Site:     func(m *dhcpOptionKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dhcpOptionKitModel) *timeouts.Value { return &m.Timeouts },
		Fields:   dhcpOptionGenFields(),
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
