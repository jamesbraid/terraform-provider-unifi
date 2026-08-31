package unifi

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// deviceValidateConfigFor builds a tfsdk.Config carrying only a
// port_override block -- every other device attribute is left absent and
// reads back null, the same SetAttribute-onto-a-null-raw approach
// syslogValidateConfigFor (setting_syslog_validate_test.go) uses.
// portOverride may be a null Set, standing in for a config with no
// port_override block declared at all.
func deviceValidateConfigFor(
	t *testing.T, ctx context.Context, schema fwresource.SchemaResponse, portOverride types.Set,
) tfsdk.Config {
	t.Helper()
	schemaType := schema.Schema.Type().TerraformType(ctx)
	staging := tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schemaType, nil)}
	if diags := staging.SetAttribute(ctx, path.Root("port_override"), portOverride); diags.HasError() {
		t.Fatalf("staging the port_override attribute: %v", diags)
	}
	return tfsdk.Config{Schema: schema.Schema, Raw: staging.Raw}
}

// TestDeviceValidateConfigTaggedNetworkIDsWarns pins the plan-time warning
// device_validate.go adds: a port_override block that sets
// tagged_networkconf_ids must warn exactly once, naming the attribute --
// measured against this controller generation, the value is accepted
// (200/rc:ok) and discarded, and forward is additionally reverted to "all"
// (see the comment on devicePortOverrideEncode in device_descriptor.go).
// Leaving it unset -- no port_override block at all, or a block that
// declares other members but not this one -- must produce no diagnostics:
// the configuration is not wrong, so this is a warning, never an error.
func TestDeviceValidateConfigTaggedNetworkIDsWarns(t *testing.T) {
	ctx := context.Background()
	r := newDeviceKitResource()
	var schemaResp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	taggedSet, diags := types.SetValue(types.StringType, []attr.Value{types.StringValue("net-1")})
	if diags.HasError() {
		t.Fatalf("building tagged_networkconf_ids: %v", diags)
	}

	tests := []struct {
		name         string
		portOverride types.Set
		wantWarnings int
	}{
		{
			name:         "no_port_override_block_at_all",
			portOverride: types.SetNull(devicePortOverrideElementType(ctx)),
			wantWarnings: 0,
		},
		{
			name: "block_without_tagged_networkconf_ids",
			portOverride: portOverrideSetWith(t, map[string]attr.Value{
				"index": types.Int64Value(1),
				"name":  types.StringValue("uplink"),
			}),
			wantWarnings: 0,
		},
		{
			name: "block_with_tagged_networkconf_ids",
			portOverride: portOverrideSetWith(t, map[string]attr.Value{
				"index":                  types.Int64Value(1),
				"tagged_networkconf_ids": taggedSet,
			}),
			wantWarnings: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
				Config: deviceValidateConfigFor(t, ctx, schemaResp, tt.portOverride),
			}, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("unexpected error diagnostics (must be a warning, never an error): %v", resp.Diagnostics)
			}
			if got := len(resp.Diagnostics.Warnings()); got != tt.wantWarnings {
				t.Fatalf("got %d warnings, want %d (diags: %v)", got, tt.wantWarnings, resp.Diagnostics)
			}
			for _, d := range resp.Diagnostics.Warnings() {
				if !strings.Contains(d.Summary(), "tagged_networkconf_ids") &&
					!strings.Contains(d.Detail(), "tagged_networkconf_ids") {
					t.Errorf("warning does not name tagged_networkconf_ids: %v", d)
				}
			}
		})
	}
}
