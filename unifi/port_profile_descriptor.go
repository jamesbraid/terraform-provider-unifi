package unifi

// port_profile's tagged VLAN membership translation (inclusion list <->
// mode + exclusion list) lives in port_profile_resource.go's BeforeSend and
// AfterReceive hooks; this file only wires them up.

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_port_profile"
	resource_port_profile "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_port_profile"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

type ppModel = portProfileKitModel

type ppSDK = ui.PortProfile

func ppString(
	wire string,
	model func(*ppModel) *types.String,
	sdk func(*ppSDK) *string,
	elide resourcekit.ElideZero,
) resourcekit.StringField[ppModel, ppSDK] {
	return resourcekit.StringField[ppModel, ppSDK]{Wire: wire, Model: model, SDK: sdk, Elide: elide}
}

func portProfileKitSpec() resourcekit.Spec[ppModel, ppSDK] {
	return resourcekit.Spec[ppModel, ppSDK]{
		TypeName: "port_profile",
		Subject:  "Port Profile",
		New:      func() *ppSDK { return &ppSDK{} },
		ID:       func(m *ppModel) *types.String { return &m.ID },
		Site:     func(m *ppModel) *types.String { return &m.Site },
		Timeouts: func(m *ppModel) *timeouts.Value { return &m.Timeouts },

		// Prefetch is bound in Configure, where the client exists.
		BeforeSend:   portProfileBeforeSend,
		AfterReceive: portProfileAfterReceive,

		// The three wire fields BeforeSend derives from tagged_networkconf_ids,
		// which is not itself a field and so cannot put them in the mask.
		AlwaysWire: []string{"tagged_vlan_mgmt", "excluded_networkconf_ids", "forward"},

		// tagged_vlan_mgmt is a strict enum with no valid empty value, but
		// AlwaysWire forces it onto every masked update even when nothing
		// about tagged-VLAN management is configured, resolving to "". Naming
		// it here when empty keeps the masked write from asserting that ""
		// explicitly -- which the controller rejects with a 400 -- restoring
		// what a full-object write always did: the key is left off the wire
		// and the controller's stored value is untouched.
		UnwritableWires: func(sdk *ppSDK) []string {
			if sdk.TaggedVLANMgmt == "" {
				return []string{"tagged_vlan_mgmt"}
			}
			return nil
		},

		Fields: resourcekit.Override(portProfileGenFields(), []resourcekit.Field[portProfileKitModel, ppSDK]{
			// "force_authorized" on an empty read.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "dot1x_ctrl",
				Model:       func(m *ppModel) *types.String { return &m.Dot1xCtrl },
				SDK:         func(s *ppSDK) *string { return &s.Dot1XCtrl },
				ReadDefault: "force_authorized",
			},
			resourcekit.DurationPtrField[ppModel, ppSDK]{
				Wire:  "dot1x_idle_timeout",
				Model: func(m *ppModel) *timetypes.GoDuration { return &m.Dot1xIdleTimeout },
				SDK:   func(s *ppSDK) **int64 { return &s.Dot1XIDleTimeout },
				Units: time.Second,
				Elide: resourcekit.KeepZero,
			},
			// "native" on an empty read, and BeforeSend overrides it whenever a
			// VLAN mode is derived.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "forward",
				Model:       func(m *ppModel) *types.String { return &m.Forward },
				SDK:         func(s *ppSDK) *string { return &s.Forward },
				ReadDefault: "native",
			},
			// "switch" on an empty read.
			resourcekit.StringField[ppModel, ppSDK]{
				Wire:        "op_mode",
				Model:       func(m *ppModel) *types.String { return &m.OpMode },
				SDK:         func(s *ppSDK) *string { return &s.OpMode },
				ReadDefault: "switch",
			},
			ppString(
				"tagged_vlan_mgmt",
				func(m *ppModel) *types.String { return &m.TaggedVLANMgmt },
				func(s *ppSDK) *string { return &s.TaggedVLANMgmt },
				resourcekit.NullZero,
			),
		}),
		// Seeded here as well as in portProfileKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ppSDK]{
			GetID: func(s *ppSDK) string { return s.ID },
			SetID: func(s *ppSDK, id string) { s.ID = id },
		},
	}
}

func portProfileKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_port_profile.PortProfileResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Version:  1,
	}
}

func portProfileKitList() resourcekit.ListSpec[ppSDK] {
	return resourcekit.ListSpec[ppSDK]{
		ConfigSchema: listresource_port_profile.PortProfileListResourceSchema,
		DisplayName: func(s *ppSDK) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ppSDK) string{
			"name": func(s *ppSDK) string { return s.Name },
		},
	}
}

func portProfileKitBackend(client *ui.ApiClient) resourcekit.Backend[ppSDK] {
	return resourcekit.Backend[ppSDK]{
		Create: func(ctx context.Context, site string, in *ppSDK) (*ppSDK, error) {
			return client.CreatePortProfile(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ppSDK, error) {
			return client.GetPortProfile(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ppSDK, fields ...string) (*ppSDK, error) {
			return client.UpdatePortProfileFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeletePortProfile(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ppSDK, error) {
			return client.ListPortProfile(ctx, site)
		},
		GetID: func(s *ppSDK) string { return s.ID },
		SetID: func(s *ppSDK, id string) { s.ID = id },
	}
}
