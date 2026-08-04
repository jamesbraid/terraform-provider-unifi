package unifi

import (
	"context"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

type dnsRecordFieldUpdater interface {
	UpdateDNSRecordFields(
		context.Context,
		string,
		*ui.DNSRecord,
		...string,
	) (*ui.DNSRecord, error)
}

var _ dnsRecordFieldUpdater = (*ui.ApiClient)(nil)

func TestDNSRecordBackendDependency(t *testing.T) {
	t.Helper()
}
