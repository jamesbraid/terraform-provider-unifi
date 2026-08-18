package unifi

// The client_qos_rate descriptor.
//
// Produced by the same derivation the dns_record one documents: every value
// below comes from provider-codegen/generated/client_qos_rate.mapping.json,
// the go-unifi ClientGroup struct, or the generated schema. The two that come
// from neither are named where they appear.
//
// SECOND SURFACE ON THE KIT. dns_record proved the shape; this one is the
// first taken from the survey's clean list, chosen because it carries no state
// upgrader, no config validators, no plan modifier and no retry -- the
// exceptions that make the other surfaces individual. What is left is the
// mechanical part, which is the part worth moving first.

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

// clientQosRateKitSpec is the whole of what varies.
//
// Elide is DERIVED, not chosen. name is Required, so its zero must survive.
// THE TWO RATE FIELDS ARE Optional+Computed AND THEREFORE ALSO KeepZero, which
// is not what I first wrote: a practitioner may set an explicit zero, and
// nulling it makes state disagree with config. The check passed the wrong
// values because the rule it encoded had never met an Optional+Computed field
// carrying an Elide -- dns_record, the surface it was built on, has none. ElideProblems enforces exactly that and fails if this
// drifts -- which is the check dns_record did not have when its record_type
// was wrong.
//
// The rates are Int64Ptr because the SDK declares them *int64. That is read
// from the struct rather than decided; the mapping says int64 either way, and
// pointer-ness is one of the two facts cmd/sdk-bootstrap now carries because
// nothing else does.
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
			resourcekit.Int64PtrField[clientQosRateKitModel, ui.ClientGroup]{
				Wire:  "qos_rate_max_down",
				Model: func(m *clientQosRateKitModel) *types.Int64 { return &m.QOSRateMaxDown },
				SDK:   func(s *ui.ClientGroup) **int64 { return &s.QOSRateMaxDown },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[clientQosRateKitModel, ui.ClientGroup]{
				Wire:  "qos_rate_max_up",
				Model: func(m *clientQosRateKitModel) *types.Int64 { return &m.QOSRateMaxUp },
				SDK:   func(s *ui.ClientGroup) **int64 { return &s.QOSRateMaxUp },
				Elide: resourcekit.KeepZero,
			},
		},
		Backend: resourcekit.Backend[ui.ClientGroup]{
			GetID: func(s *ui.ClientGroup) string { return s.ID },
			SetID: func(s *ui.ClientGroup, id string) { s.ID = id },
		},
	}
}

// clientQosRateKitSchema is the schema half. NO VERSION AND NO UPGRADERS, and
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
		// PREFER THE NAME, FALL BACK TO THE ID -- a group with no name is not a
		// state the controller should produce, and an empty display name would
		// make the row unidentifiable.
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
		// FIELD-MASKED, like every other surface on the kit. The SDK carries
		// Update<T>Fields for 33 of its types, so the masked form is the
		// general case rather than dns_record's peculiarity -- which is worth
		// recording, because I had it filed the other way round.
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
