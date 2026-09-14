package unifi

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_content_filtering "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_content_filtering"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// The unifi_content_filtering descriptor is derived from the controller's
// ContentFiltering definition. The scalars and the six string lists are
// emitted into content_filtering_descriptor_gen.go, as is the schedule
// element model. The hand judgment kept here is schedule's encode/decode: a
// single_nested object over *ui.ContentFilteringSchedule, the same shape
// ospf_router's areas and interfaces carry one nesting level down.
//
// categories, name and schedule are required on create -- behavior.json's
// writes.ContentFiltering, not a hand decision. All three are top-level
// wires, forced required by the compiler.
//
// The create path is POST v2/api/site/{site}/content-filtering/create: the
// one collection in this pipeline whose create verb lands on a /create
// suffix rather than the bare collection path (nat's and ospf_router's do
// not). That is an SDK client fact -- CreateContentFiltering already dials
// it -- not something this descriptor encodes.

// contentFilteringScheduleToAPI encodes the schedule object.
func contentFilteringScheduleToAPI(
	ctx context.Context, object types.Object,
) (*ui.ContentFilteringSchedule, diag.Diagnostics) {
	var diags diag.Diagnostics
	var m contentFilteringScheduleModel
	diags.Append(object.As(ctx, &m, basetypes.ObjectAsOptions{})...)
	if diags.HasError() {
		return nil, diags
	}
	return &ui.ContentFilteringSchedule{Mode: m.Mode.ValueString()}, diags
}

// contentFilteringScheduleFromAPI decodes the schedule object. mode carries
// omitempty on the wire, so an empty string comes back null rather than a
// configured empty.
func contentFilteringScheduleFromAPI(
	_ context.Context, sdk *ui.ContentFilteringSchedule,
) (types.Object, diag.Diagnostics) {
	mode := types.StringValue(sdk.Mode)
	if sdk.Mode == "" {
		mode = types.StringNull()
	}
	return types.ObjectValue(contentFilteringScheduleAttrTypes, map[string]attr.Value{
		"mode": mode,
	})
}

func contentFilteringKitSpec() resourcekit.Spec[contentFilteringKitModel, ui.ContentFiltering] {
	return resourcekit.Spec[contentFilteringKitModel, ui.ContentFiltering]{
		TypeName: "content_filtering",
		Subject:  "Content Filtering Policy",
		IDWire:   "_id",
		New:      func() *ui.ContentFiltering { return &ui.ContentFiltering{} },
		ID:       func(m *contentFilteringKitModel) *types.String { return &m.ID },
		Site:     func(m *contentFilteringKitModel) *types.String { return &m.Site },
		Timeouts: func(m *contentFilteringKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(contentFilteringGenFields(), []resourcekit.Field[contentFilteringKitModel, ui.ContentFiltering]{
			resourcekit.ObjectField[contentFilteringKitModel, ui.ContentFiltering, ui.ContentFilteringSchedule]{
				Wire:      "schedule",
				Model:     func(m *contentFilteringKitModel) *types.Object { return &m.Schedule },
				SDK:       func(s *ui.ContentFiltering) **ui.ContentFilteringSchedule { return &s.Schedule },
				AttrTypes: contentFilteringScheduleAttrTypes,
				Encode:    contentFilteringScheduleToAPI,
				Decode:    contentFilteringScheduleFromAPI,
				Elide:     resourcekit.KeepZero,
			},
		}),
		// Seeded here as well as in contentFilteringKitBackend, because
		// Configure binds the real Backend and a unit test calling ToModel on
		// an unconfigured spec would otherwise dereference nil.
		Backend: resourcekit.Backend[ui.ContentFiltering]{
			GetID: func(s *ui.ContentFiltering) string { return s.ID },
			SetID: func(s *ui.ContentFiltering, id string) { s.ID = id },
		},
	}
}

func contentFilteringKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_content_filtering.ContentFilteringResourceSchema,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	}
}

func contentFilteringKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.ContentFiltering] {
	return resourcekit.Backend[ui.ContentFiltering]{
		Create: func(ctx context.Context, site string, in *ui.ContentFiltering) (*ui.ContentFiltering, error) {
			return client.CreateContentFiltering(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.ContentFiltering, error) {
			return client.GetContentFiltering(ctx, site, id)
		},
		UpdateFields: func(
			ctx context.Context, site string, in *ui.ContentFiltering, fields ...string,
		) (*ui.ContentFiltering, error) {
			return client.UpdateContentFilteringFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteContentFiltering(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.ContentFiltering, error) {
			return client.ListContentFiltering(ctx, site)
		},
		GetID: func(s *ui.ContentFiltering) string { return s.ID },
		SetID: func(s *ui.ContentFiltering, id string) { s.ID = id },
	}
}
