package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_hotspot_package "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_hotspot_package"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The unifi_hotspot_package descriptor is derived from the controller's
// HotspotPackage definition. Everything mechanical -- the model and every
// plain string/bool/int64 field -- is emitted into
// hotspot_package_descriptor_gen.go. The judgment kept by hand here is only
// amount and trial_reset: the SDK carries both as float64, a wire shape none
// of the mechanically-derivable field kinds cover, so they are mapped with
// resourcekit.Float64Field instead.
//
// trial_duration_minutes is required on create (writes.HotspotPackage's
// required_on_create); that requiredness is not declared here but forced by
// the compiler from the behaviour artifact, the same way nat's protocol is.
// It is also what satisfies the controller's other measured constraint,
// which the compiler has no field for: a masked update to hotspotpackage
// that names any field but neither trial_duration_minutes nor hours is
// refused with api.err.InvalidHotspotPackageDuration, even when the stored
// package already carries a duration. Nothing here has to reach for that
// specially -- a Required attribute is never null in a plan, so
// trial_duration_minutes's own Field is always in the update's wire mask
// alongside whatever else changed, on every update this resource makes.
//
// The controller's sanitizer is in fact an exclusive-or keyed on amount: zero
// wants trial_duration_minutes and refuses hours, non-zero wants hours and
// refuses trial_duration_minutes. required_on_create only ever measured the
// free-trial half of that (paid creates were probed and never accepted on the
// harness that measured this SDK), and forcing trial_duration_minutes
// Required is a blanket rule the compiler cannot make conditional on amount.
// So this resource models the free-trial packages the artifact actually
// proves; a paid, hours-priced package needs trial_duration_minutes absent,
// which a Required attribute has no way to express.
//
// currency carries a validator from its own [A-Z]{3} pattern, which already
// keeps "" out of a practitioner's config -- the empty-write suppression NAT
// needed for in_interface/ip_address does not apply here, since currency's
// own empty-family entry is EMPTY-REJECTED paired with OMIT-KEEPS, not
// OMIT-CLEARS: omitting it changes nothing, it does not clear a stored value.
// charged_as clears normally through the masked path (EMPTY-CLEARS), so it
// carries no such caveat either. The four attr_* controller internals stay
// unmapped.
func hotspotPackageKitSpec() resourcekit.Spec[hotspotPackageKitModel, ui.HotspotPackage] {
	return resourcekit.Spec[hotspotPackageKitModel, ui.HotspotPackage]{
		TypeName: "hotspot_package",
		Subject:  "Hotspot Package",
		IDWire:   "_id",
		New:      func() *ui.HotspotPackage { return &ui.HotspotPackage{} },
		ID:       func(m *hotspotPackageKitModel) *types.String { return &m.ID },
		Site:     func(m *hotspotPackageKitModel) *types.String { return &m.Site },
		Timeouts: func(m *hotspotPackageKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(
			hotspotPackageGenFields(),
			[]resourcekit.Field[hotspotPackageKitModel, ui.HotspotPackage]{
				resourcekit.Float64Field[hotspotPackageKitModel, ui.HotspotPackage]{
					Wire:  "amount",
					Model: func(m *hotspotPackageKitModel) *types.Float64 { return &m.Amount },
					SDK:   func(s *ui.HotspotPackage) *float64 { return &s.Amount },
					Elide: resourcekit.NullZero,
				},
				resourcekit.Float64Field[hotspotPackageKitModel, ui.HotspotPackage]{
					Wire:  "trial_reset",
					Model: func(m *hotspotPackageKitModel) *types.Float64 { return &m.TrialReset },
					SDK:   func(s *ui.HotspotPackage) *float64 { return &s.TrialReset },
					Elide: resourcekit.NullZero,
				},
			},
		),
		// Seeded here as well as in hotspotPackageKitBackend, because
		// Configure binds the real Backend and a unit test calling ToModel on
		// an unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.HotspotPackage]{
			GetID: func(s *ui.HotspotPackage) string { return s.ID },
			SetID: func(s *ui.HotspotPackage, id string) { s.ID = id },
		},
	}
}

func hotspotPackageKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_hotspot_package.HotspotPackageResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func hotspotPackageKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.HotspotPackage] {
	return resourcekit.Backend[ui.HotspotPackage]{
		Create: func(ctx context.Context, site string, in *ui.HotspotPackage) (*ui.HotspotPackage, error) {
			return client.CreateHotspotPackage(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.HotspotPackage, error) {
			return client.GetHotspotPackage(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.HotspotPackage, fields ...string,
		) (*ui.HotspotPackage, error) {
			return client.UpdateHotspotPackageFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteHotspotPackage(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.HotspotPackage, error) {
			return client.ListHotspotPackage(ctx, site)
		},
		GetID: func(s *ui.HotspotPackage) string { return s.ID },
		SetID: func(s *ui.HotspotPackage, id string) { s.ID = id },
	}
}
