package unifi

// The netflow section descriptor: an unconditional-mirror hydration with no
// specials, shaped like setting_mgmt_descriptor.go. All eleven of
// settings.Netflow's own fields are modelled; none is omitted.
//
// Like every other top-level settings document in this batch, Netflow's
// bootstrap fields carry no captured FieldConstraints (the lookup key is
// "SettingNetflow", not the bare "Netflow" the bootstrap walks a document
// under -- see setting_global_switch_descriptor.go's own comment for the
// mechanism), so sampling_mode's OneOf, port/sampling_rate's Between and
// version's OneOf are hand-transcribed in provider-codegen/policy/setting.json
// rather than compiler-derived.
//
// Five of netflow's six Int64PtrFields carry a controller-published pattern;
// checked one at a time against a literal "0":
//   - engine_id: `^$|[1-9][0-9]*` -- rejects "0" (the digit class starts at
//     1-9), needs OmitZero.
//   - export_frequency: no FieldConstraints entry captured at all -- nothing
//     to check, left without OmitZero (absence of a captured pattern is not
//     evidence 0 is safe, just evidence nobody has measured it either way).
//   - port: `HasBounds` 1024-65535 -- 0 is out of range, needs OmitZero.
//   - refresh_rate: no FieldConstraints entry captured -- same as
//     export_frequency.
//   - sampling_rate: `HasBounds` 2-16383 -- 0 is out of range, needs
//     OmitZero.
//   - version: `Int64Values` {5, 9, 10} -- 0 is not a member, needs
//     OmitZero.
//
// server's own pattern (`.{0,252}[^\.]$`) requires at least one non-dot
// character -- confirmed by running the anchored pattern against "" -- so
// unlike teleport.subnet_cidr (whose pattern has its own `|^$` escape
// hatch) an empty server rejects at plan time, and the field wants
// NullZero rather than KeepZero.
import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// settingNetflowModel is netflow's own section model, decoded out of
// settingResourceModel.Netflow.
type settingNetflowModel struct {
	AutoEngineIDEnabled types.Bool   `tfsdk:"auto_engine_id_enabled"`
	Enabled             types.Bool   `tfsdk:"enabled"`
	EngineID            types.Int64  `tfsdk:"engine_id"`
	ExportFrequency     types.Int64  `tfsdk:"export_frequency"`
	NetworkIDs          types.List   `tfsdk:"network_ids"`
	Port                types.Int64  `tfsdk:"port"`
	RefreshRate         types.Int64  `tfsdk:"refresh_rate"`
	SamplingMode        types.String `tfsdk:"sampling_mode"`
	SamplingRate        types.Int64  `tfsdk:"sampling_rate"`
	Server              types.String `tfsdk:"server"`
	Version             types.Int64  `tfsdk:"version"`
}

// netflowAttrTypes types netflow's own object in state; it must match the
// generated schema exactly.
var netflowAttrTypes = map[string]attr.Type{
	"auto_engine_id_enabled": types.BoolType,
	"enabled":                types.BoolType,
	"engine_id":              types.Int64Type,
	"export_frequency":       types.Int64Type,
	"network_ids":            types.ListType{ElemType: types.StringType},
	"port":                   types.Int64Type,
	"refresh_rate":           types.Int64Type,
	"sampling_mode":          types.StringType,
	"sampling_rate":          types.Int64Type,
	"server":                 types.StringType,
	"version":                types.Int64Type,
}

