package unifi

import (
	"context"
	"fmt"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

type dnsRecordField string

const (
	dnsRecordFieldEnabled    dnsRecordField = "enabled"
	dnsRecordFieldName       dnsRecordField = "key"
	dnsRecordFieldPort       dnsRecordField = "port"
	dnsRecordFieldPriority   dnsRecordField = "priority"
	dnsRecordFieldRecordType dnsRecordField = "record_type"
	dnsRecordFieldTTL        dnsRecordField = "ttl"
	dnsRecordFieldValue      dnsRecordField = "value"
	dnsRecordFieldWeight     dnsRecordField = "weight"
)

var managedDNSRecordFields = map[dnsRecordField]struct{}{
	dnsRecordFieldEnabled:    {},
	dnsRecordFieldName:       {},
	dnsRecordFieldPort:       {},
	dnsRecordFieldPriority:   {},
	dnsRecordFieldRecordType: {},
	dnsRecordFieldTTL:        {},
	dnsRecordFieldValue:      {},
	dnsRecordFieldWeight:     {},
}

type dnsRecordModel struct {
	ID         string `json:"_id,omitempty"`
	Enabled    bool   `json:"enabled"`
	Name       string `json:"key,omitempty"`
	Port       *int64 `json:"port,omitempty"`
	Priority   int64  `json:"priority,omitempty"`
	RecordType string `json:"record_type,omitempty"`
	TTL        int64  `json:"ttl,omitempty"`
	Value      string `json:"value,omitempty"`
	Weight     int64  `json:"weight,omitempty"`
}

type dnsRecordIntent struct {
	Enabled    bool
	Name       string
	Port       *int64
	Priority   int64
	RecordType string
	TTL        int64
	Value      string
	Weight     int64
}

type dnsRecordPatch struct {
	ID     string
	Values dnsRecordIntent
	Fields []dnsRecordField
}

type dnsRecordBackend interface {
	Create(context.Context, string, dnsRecordIntent) (dnsRecordModel, error)
	Read(context.Context, string, string) (dnsRecordModel, error)
	Update(context.Context, string, dnsRecordPatch) (dnsRecordModel, error)
	Delete(context.Context, string, string) error
	List(context.Context, string) ([]dnsRecordModel, error)
}

type privateDNSRecordBackend struct {
	client *ui.ApiClient
}

var _ dnsRecordBackend = (*privateDNSRecordBackend)(nil)

func newPrivateDNSRecordBackend(client *ui.ApiClient) dnsRecordBackend {
	return &privateDNSRecordBackend{client: client}
}

func (b *privateDNSRecordBackend) Create(
	ctx context.Context,
	site string,
	intent dnsRecordIntent,
) (dnsRecordModel, error) {
	record, err := b.client.CreateDNSRecord(ctx, site, dnsRecordFromIntent(intent))
	if err != nil {
		return dnsRecordModel{}, err
	}
	return dnsRecordFromAPI(record), nil
}

func (b *privateDNSRecordBackend) Read(
	ctx context.Context,
	site string,
	id string,
) (dnsRecordModel, error) {
	record, err := b.client.GetDNSRecord(ctx, site, id)
	if err != nil {
		return dnsRecordModel{}, err
	}
	return dnsRecordFromAPI(record), nil
}

func (b *privateDNSRecordBackend) Update(
	ctx context.Context,
	site string,
	patch dnsRecordPatch,
) (dnsRecordModel, error) {
	fields, err := patch.wireFields()
	if err != nil {
		return dnsRecordModel{}, err
	}
	if patch.ID == "" {
		return dnsRecordModel{}, fmt.Errorf("DNS record patch has an empty ID")
	}
	record := dnsRecordFromIntent(patch.Values)
	record.ID = patch.ID
	updated, err := b.client.UpdateDNSRecordFields(ctx, site, record, fields...)
	if err != nil {
		return dnsRecordModel{}, err
	}
	return dnsRecordFromAPI(updated), nil
}

func (b *privateDNSRecordBackend) Delete(ctx context.Context, site, id string) error {
	return b.client.DeleteDNSRecord(ctx, site, id)
}

func (b *privateDNSRecordBackend) List(ctx context.Context, site string) ([]dnsRecordModel, error) {
	records, err := b.client.ListDNSRecord(ctx, site)
	if err != nil {
		return nil, err
	}
	models := make([]dnsRecordModel, 0, len(records))
	for i := range records {
		models = append(models, dnsRecordFromAPI(&records[i]))
	}
	return models, nil
}

func (p dnsRecordPatch) wireFields() ([]string, error) {
	if len(p.Fields) == 0 {
		return nil, fmt.Errorf("DNS record patch needs at least one managed field")
	}
	seen := make(map[dnsRecordField]struct{}, len(p.Fields))
	fields := make([]string, 0, len(p.Fields))
	for _, field := range p.Fields {
		if _, ok := managedDNSRecordFields[field]; !ok {
			return nil, fmt.Errorf("DNS record patch contains unknown field %q", field)
		}
		if _, duplicate := seen[field]; duplicate {
			return nil, fmt.Errorf("DNS record patch contains duplicate field %q", field)
		}
		seen[field] = struct{}{}
		fields = append(fields, string(field))
	}
	return fields, nil
}

func dnsRecordFromIntent(intent dnsRecordIntent) *ui.DNSRecord {
	return &ui.DNSRecord{
		Enabled:    intent.Enabled,
		Key:        intent.Name,
		Port:       copyInt64Pointer(intent.Port),
		Priority:   intent.Priority,
		RecordType: intent.RecordType,
		Ttl:        intent.TTL,
		Value:      intent.Value,
		Weight:     intent.Weight,
	}
}

func dnsRecordFromAPI(record *ui.DNSRecord) dnsRecordModel {
	return dnsRecordModel{
		ID:         record.ID,
		Enabled:    record.Enabled,
		Name:       record.Key,
		Port:       copyInt64Pointer(record.Port),
		Priority:   record.Priority,
		RecordType: record.RecordType,
		TTL:        record.Ttl,
		Value:      record.Value,
		Weight:     record.Weight,
	}
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
