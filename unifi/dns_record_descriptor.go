package unifi

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

// dnsRecordKitSpec keeps the two entries the artifacts cannot state: ttl's
// unit (the controller counts seconds) and port's OmitZero staying off --
// the derivation would turn it on, and omit_zero_census_test.go's pin
// records why that fix was declined.
func dnsRecordKitSpec() resourcekit.Spec[dnsRecordKitModel, ui.DNSRecord] {
	return resourcekit.Spec[dnsRecordKitModel, ui.DNSRecord]{
		TypeName: "dns_record",
		Subject:  "Dns Record",
		New:      func() *ui.DNSRecord { return &ui.DNSRecord{} },
		ID:       func(m *dnsRecordKitModel) *types.String { return &m.ID },
		Site:     func(m *dnsRecordKitModel) *types.String { return &m.Site },
		Timeouts: func(m *dnsRecordKitModel) *timeouts.Value { return &m.Timeouts },
		Fields: resourcekit.Override(dnsRecordGenFields(), []resourcekit.Field[dnsRecordKitModel, ui.DNSRecord]{
			resourcekit.Int64PtrField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "port",
				Model: func(m *dnsRecordKitModel) *types.Int64 { return &m.Port },
				SDK:   func(s *ui.DNSRecord) **int64 { return &s.Port },
				Elide: resourcekit.NullZero,
			},
			resourcekit.DurationField[dnsRecordKitModel, ui.DNSRecord]{
				Wire:  "ttl",
				Model: func(m *dnsRecordKitModel) *timetypes.GoDuration { return &m.TTL },
				SDK:   func(s *ui.DNSRecord) *int64 { return &s.Ttl },
				Units: time.Second,
				Elide: resourcekit.NullZero,
			},
		}),
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
						resp.Diagnostics.AddError("Failed to upgrade DNS record state", resourcekit.DiagErrorText(err))
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