// netflowKitSpec maps every attribute of the generated netflow schema
// (resource_setting/setting_resource_gen.go's "netflow"
// SingleNestedAttribute) onto settings.Netflow. See this file's own top
// comment for the OmitZero and Elide reasoning behind each field.
func netflowKitSpec() resourcekit.Spec[settingNetflowModel, settings.Netflow] {
	return resourcekit.Spec[settingNetflowModel, settings.Netflow]{
		TypeName: "setting_netflow",
		Subject:  "NetFlow Setting",
		New:      func() *settings.Netflow { return &settings.Netflow{} },
		Fields: []resourcekit.Field[settingNetflowModel, settings.Netflow]{
			resourcekit.BoolField[settingNetflowModel, settings.Netflow]{
				Wire:  "auto_engine_id_enabled",
				Model: func(m *settingNetflowModel) *types.Bool { return &m.AutoEngineIDEnabled },
				SDK:   func(s *settings.Netflow) *bool { return &s.AutoEngineIDEnabled },
			},
			resourcekit.BoolField[settingNetflowModel, settings.Netflow]{
				Wire:  "enabled",
				Model: func(m *settingNetflowModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *settings.Netflow) *bool { return &s.Enabled },
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:     "engine_id",
				Model:    func(m *settingNetflowModel) *types.Int64 { return &m.EngineID },
				SDK:      func(s *settings.Netflow) **int64 { return &s.EngineID },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:  "export_frequency",
				Model: func(m *settingNetflowModel) *types.Int64 { return &m.ExportFrequency },
				SDK:   func(s *settings.Netflow) **int64 { return &s.ExportFrequency },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringListField[settingNetflowModel, settings.Netflow]{
				Wire:  "network_ids",
				Model: func(m *settingNetflowModel) *types.List { return &m.NetworkIDs },
				SDK:   func(s *settings.Netflow) *[]string { return &s.NetworkIDs },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:     "port",
				Model:    func(m *settingNetflowModel) *types.Int64 { return &m.Port },
				SDK:      func(s *settings.Netflow) **int64 { return &s.Port },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:  "refresh_rate",
				Model: func(m *settingNetflowModel) *types.Int64 { return &m.RefreshRate },
				SDK:   func(s *settings.Netflow) **int64 { return &s.RefreshRate },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.StringField[settingNetflowModel, settings.Netflow]{
				Wire:  "sampling_mode",
				Model: func(m *settingNetflowModel) *types.String { return &m.SamplingMode },
				SDK:   func(s *settings.Netflow) *string { return &s.SamplingMode },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:     "sampling_rate",
				Model:    func(m *settingNetflowModel) *types.Int64 { return &m.SamplingRate },
				SDK:      func(s *settings.Netflow) **int64 { return &s.SamplingRate },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
			resourcekit.StringField[settingNetflowModel, settings.Netflow]{
				Wire:  "server",
				Model: func(m *settingNetflowModel) *types.String { return &m.Server },
				SDK:   func(s *settings.Netflow) *string { return &s.Server },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64PtrField[settingNetflowModel, settings.Netflow]{
				Wire:     "version",
				Model:    func(m *settingNetflowModel) *types.Int64 { return &m.Version },
				SDK:      func(s *settings.Netflow) **int64 { return &s.Version },
				Elide:    resourcekit.KeepZero,
				OmitZero: true,
			},
		},
	}
}

// netflowNestedSchema is the netflow SingleNestedAttribute's own
// Attributes, wrapped as a schema.Schema so resourcekit's conformance
// checks -- built for a whole resource's top-level schema -- can run
// against one section of unifi_setting instead.
func netflowNestedSchema(ctx context.Context) schema.Schema {
	built := resource_setting.SettingResourceSchema(ctx)
	netflow := built.Attributes["netflow"].(schema.SingleNestedAttribute) //nolint:forcetypeassert // netflow is declared as SingleNestedAttribute in the generated schema; a mismatch here is a generator regression this is meant to catch loudly.
	return schema.Schema{Attributes: netflow.Attributes}
}

// netflowKitBackend binds netflowKitSpec to a client: Read is
// GetSetting[*Netflow], UpdateFields is the masked UpdateSettingFields --
// naming only the fields the plan set.
func netflowKitBackend(client *ui.ApiClient) resourcekit.Backend[settings.Netflow] {
	return resourcekit.Backend[settings.Netflow]{
		Read: func(ctx context.Context, site, _ string) (*settings.Netflow, error) {
			_, netflow, err := ui.GetSetting[*settings.Netflow](client, ctx, site)
			if err != nil {
				return nil, err
			}
			return netflow, nil
		},
		UpdateFields: func(
			ctx context.Context, site string, in *settings.Netflow, fields ...string,
		) (*settings.Netflow, error) {
			if err := client.UpdateSettingFields(ctx, site, in, fields...); err != nil {
				return nil, err
			}
			return in, nil
		},
	}
}

// netflowKitSection builds the netflow entry for settingResource's
// Sections, bound to client via settingKitSections, which calls it with
// r.client.ApiClient.
func netflowKitSection(client *ui.ApiClient) resourcekit.Section[settingResourceModel] {
	spec := netflowKitSpec()
	spec.Backend = netflowKitBackend(client)
	return resourcekit.SpecSection[settingResourceModel, settingNetflowModel, settings.Netflow]{
		SectionName: "netflow",
		Get:         func(m *settingResourceModel) *types.Object { return &m.Netflow },
		Set:         func(m *settingResourceModel, o types.Object) { m.Netflow = o },
		AttrTypes:   netflowAttrTypes,
		Spec:        spec,
	}
}
