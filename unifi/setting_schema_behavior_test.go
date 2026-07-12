package unifi

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// setting_schema_behavior_test.go covers what setting_schema_equiv_test.go's
// description-equality golden cannot: that a registry-built section's
// validators/defaults/plan-modifiers actually BEHAVE correctly, not just
// describe themselves identically to the legacy block. These drive the real
// built schema (via settingResource.Schema, the post-Task-24a rewire) rather
// than a section's schemaAttribute() in isolation, so a wiring bug (e.g. a
// section attached under the wrong attrName) would also be caught here too.

// builtSchema returns the current setting resource schema's Attributes map.
func builtSchema(t *testing.T) map[string]schema.Attribute {
	t.Helper()
	r := &settingResource{}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema() produced diagnostics: %v", resp.Diagnostics)
	}
	return resp.Schema.Attributes
}

// nestedAttr looks up a leaf attribute path.path... under top (a
// SingleNestedAttribute), e.g. nestedAttr(t, attrs, "ntp", "setting_preference").
func nestedAttr(t *testing.T, attrs map[string]schema.Attribute, top string, leaf string) schema.Attribute {
	t.Helper()
	a, ok := attrs[top]
	if !ok {
		t.Fatalf("schema missing top-level attribute %q", top)
	}
	sn, ok := a.(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("attribute %q is %T, want schema.SingleNestedAttribute", top, a)
	}
	child, ok := sn.Attributes[leaf]
	if !ok {
		t.Fatalf("attribute %q has no child %q", top, leaf)
	}
	return child
}

