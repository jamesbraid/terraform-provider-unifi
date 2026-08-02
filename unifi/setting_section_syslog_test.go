package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestSyslogSection_GoldenReproduction proves overlay() reproduces the
// Task-9 golden PUT body (byte-identical after stripping the routing "key"
// field) for the representative model used to capture that golden, EXCEPT
// NetconsoleHost is fed as NULL (not ""): overlayString skips null/unknown,
// which is what reproduces goldenSyslog's omission of netconsole_host (the
// legacy typed converter dropped it via omitempty). This is the
// empty-vs-absent delta documented in the task brief.
func TestSyslogSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	contents, diags := types.ListValueFrom(ctx, types.StringType, []string{"device", "client"})
	if diags.HasError() {
		t.Fatalf("building contents list: %v", diags)
	}

	m := settingSyslogModel{
		Enabled:                     types.BoolValue(true),
		Contents:                    contents,
		Debug:                       types.BoolValue(false),
		IP:                          types.StringValue("192.0.2.10"),
		Port:                        types.Int64Value(514),
		LogAllContents:              types.BoolValue(false),
		NetconsoleEnabled:           types.BoolValue(false),
		NetconsoleHost:              types.StringNull(), // see (B) in the task brief
		NetconsolePort:              types.Int64Value(6514),
		ThisController:              types.BoolValue(true),
		ThisControllerEncryptedOnly: types.BoolValue(false),
	}
	obj, objDiags := types.ObjectValueFrom(ctx, syslogAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building syslog object: %v", objDiags)
	}

	model := settingResourceModel{Syslog: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "rsyslogd" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "rsyslogd")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenSyslog)
}

// TestSyslogSection_DecodeRoundTrip proves decode() reads a snapshot
// section's fields (keyed by the controller key "rsyslogd") into
// model.Syslog, including the contents string list.
func TestSyslogSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "rsyslogd"},
		Data: map[string]any{
			"enabled":                        true,
			"contents":                       []any{"device", "client"},
			"debug":                          false,
			"ip":                             "192.0.2.10",
			"port":                           float64(514),
			"log_all_contents":               false,
			"this_controller":                true,
			"this_controller_encrypted_only": false,
			"netconsole_enabled":             false,
			"netconsole_host":                "console.example.com",
			"netconsole_port":                float64(6514),
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Syslog.IsNull() || model.Syslog.IsUnknown() {
		t.Fatalf("model.Syslog is null/unknown after decode")
	}

	var got settingSyslogModel
	if diags := model.Syslog.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingSyslogModel: %v", diags)
	}

	if !got.Enabled.ValueBool() {
		t.Errorf("Enabled = %v, want true", got.Enabled)
	}
	if got.IP.ValueString() != "192.0.2.10" {
		t.Errorf("IP = %q, want %q", got.IP.ValueString(), "192.0.2.10")
	}
	if got.Port.ValueInt64() != 514 {
		t.Errorf("Port = %d, want 514", got.Port.ValueInt64())
	}
	if got.NetconsolePort.ValueInt64() != 6514 {
		t.Errorf("NetconsolePort = %d, want 6514", got.NetconsolePort.ValueInt64())
	}
	if got.NetconsoleHost.ValueString() != "console.example.com" {
		t.Errorf("NetconsoleHost = %q, want %q", got.NetconsoleHost.ValueString(), "console.example.com")
	}

	if got.Contents.IsNull() || got.Contents.IsUnknown() {
		t.Fatalf("Contents is null/unknown after decode")
	}
	var contents []string
	if diags := got.Contents.ElementsAs(ctx, &contents, false); diags.HasError() {
		t.Fatalf("extracting Contents: %v", diags)
	}
	if len(contents) != 2 || contents[0] != "device" || contents[1] != "client" {
		t.Errorf("Contents = %v, want [device client]", contents)
	}
}

// TestSyslogSection_PresentEmpty proves the present-empty codec contract
// (permitted delta 1): a snapshot with ip:"" decodes to StringValue(""),
// never collapsed to null.
func TestSyslogSection_PresentEmpty(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "rsyslogd"},
		Data: map[string]any{
			"ip": "",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingSyslogModel
	if diags := model.Syslog.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingSyslogModel: %v", diags)
	}

	if got.IP.IsNull() {
		t.Errorf("IP is null, want present-empty StringValue(\"\")")
	}
	if got.IP.ValueString() != "" {
		t.Errorf("IP = %q, want empty string", got.IP.ValueString())
	}
}

