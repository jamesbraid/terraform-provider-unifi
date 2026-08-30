package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// mdnsValidateConfigFor builds a tfsdk.Config carrying only an mdns block
// (mode, custom_services, predefined_services) -- every other attribute is
// left absent and reads back null, which SetAttribute fills in from the
// schema. Mirrors syslogValidateConfigFor's own shape
// (setting_syslog_validate_test.go).
func mdnsValidateConfigFor(
	t *testing.T, ctx context.Context, schema fwresource.SchemaResponse,
	mode types.String, customServices, predefinedServices types.List,
) tfsdk.Config {
	t.Helper()
	mdnsModel := settingMdnsModel{
		Mode:               mode,
		CustomServices:     customServices,
		PredefinedServices: predefinedServices,
	}
	mdnsObj, diags := types.ObjectValueFrom(ctx, mdnsAttrTypes, mdnsModel)
	if diags.HasError() {
		t.Fatalf("building the mdns object: %v", diags)
	}

	schemaType := schema.Schema.Type().TerraformType(ctx)
	staging := tfsdk.State{Schema: schema.Schema, Raw: tftypes.NewValue(schemaType, nil)}
	if diags := staging.SetAttribute(ctx, path.Root("mdns"), mdnsObj); diags.HasError() {
		t.Fatalf("staging the mdns attribute: %v", diags)
	}
	return tfsdk.Config{Schema: schema.Schema, Raw: staging.Raw}
}

// TestSettingValidateConfigMdnsRequiresCustomModeForServiceLists pins the
// plan-time rule validateMdnsConfig enforces (setting_mdns_validate.go):
// mode != "custom" with a non-empty custom_services or predefined_services
// fails apply against the pinned controller (Provider produced
// inconsistent result after apply, reproduced directly -- see
// setting_mdns_validate.go's own comment), so this catches it before
// apply.
func TestSettingValidateConfigMdnsRequiresCustomModeForServiceLists(t *testing.T) {
	ctx := context.Background()
	r := &settingResource{}
	schemaResp := fwresource.SchemaResponse{}
	r.Schema(ctx, fwresource.SchemaRequest{}, &schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("build the schema: %v", schemaResp.Diagnostics)
	}

	customServicesElemType := types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}
	predefinedServicesElemType := types.ObjectType{AttrTypes: mdnsPredefinedServiceAttrTypes}

	oneCustomService := types.ListValueMust(customServicesElemType, []attr.Value{
		types.ObjectValueMust(mdnsCustomServiceAttrTypes, map[string]attr.Value{
			"address": types.StringValue("_myservice._tcp.local"),
			"name":    types.StringValue("my service"),
		}),
	})
	onePredefinedService := types.ListValueMust(predefinedServicesElemType, []attr.Value{
		types.ObjectValueMust(mdnsPredefinedServiceAttrTypes, map[string]attr.Value{
			"code": types.StringValue("printers"),
		}),
	})
	emptyCustomServices := types.ListValueMust(customServicesElemType, []attr.Value{})
	emptyPredefinedServices := types.ListValueMust(predefinedServicesElemType, []attr.Value{})
	nullCustomServices := types.ListNull(customServicesElemType)
	nullPredefinedServices := types.ListNull(predefinedServicesElemType)
	unknownCustomServices := types.ListUnknown(customServicesElemType)
	unknownPredefinedServices := types.ListUnknown(predefinedServicesElemType)

	tests := []struct {
		name               string
		mode               types.String
		customServices     types.List
		predefinedServices types.List
		wantErrorAt        *path.Path
	}{
		{
			name:               "custom_mode_with_both_lists_is_clean",
			mode:               types.StringValue("custom"),
			customServices:     oneCustomService,
			predefinedServices: onePredefinedService,
			wantErrorAt:        nil,
		},
		{
			// The exact shape reproduced against the live controller in
			// review: mode changed away from "custom" while
			// custom_services is still explicitly non-empty.
			name:               "auto_mode_with_nonempty_custom_services_errors",
			mode:               types.StringValue("auto"),
			customServices:     oneCustomService,
			predefinedServices: nullPredefinedServices,
			wantErrorAt:        pathPtr(path.Root("mdns").AtName("custom_services")),
		},
		{
			name:               "auto_mode_with_nonempty_predefined_services_errors",
			mode:               types.StringValue("auto"),
			customServices:     nullCustomServices,
			predefinedServices: onePredefinedService,
			wantErrorAt:        pathPtr(path.Root("mdns").AtName("predefined_services")),
		},
		{
			name:               "all_mode_with_nonempty_custom_services_errors",
			mode:               types.StringValue("all"),
			customServices:     oneCustomService,
			predefinedServices: nullPredefinedServices,
			wantErrorAt:        pathPtr(path.Root("mdns").AtName("custom_services")),
		},
		{
			// The shape mdnsAfterReceive's own normalization already
			// handles cleanly: explicitly emptied, not left over.
			name:               "auto_mode_with_both_lists_explicitly_empty_is_clean",
			mode:               types.StringValue("auto"),
			customServices:     emptyCustomServices,
			predefinedServices: emptyPredefinedServices,
			wantErrorAt:        nil,
		},
		{
			name:               "auto_mode_with_both_lists_null_is_clean",
			mode:               types.StringValue("auto"),
			customServices:     nullCustomServices,
			predefinedServices: nullPredefinedServices,
			wantErrorAt:        nil,
		},
		{
			name:               "unknown_mode_is_clean",
			mode:               types.StringUnknown(),
			customServices:     oneCustomService,
			predefinedServices: onePredefinedService,
			wantErrorAt:        nil,
		},
		{
			name:               "auto_mode_with_unknown_lists_is_clean",
			mode:               types.StringValue("auto"),
			customServices:     unknownCustomServices,
			predefinedServices: unknownPredefinedServices,
			wantErrorAt:        nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &fwresource.ValidateConfigResponse{}
			r.ValidateConfig(ctx, fwresource.ValidateConfigRequest{
				Config: mdnsValidateConfigFor(t, ctx, schemaResp, tt.mode, tt.customServices, tt.predefinedServices),
			}, resp)

			wantError := tt.wantErrorAt != nil
			if got := resp.Diagnostics.HasError(); got != wantError {
				t.Fatalf("mode=%v: got error=%v, want %v (diags: %v)", tt.mode, got, wantError, resp.Diagnostics)
			}
			if !wantError {
				return
			}
			var atWantPath bool
			for _, d := range resp.Diagnostics.Errors() {
				withPath, ok := d.(diag.DiagnosticWithPath)
				if ok && withPath.Path().Equal(*tt.wantErrorAt) {
					atWantPath = true
				}
			}
			if !atWantPath {
				t.Errorf("no error diagnostic at %s; got %v", *tt.wantErrorAt, resp.Diagnostics)
			}
		})
	}
}

// pathPtr is a tiny helper so the table above can express "no error
// expected" as a nil pointer rather than a zero-value path.Path that would
// need its own IsZero-style sentinel check.
func pathPtr(p path.Path) *path.Path { return &p }
