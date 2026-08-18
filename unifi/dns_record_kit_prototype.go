package unifi

// PROTOTYPE, NOT GENERATED YET. This file is what cmd/provider-spec-compiler
// would emit for unifi_dns_record under the shared-implementation shape, hand-
// written first so the shape could be measured against the 838 lines it
// replaces before a generator was built to produce it. Every value in it comes
// from provider-codegen/generated/dns_record.mapping.json except the four noted
// below, which no artifact carries.

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
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
// FOUR VALUES HERE COME FROM NOWHERE AN ARTIFACT CARRIES, and they are the
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
				Elide: resourcekit.NullZero,
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
		Backend: resourcekit.Backend[ui.DNSRecord]{
			GetID: func(s *ui.DNSRecord) string { return s.ID },
			SetID: func(s *ui.DNSRecord, id string) { s.ID = id },
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
		GetID: func(s *ui.DNSRecord) string { return s.ID },
		SetID: func(s *ui.DNSRecord, id string) { s.ID = id },
	}
}
