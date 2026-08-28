package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_client_qos_rate"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// clientQosRateKitModel is the generated model. The tfsdk tags are what the
// framework reflects on, and what ElideProblems follows to reach the schema.
type clientQosRateKitModel struct {
	ID             types.String   `tfsdk:"id"`
	Site           types.String   `tfsdk:"site"`
	Name           types.String   `tfsdk:"name"`
	QOSRateMaxDown types.Int64    `tfsdk:"qos_rate_max_down"`
	QOSRateMaxUp   types.Int64    `tfsdk:"qos_rate_max_up"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

// clientQosRateKitSpec is the whole of what varies. Elide is derived, not
// chosen: name is Required so its zero must survive, and the two rate fields
// are Optional+Computed so they're also KeepZero -- a practitioner may set an
// explicit zero, and nulling it would make state disagree with config.
// ElideProblems enforces that pairing.
//
// The rates are Int64Ptr because the SDK declares them *int64.
func clientQosRateKitSpec() resourcekit.Spec[clientQosRateKitModel, ui.ClientGroup] {
	return resourcekit.Spec[clientQosRateKitModel, ui.ClientGroup]{
		TypeName: "client_qos_rate",
		Subject:  "Client QOS Rate",
		New:      func() *ui.ClientGroup { return &ui.ClientGroup{} },
		ID:       func(m *clientQosRateKitModel) *types.String { return &m.ID },
		Site:     func(m *clientQosRateKitModel) *types.String { return &m.Site },
		Timeouts: func(m *clientQosRateKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[clientQosRateKitModel, ui.ClientGroup]{
			resourcekit.StringField[clientQosRateKitModel, ui.ClientGroup]{
				Wire:  "name",
				Model: func(m *clientQosRateKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.ClientGroup) *string { return &s.Name },
				Elide: resourcekit.KeepZero,
			},
			// OmitZero: unrelated to the Elide reasoning above -- the
			// controller's own pattern (-1|[2-9]|...|100000) rejects a
			// literal 0 outright, and the schema default (-1) means an
			// unset value is never actually Unknown at ToSDK time, so this
			// is defensive parity with the class rather than a live fix
			// (R2-C Task 10b fix round 1's census).
			resourcekit.Int64PtrField[clientQosRateKitModel, ui.ClientGroup]{
				Wire:     "qos_rate_max_down",
				Model:    func(m *clientQosRateKitModel) *types.Int64 { return &m.QOSRateMaxDown },
				SDK:      func(s *ui.ClientGroup) **int64 { return &s.QOSRateMaxDown },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[clientQosRateKitModel, ui.ClientGroup]{
				Wire:     "qos_rate_max_up",
				Model:    func(m *clientQosRateKitModel) *types.Int64 { return &m.QOSRateMaxUp },
				SDK:      func(s *ui.ClientGroup) **int64 { return &s.QOSRateMaxUp },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
		},
	}
}

// clientQosRateKitSchema is the schema half. No version and no upgraders, and
// the absence is the point: this surface has never changed its state shape, so
// declaring version 0 is the honest value rather than an omission.
func clientQosRateKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_client_qos_rate.ClientQosRateResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

// clientQosRateKitList is the list surface: one filter and a display name.
func clientQosRateKitList() resourcekit.ListSpec[ui.ClientGroup] {
	return resourcekit.ListSpec[ui.ClientGroup]{
		ConfigSchema: listresource_client_qos_rate.ClientQosRateListResourceSchema,
		// Prefer the name, fall back to the id -- a group with no name is not
		// a state the controller should produce, and an empty display name
		// would make the row unidentifiable.
		DisplayName: func(s *ui.ClientGroup) string {
			if s.Name != "" {
				return s.Name
			}
			return s.ID
		},
		Filters: map[string]func(*ui.ClientGroup) string{
			"name": func(s *ui.ClientGroup) string { return s.Name },
		},
	}
}

// clientQosRateKitBackend binds the spec to a client. Separate from the spec so
// a test can build the descriptor with no provider configured, and so a wrong
// method name is a compile error rather than a runtime one.
func clientQosRateKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.ClientGroup] {
	return resourcekit.Backend[ui.ClientGroup]{
		Create: func(ctx context.Context, site string, in *ui.ClientGroup) (*ui.ClientGroup, error) {
			return client.CreateClientGroup(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.ClientGroup, error) {
			return client.GetClientGroup(ctx, site, id)
		},
		// Field-masked, like every other surface on the kit: the SDK carries
		// Update<T>Fields for 33 of its types, so the masked form is the
		// general case, not dns_record's peculiarity.
		UpdateFields: func(ctx context.Context, site string, in *ui.ClientGroup, fields ...string) (*ui.ClientGroup, error) {
			return client.UpdateClientGroupFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteClientGroup(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.ClientGroup, error) {
			return client.ListClientGroup(ctx, site)
		},
		GetID: func(s *ui.ClientGroup) string { return s.ID },
		SetID: func(s *ui.ClientGroup, id string) { s.ID = id },
	}
}
