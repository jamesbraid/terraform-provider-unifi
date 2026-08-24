package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-nettypes/hwtypes"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_ap_group"
	resource_ap_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_ap_group"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/planmodifiers"
)

type apGroupKitModel struct {
	ID         types.String   `tfsdk:"id"`
	Site       types.String   `tfsdk:"site"`
	Name       types.String   `tfsdk:"name"`
	DeviceMacs types.Set      `tfsdk:"device_macs"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func apGroupKitSpec() resourcekit.Spec[apGroupKitModel, ui.APGroup] {
	return resourcekit.Spec[apGroupKitModel, ui.APGroup]{
		TypeName: "ap_group",
		Subject:  "AP Group",
		New:      func() *ui.APGroup { return &ui.APGroup{} },
		ID:       func(m *apGroupKitModel) *types.String { return &m.ID },
		Site:     func(m *apGroupKitModel) *types.String { return &m.Site },
		Timeouts: func(m *apGroupKitModel) *timeouts.Value { return &m.Timeouts },

		// Nothing declares for_wlanconf, and that is the whole of the fix: the
		// mask is derived from the list below, so a field absent from it has
		// no wire name and cannot be sent.
		Fields: []resourcekit.Field[apGroupKitModel, ui.APGroup]{
			resourcekit.StringField[apGroupKitModel, ui.APGroup]{
				Wire:  "name",
				Model: func(m *apGroupKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.APGroup) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringSetField[apGroupKitModel, ui.APGroup]{
				Wire:  "device_macs",
				Model: func(m *apGroupKitModel) *types.Set { return &m.DeviceMacs },
				SDK:   func(s *ui.APGroup) *[]string { return &s.DeviceMacs },
				// The schema types the elements as MAC addresses, so the set
				// built on the read path has to as well or the value does not
				// fit the schema.
				ElementType: hwtypes.MACAddressType{},
				// The element type alone isn't enough: MACAddressType gives each
				// element semantic equality, but a Set identifies members by
				// string value, so that never reaches the set. Overwriting a
				// practitioner's "AA-BB-.." with the controller's "aa:bb:.."
				// would leave an unsettleable diff.
				KeepPrior: planmodifiers.MACSetsEqual,
				// Optional and Computed, so an empty membership is a value the
				// practitioner may have asked for rather than an absence. The
				// controller accepts a group with no members.
				Elide: resourcekit.KeepZero,
			},
		},

		// The controller stores MACs lowercased and colon-separated. Normalising
		// on the write path is what makes the object it returns match what was
		// sent; without it every apply on a group written in another spelling
		// reports a change.
		BeforeSend: func(
			_ context.Context,
			_, _ *apGroupKitModel,
			sdk *ui.APGroup,
			_ any,
		) diag.Diagnostics {
			for i, mac := range sdk.DeviceMacs {
				sdk.DeviceMacs[i] = cleanMAC(mac)
			}
			return nil
		},

		// Seeded here as well as in apGroupKitBackend, because Configure binds
		// the real Backend and a unit test calling ToModel on an unconfigured
		// spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.APGroup]{
			GetID: func(s *ui.APGroup) string { return s.ID },
			SetID: func(s *ui.APGroup, id string) { s.ID = id },
		},
	}
}

func apGroupKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_ap_group.ApGroupResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func apGroupKitList() resourcekit.ListSpec[ui.APGroup] {
	return resourcekit.ListSpec[ui.APGroup]{
		ConfigSchema: listresource_ap_group.ApGroupListResourceSchema,
		DisplayName: func(s *ui.APGroup) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.APGroup) string{
			"name": func(s *ui.APGroup) string { return s.Name },
		},
	}
}

func apGroupKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.APGroup] {
	return resourcekit.Backend[ui.APGroup]{
		Create: func(ctx context.Context, site string, in *ui.APGroup) (*ui.APGroup, error) {
			return client.CreateAPGroup(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.APGroup, error) {
			return client.GetAPGroup(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.APGroup, fields ...string,
		) (*ui.APGroup, error) {
			return client.UpdateAPGroupFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteAPGroup(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.APGroup, error) {
			return client.ListAPGroup(ctx, site)
		},
		GetID: func(s *ui.APGroup) string { return s.ID },
		SetID: func(s *ui.APGroup, id string) { s.ID = id },
	}
}
