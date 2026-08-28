package unifi

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// TestDNSRecordPortRejectsZeroAtPlan pins provider-codegen/policy/dns_record.json's
// R2-C Task 10c correction: port's hand int64validator.Between(0, 65535) permitted a
// literal 0 in config, while the controller's own published pattern for this field
// ([1-9][0-9]{0,4}, from go-unifi's FieldConstraints["DNSRecord"]["port"]) cannot match
// "0" at all -- a practitioner who typed port = 0 passed schema validation and only
// found out live, from api.err.InvalidValue. A 0 must now fail during config
// validation -- at plan, not apply.
func TestDNSRecordPortRejectsZeroAtPlan(t *testing.T) {
	ctx := context.Background()
	var response resource.SchemaResponse
	newDNSRecordKitResource().Schema(ctx, resource.SchemaRequest{}, &response)
	if response.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", response.Diagnostics)
	}

	attribute, ok := response.Schema.Attributes["port"]
	if !ok {
		t.Fatal(`schema is missing attribute "port"`)
	}
	int64Attribute, ok := attribute.(schema.Int64Attribute)
	if !ok {
		t.Fatalf(`attribute "port" is a %T, want schema.Int64Attribute`, attribute)
	}

	validatePort := func(t *testing.T, value int64) diag.Diagnostics {
		t.Helper()
		var diags diag.Diagnostics
		for _, v := range int64Attribute.Validators {
			validateResp := &validator.Int64Response{}
			v.ValidateInt64(ctx, validator.Int64Request{
				Path:        path.Root("port"),
				ConfigValue: types.Int64Value(value),
			}, validateResp)
			diags.Append(validateResp.Diagnostics...)
		}
		return diags
	}

	if diags := validatePort(t, 0); !diags.HasError() {
		t.Error("port = 0 passed config validation; want a plan-time error, since the " +
			"controller's own pattern ([1-9][0-9]{0,4}) cannot match 0")
	}
	// The control: a real port number must still pass, or the assertion above
	// would hold for a validator that rejects everything.
	if diags := validatePort(t, 53); diags.HasError() {
		t.Errorf("port = 53 failed config validation: %v", diags)
	}
}

func TestDNSRecordCompilerCutoverKeepsStateUpgrade(t *testing.T) {
	upgraders := newDNSRecordKitResource().UpgradeState(context.Background())
	if _, ok := upgraders[0]; !ok {
		t.Fatal("DNS v0 state upgrader was removed during compiler cutover")
	}
}
