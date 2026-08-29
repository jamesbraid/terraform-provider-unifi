package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// syslogValidateConfigFor builds a tfsdk.Config carrying only a syslog
// block (enabled, ip) -- every other attribute, including the rest of
// syslog's own, is left absent and reads back null, which SetAttribute
// fills in from the schema. That mirrors what a practitioner config setting
// only these two syslog attributes would produce.
func syslogValidateConfigFor(
	t *testing.T, ctx context.Context, schema fwresource.SchemaResponse, enabled types.Bool, ip types.String,
) tfsdk.Config {
	t.Helper()
	syslogModel := settingSyslogModel{
		Contents:                    types.ListNull(types.StringType),
		Debug:                       types.BoolNull(),
		Enabled:                     enabled,
		IP:                          ip,
		LogAllContents:              types.BoolNull(),
		NetconsoleEnabled:           types.BoolNull(),
		NetconsoleHost:              types.StringNull(),
		NetconsolePort:              types.Int64Null(),
		Port:                        types.Int64Null(),
		ThisController:              types.BoolNull(),
		ThisControllerEncryptedOnly: types.BoolNull(),
	}
	syslogObj, diags := types.ObjectValueFrom(ctx, syslogAttrTypes, syslogModel)
	if diags.HasError() {
		t.Fatalf("building the syslog object: %v", diags)
	}

	schemaType := schema.Schema.Type().TerraformType(ctx)
	staging := tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schemaType, nil)}
	if diags := staging.SetAttribute(ctx, path.Root("syslog"), syslogObj); diags.HasError() {
		t.Fatalf("staging the syslog attribute: %v", diags)
	}
	return tfsdk.Config{Schema: schema.Schema, Raw: staging.Raw}
}

// TestSettingValidateConfigSyslogRequiresAnAddressWhenEnabled pins the plan-time
// rule ValidateConfig enforces (setting_syslog_validate.go): the controller
// rejects syslog.enabled = true with no syslog.ip (api.err.Invalid,
// measured on 10.4.57 by Task 1), so this catches it before apply.
func TestSettingValidateConfigSyslogRequiresAnAddressWhenEnabled(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}
	schemaResp := fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	tests := []struct {
		name      string
		enabled   types.Bool
		ip        types.String
		wantError bool
	}{
		{"enabled_without_ip_errors", types.BoolValue(true), types.StringNull(), true},
		{"enabled_with_ip_is_clean", types.BoolValue(true), types.StringValue("10.0.0.5"), false},
		{"enabled_unknown_is_clean", types.BoolUnknown(), types.StringNull(), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
				Config: syslogValidateConfigFor(t, ctx, schemaResp, tt.enabled, tt.ip),
			}, resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("enabled=%v ip=%v: got error=%v, want %v (diags: %v)",
					tt.enabled, tt.ip, got, tt.wantError, resp.Diagnostics)
			}
			if !tt.wantError {
				return
			}
			want := path.Root("syslog").AtName("ip")
			var atWantPath bool
			for _, d := range resp.Diagnostics.Errors() {
				withPath, ok := d.(diag.DiagnosticWithPath)
				if ok && withPath.Path().Equal(want) {
					atWantPath = true
				}
			}
			if !atWantPath {
				t.Errorf("no error diagnostic at %s; got %v", want, resp.Diagnostics)
			}
		})
	}
}