// validateStringAll runs every validator in vs against value at p, returning
// the accumulated diagnostics.
func validateStringAll(ctx context.Context, vs []validator.String, p path.Path, value string) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, v := range vs {
		req := validator.StringRequest{Path: p, ConfigValue: types.StringValue(value)}
		var resp validator.StringResponse
		v.ValidateString(ctx, req, &resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

// --- validator rejection ---------------------------------------------------

func TestSettingSchemaBehavior_ntpSettingPreferenceRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "ntp", "setting_preference")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("ntp.setting_preference is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("ntp.setting_preference has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"auto is valid", "auto", false},
		{"manual is valid", "manual", false},
		{"garbage is invalid", "sometimes", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("ntp").AtName("setting_preference"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_ipsModeRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "ips", "ips_mode")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("ips.ips_mode is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("ips.ips_mode has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"ids is valid", "ids", false},
		{"ips is valid", "ips", false},
		{"ipsInline is valid", "ipsInline", false},
		{"disabled is valid", "disabled", false},
		{"garbage is invalid", "yolo", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("ips").AtName("ips_mode"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_globalNatModeRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "global_nat", "mode")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("global_nat.mode is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("global_nat.mode has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"auto is valid", "auto", false},
		{"custom is valid", "custom", false},
		{"off is valid", "off", false},
		{"garbage is invalid", "bridge", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("global_nat").AtName("mode"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_sslInspectionStateRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "ssl_inspection", "state")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("ssl_inspection.state is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("ssl_inspection.state has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"off is valid", "off", false},
		{"simple is valid", "simple", false},
		{"advanced is valid", "advanced", false},
		{"garbage is invalid", "full", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("ssl_inspection").AtName("state"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

// validateInt64All runs every validator in vs against value at p, returning
// the accumulated diagnostics.
func validateInt64All(ctx context.Context, vs []validator.Int64, p path.Path, value int64) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, v := range vs {
		req := validator.Int64Request{Path: p, ConfigValue: types.Int64Value(value)}
		var resp validator.Int64Response
		v.ValidateInt64(ctx, req, &resp)
		diags.Append(resp.Diagnostics...)
	}
	return diags
}

func TestSettingSchemaBehavior_netflowSamplingModeRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "netflow", "sampling_mode")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("netflow.sampling_mode is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("netflow.sampling_mode has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"off is valid", "off", false},
		{"hash is valid", "hash", false},
		{"random is valid", "random", false},
		{"deterministic is valid", "deterministic", false},
		{"garbage is invalid", "always", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("netflow").AtName("sampling_mode"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_netflowVersionRejectsInvalid(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "netflow", "version")
	ia, ok := a.(schema.Int64Attribute)
	if !ok {
		t.Fatalf("netflow.version is %T, want schema.Int64Attribute", a)
	}
	if len(ia.Validators) == 0 {
		t.Fatal("netflow.version has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   int64
		wantErr bool
	}{
		{"5 is valid", 5, false},
		{"9 is valid", 9, false},
		{"10 is valid", 10, false},
		{"7 is invalid", 7, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateInt64All(ctx, ia.Validators, path.Root("netflow").AtName("version"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %d: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_netflowPortRejectsOutOfRange(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "netflow", "port")
	ia, ok := a.(schema.Int64Attribute)
	if !ok {
		t.Fatalf("netflow.port is %T, want schema.Int64Attribute", a)
	}
	if len(ia.Validators) == 0 {
		t.Fatal("netflow.port has no validators")
	}

	for _, tc := range []struct {
		name    string
		value   int64
		wantErr bool
	}{
		{"1024 (lower bound) is valid", 1024, false},
		{"65535 (upper bound) is valid", 65535, false},
		{"2055 is valid", 2055, false},
		{"1023 is too low", 1023, true},
		{"65536 is too high", 65536, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateInt64All(ctx, ia.Validators, path.Root("netflow").AtName("port"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %d: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

func TestSettingSchemaBehavior_radiusSecretRejectsTooLong(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "radius", "secret")
	sa, ok := a.(schema.StringAttribute)
	if !ok {
		t.Fatalf("radius.secret is %T, want schema.StringAttribute", a)
	}
	if len(sa.Validators) == 0 {
		t.Fatal("radius.secret has no validators")
	}

	tooLong := strings.Repeat("x", 49) // LengthBetween(1, 48): 49 chars must fail

	for _, tc := range []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"48 chars is valid", tooLong[:48], false},
		{"49 chars is too long", tooLong, true},
		{"empty is too short", "", true},
		{"contains a space is invalid", "has space", true},
		{"contains a backslash is invalid", `back\slash`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			diags := validateStringAll(ctx, sa.Validators, path.Root("radius").AtName("secret"), tc.value)
			if got := diags.HasError(); got != tc.wantErr {
				t.Errorf("value %q: validator error = %v, want %v (diags: %v)", tc.value, got, tc.wantErr, diags)
			}
		})
	}
}

// --- default application ----------------------------------------------------

func TestSettingSchemaBehavior_autoSpeedtestEnabledDefaultsFalse(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "auto_speedtest", "enabled")
	ba, ok := a.(schema.BoolAttribute)
	if !ok {
		t.Fatalf("auto_speedtest.enabled is %T, want schema.BoolAttribute", a)
	}
	if ba.Default == nil {
		t.Fatal("auto_speedtest.enabled has no Default")
	}
	assertBoolDefault(t, ctx, ba.Default, false)
}

func TestSettingSchemaBehavior_syslogEnabledDefaultsFalse(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "syslog", "enabled")
	ba, ok := a.(schema.BoolAttribute)
	if !ok {
		t.Fatalf("syslog.enabled is %T, want schema.BoolAttribute", a)
	}
	if ba.Default == nil {
		t.Fatal("syslog.enabled has no Default")
	}
	assertBoolDefault(t, ctx, ba.Default, false)
}

func TestSettingSchemaBehavior_lcmEnabledDefaultsTrue(t *testing.T) {
	// lcm.enabled defaults to true (unlike most other bool leaves in this
	// schema, which default false) — worth pinning explicitly so a
	// copy-paste of another section's default doesn't silently flip it.
	ctx := context.Background()
	attrs := builtSchema(t)
	a := nestedAttr(t, attrs, "lcm", "enabled")
	ba, ok := a.(schema.BoolAttribute)
	if !ok {
		t.Fatalf("lcm.enabled is %T, want schema.BoolAttribute", a)
	}
	if ba.Default == nil {
		t.Fatal("lcm.enabled has no Default")
	}
	assertBoolDefault(t, ctx, ba.Default, true)
}

func assertBoolDefault(t *testing.T, ctx context.Context, d defaults.Bool, want bool) {
	t.Helper()
	var resp defaults.BoolResponse
	d.DefaultBool(ctx, defaults.BoolRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("DefaultBool produced diagnostics: %v", resp.Diagnostics)
	}
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		t.Fatalf("DefaultBool produced null/unknown, want %v", want)
	}
	if got := resp.PlanValue.ValueBool(); got != want {
		t.Errorf("default = %v, want %v", got, want)
	}
}

// --- plan-modifier no-churn --------------------------------------------------

// TestSettingSchemaBehavior_autoSpeedtestUseStateForUnknownNoChurn exercises
// auto_speedtest's UseStateForUnknown object plan modifier directly: a prior
// known state value survives onto an unknown plan value (the framework's
// "this section wasn't touched by the edit" case) unchanged, so an edit to
// an unrelated attribute causes no diff/churn for a section the user didn't
// configure this apply.
func TestSettingSchemaBehavior_autoSpeedtestUseStateForUnknownNoChurn(t *testing.T) {
	ctx := context.Background()
	attrs := builtSchema(t)

	top, ok := attrs["auto_speedtest"].(schema.SingleNestedAttribute)
	if !ok {
		t.Fatalf("auto_speedtest is %T, want schema.SingleNestedAttribute", attrs["auto_speedtest"])
	}
	if len(top.PlanModifiers) == 0 {
		t.Fatal("auto_speedtest has no object plan modifiers")
	}

	attrTypes := map[string]attr.Type{
		"enabled":   types.BoolType,
		"cron_expr": types.StringType,
	}
	priorObj := types.ObjectValueMust(attrTypes, map[string]attr.Value{
		"enabled":   types.BoolValue(true),
		"cron_expr": types.StringValue("0 * * * *"),
	})

	tfObjType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"enabled":   tftypes.Bool,
		"cron_expr": tftypes.String,
	}}
	stateRaw := tftypes.NewValue(
		tftypes.Object{AttributeTypes: map[string]tftypes.Type{"auto_speedtest": tfObjType}},
		map[string]tftypes.Value{
			"auto_speedtest": tftypes.NewValue(tfObjType, map[string]tftypes.Value{
				"enabled":   tftypes.NewValue(tftypes.Bool, true),
				"cron_expr": tftypes.NewValue(tftypes.String, "0 * * * *"),
			}),
		},
	)

	for _, pm := range top.PlanModifiers {
		req := planmodifier.ObjectRequest{
			Path:        path.Root("auto_speedtest"),
			State:       tfsdk.State{Raw: stateRaw},
			StateValue:  priorObj,
			PlanValue:   types.ObjectUnknown(attrTypes), // framework proposes unknown for a Computed attr not in config
			ConfigValue: types.ObjectNull(attrTypes),    // unrelated edit: this section absent from config
		}
		var resp planmodifier.ObjectResponse
		resp.PlanValue = req.PlanValue
		pm.PlanModifyObject(ctx, req, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("PlanModifyObject produced diagnostics: %v", resp.Diagnostics)
		}
		if !resp.PlanValue.Equal(priorObj) {
			t.Errorf("PlanModifyObject: plan value = %v, want unchanged prior state %v (no-churn expected)", resp.PlanValue, priorObj)
		}
	}
}