// TestSyslogSection_ContentsEmptyList proves the list analogue of the
// present-empty delta: an explicit empty JSON array decodes to an empty
// (non-null) list, while an absent key decodes to null.
func TestSyslogSection_ContentsEmptyList(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	t.Run("present empty", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "rsyslogd"},
			Data: map[string]any{
				"contents": []any{},
			},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingSyslogModel
		if diags := model.Syslog.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingSyslogModel: %v", diags)
		}

		if got.Contents.IsNull() {
			t.Errorf("Contents is null, want present-empty (non-null) list")
		}
		if got.Contents.IsUnknown() {
			t.Errorf("Contents is unknown, want present-empty (non-null) list")
		}
		var contents []string
		if diags := got.Contents.ElementsAs(ctx, &contents, false); diags.HasError() {
			t.Fatalf("extracting Contents: %v", diags)
		}
		if len(contents) != 0 {
			t.Errorf("Contents = %v, want empty", contents)
		}
	})

	t.Run("absent", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "rsyslogd"},
			Data:        map[string]any{},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingSyslogModel
		if diags := model.Syslog.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingSyslogModel: %v", diags)
		}

		if !got.Contents.IsNull() {
			t.Errorf("Contents = %v, want null when absent", got.Contents)
		}
	})
}

// TestSyslogSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data (keyed by "rsyslogd").
func TestSyslogSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "rsyslogd"},
		Data: map[string]any{
			"enabled":     true,
			"ip":          "192.0.2.10",
			"x_unmanaged": "keep",
		},
	}})

	contents, diags := types.ListValueFrom(ctx, types.StringType, []string{"device"})
	if diags.HasError() {
		t.Fatalf("building contents list: %v", diags)
	}
	m := settingSyslogModel{
		Enabled:                     types.BoolValue(true),
		Contents:                    contents,
		Debug:                       types.BoolValue(false),
		IP:                          types.StringValue("192.0.2.10"),
		Port:                        types.Int64Value(514),
		LogAllContents:              types.BoolValue(false),
		NetconsoleEnabled:           types.BoolValue(false),
		NetconsoleHost:              types.StringNull(),
		NetconsolePort:              types.Int64Value(6514),
		ThisController:              types.BoolValue(true),
		ThisControllerEncryptedOnly: types.BoolValue(false),
	}
	obj, objDiags := types.ObjectValueFrom(ctx, syslogAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building syslog object: %v", objDiags)
	}

	model := settingResourceModel{Syslog: obj}
	prior := settingResourceModel{}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}

	if got, ok := rs.Data["x_unmanaged"]; !ok || got != "keep" {
		t.Errorf("rs.Data[%q] = %v (ok=%v), want %q", "x_unmanaged", got, ok, "keep")
	}
}

// TestSyslogSection_NotConfigured proves overlay() returns configured ==
// false and a zero-value RawSetting when the section is not configured
// (null object) in the model.
func TestSyslogSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := syslogSection{}

	model := settingResourceModel{Syslog: types.ObjectNull(syslogAttrTypes)}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if configured {
		t.Fatalf("overlay configured = true, want false")
	}
	if rs.Key != "" || len(rs.Data) != 0 {
		t.Errorf("overlay returned non-zero RawSetting when not configured: %+v", rs)
	}
}

// TestSyslogSection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() diverge correctly (the
// controller key "rsyslogd" vs. the Terraform attribute "syslog"), matching
// the pattern the registry-level tests (TestRegistryKeysUnique,
// TestSectionOwnershipCoversSchema) already sweep over settingSections.
func TestSyslogSection_InterfaceWiring(t *testing.T) {
	sec := syslogSection{}
	if sec.key() != "rsyslogd" {
		t.Errorf("key() = %q, want %q", sec.key(), "rsyslogd")
	}
	if sec.attrName() != "syslog" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "syslog")
	}

	var foundByKey, foundByAttrName bool
	for _, s := range settingSections {
		if s.key() == "rsyslogd" {
			foundByKey = true
		}
		if s.attrName() == "syslog" {
			foundByAttrName = true
		}
	}
	if !foundByKey {
		t.Errorf("no section in settingSections registry has key() == %q", "rsyslogd")
	}
	if !foundByAttrName {
		t.Errorf("no section in settingSections registry has attrName() == %q", "syslog")
	}
}
