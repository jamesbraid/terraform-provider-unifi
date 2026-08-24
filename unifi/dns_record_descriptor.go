package unifi

// The dns_record descriptor is hand-written, not yet generated: this is what
// cmd/provider-spec-compiler will emit.

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/listresource_dns_record"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// dnsRecordKitModel is the generated model. Identical in shape to the
// hand-written one; the tfsdk tags are what the framework reflects on.
type dnsRecordKitModel struct {
	ID         types.String         `tfsdk:"id"`
	Site       types.String         `tfsdk:"site"`
	Name       types.String         `tfsdk:"name"`
	Enabled    types.Bool           `tfsdk:"enabled"`
	Port       types.Int64          `tfsdk:"port"`
	Priority   types.Int64          `tfsdk:"priority"`
	RecordType types.String         `tfsdk:"record_type"`
	TTL        timetypes.GoDuration `tfsdk:"ttl"`
	Value      types.String         `tfsdk:"value"`
	Weight     types.Int64          `tfsdk:"weight"`
	Timeouts   timeouts.Value       `tfsdk:"timeouts"`
}

// dnsRecordKitSpec is the whole of what varies.
//
// Four values here come from nowhere an artifact carries, and they are the
// compiler's remaining gap rather than an authoring choice:
//
//	the Go identifier per field   key -> Key, ttl -> Ttl, record_type -> RecordType
//	pointer-ness                  port is *int64 where the mapping says int64
//	the duration unit             ttl counts seconds
//	the SDK method names          and whether the update is field-masked
//
// The first two are readable from the SDK struct -- cmd/sdk-bootstrap already
// parses it and discards them. The last two are decisions.
func dnsRecordKitSpec() resourcekit.Spec[dnsRecordKitModel, ui.DNSRecord] {
	return resourcekit.Spec[dnsRecordKitModel, ui.DNSRecord]{
		TypeName: "dns_record",
		Subject:  "Dns Record",
		New:      func() *ui.DNSRecord { return &ui.DNSRecord{} },
		ID:       func(m *dnsRecordKitModel) *types.String { return &m.ID },
		Site:     func(m *dnsRecordKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dnsRecordKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: []resourcekit.Field[dnsRecordKitModel, ui.DNSRecord]{
			resourcekit.BoolField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "enabled",
				Model: func(m *dnsRecordKitModel) *types.Bool { return &m.Enabled },
				SDK:   func(s *ui.DNSRecord) *bool { return &s.Enabled },
			},
			resourcekit.StringField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "key",
				Model: func(m *dnsRecordKitModel) *types.String { return &m.Name },
				SDK:   func(s *ui.DNSRecord) *string { return &s.Key },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64PtrField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "port",
				Model: func(m *dnsRecordKitModel) *types.Int64 { return &m.Port },
				SDK:   func(s *ui.DNSRecord) **int64 { return &s.Port },
				Elide: resourcekit.NullZero,
			},
			resourcekit.Int64Field[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "priority",
				Model: func(m *dnsRecordKitModel) *types.Int64 { return &m.Priority },
				SDK:   func(s *ui.DNSRecord) *int64 { return &s.Priority },
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "record_type",
				Model: func(m *dnsRecordKitModel) *types.String { return &m.RecordType },
				SDK:   func(s *ui.DNSRecord) *string { return &s.RecordType },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.DurationField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "ttl",
				Model: func(m *dnsRecordKitModel) *timetypes.GoDuration { return &m.TTL },
				SDK:   func(s *ui.DNSRecord) *int64 { return &s.Ttl },
				Units: time.Second,
				Elide: resourcekit.NullZero,
			},
			resourcekit.StringField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "value",
				Model: func(m *dnsRecordKitModel) *types.String { return &m.Value },
				SDK:   func(s *ui.DNSRecord) *string { return &s.Value },
				Elide: resourcekit.KeepZero,
			},
			resourcekit.Int64Field[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "weight",
				Model: func(m *dnsRecordKitModel) *types.Int64 { return &m.Weight },
				SDK:   func(s *ui.DNSRecord) *int64 { return &s.Weight },
				Elide: resourcekit.NullZero,
			},
		},
	}
}

// dnsRecordKitSchema is the schema half: the generated schema, the version its
// state migration established, and the upgrader itself.
func dnsRecordKitSchema() resourcekit.SchemaSpec {
	return resourcekit.SchemaSpec{
		Resource: resource_dns_record.DnsRecordResourceSchema,
		// v1: ttl moved from Int64 seconds to a GoDuration string.
		Version:  1,
		Timeouts: timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
		Upgraders: func(_ context.Context, built schema.Schema) map[int64]resource.StateUpgrader {
			return map[int64]resource.StateUpgrader{
				0: {StateUpgrader: func(
					ctx context.Context,
					req resource.UpgradeStateRequest,
					resp *resource.UpgradeStateResponse,
				) {
					if req.RawState == nil {
						return
					}
					dv, err := util.UpgradeDurationRawState(
						built.Type().TerraformType(ctx),
						req.RawState.JSON,
						func(state map[string]any) {
							util.SetDurationField(state, "ttl", time.Second)
						},
					)
					if err != nil {
						resp.Diagnostics.AddError("Failed to upgrade DNS record state", err.Error())
						return
					}
					resp.DynamicValue = dv
				}},
			}
		},
	}
}

// dnsRecordKitList is the list surface. Three filters and a display name are
// the whole of what varies; the streaming, the identity and the model
// conversion are the kit's.
func dnsRecordKitList() resourcekit.ListSpec[ui.DNSRecord] {
	return resourcekit.ListSpec[ui.DNSRecord]{
		ConfigSchema: listresource_dns_record.DnsRecordListResourceSchema,
		// Prefer the key, fall back to the id. A record with no key is not a
		// state the controller should produce, and showing an empty display
		// name instead of an id would make the result unidentifiable.
		DisplayName: func(s *ui.DNSRecord) string {
			if s.Key != "" {
				return s.Key
			}
			return s.ID
		},
		Filters: map[string]func(*ui.DNSRecord) string{
			"name":        func(s *ui.DNSRecord) string { return s.Key },
			"record_type": func(s *ui.DNSRecord) string { return s.RecordType },
			"enabled":     func(s *ui.DNSRecord) string { return fmt.Sprintf("%t", s.Enabled) },
		},
	}
}

// dnsRecordKitBackend binds the spec to a client. Separate from the spec so the
// method set is named in one place and a wrong name does not compile.
func dnsRecordKitBackend(client *ui.ApiClient) resourcekit.Backend[ui.DNSRecord] {
	return resourcekit.Backend[ui.DNSRecord]{
		Create: func(ctx context.Context, site string, in *ui.DNSRecord) (*ui.DNSRecord, error) {
			return client.CreateDNSRecord(ctx, site, in)
		},
		Read: func(ctx context.Context, site, id string) (*ui.DNSRecord, error) {
			return client.GetDNSRecord(ctx, site, id)
		},
		UpdateFields: func(ctx context.Context, site string, in *ui.DNSRecord, fields ...string) (*ui.DNSRecord, error) {
			return client.UpdateDNSRecordFields(ctx, site, in, fields...)
		},
		Delete: func(ctx context.Context, site, id string) error {
			return client.DeleteDNSRecord(ctx, site, id)
		},
		List: func(ctx context.Context, site string) ([]ui.DNSRecord, error) {
			return client.ListDNSRecord(ctx, site)
		},
		GetID: func(s *ui.DNSRecord) string { return s.ID },
		SetID: func(s *ui.DNSRecord, id string) { s.ID = id },
	}
}
