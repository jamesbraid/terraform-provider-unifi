package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resource_dns_record "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
)

func TestDNSRecordSchemaUsesCompilerOutput(t *testing.T) {
	ctx := context.Background()
	generated := resource_dns_record.DnsRecordResourceSchema(ctx)
	generated.Version = 1
	generated.Attributes["timeouts"] = timeouts.Attributes(
		ctx,
		timeouts.Opts{Create: true, Read: true, Update: true, Delete: true},
	)

	var response resource.SchemaResponse
	newDNSRecordKitResource().Schema(ctx, resource.SchemaRequest{}, &response)
	if response.Schema.Version != generated.Version {
		t.Fatalf("schema version = %d, want %d", response.Schema.Version, generated.Version)
	}
	if len(response.Schema.Attributes) != len(generated.Attributes) {
		t.Fatalf("schema attribute count = %d, want %d", len(response.Schema.Attributes), len(generated.Attributes))
	}
	for name, expected := range generated.Attributes {
		actual, ok := response.Schema.Attributes[name]
		if !ok {
			t.Errorf("schema is missing compiler attribute %q", name)
			continue
		}
		if reflect.TypeOf(actual) != reflect.TypeOf(expected) {
			t.Errorf("schema attribute %q type = %T, want %T", name, actual, expected)
		}
	}
}

func TestDNSRecordCompilerCutoverKeepsStateUpgrade(t *testing.T) {
	upgraders := newDNSRecordKitResource().UpgradeState(context.Background())
	if _, ok := upgraders[0]; !ok {
		t.Fatal("DNS v0 state upgrader was removed during compiler cutover")
	}
}
