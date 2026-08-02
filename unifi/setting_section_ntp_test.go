package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestNtpSection_GoldenReproduction proves overlay() reproduces the Task-9
// golden PUT body (byte-identical after stripping the routing "key" field)
// for the representative model used to capture that golden (matching
// TestGolden_ntp): server_1/2 + setting_preference set, server_3/4 null.
func TestNtpSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := ntpSection{}

	m := settingNtpModel{
		NtpServer1:        types.StringValue("ntp1.example.com"),
		NtpServer2:        types.StringValue("ntp2.example.com"),
		NtpServer3:        types.StringNull(),
		NtpServer4:        types.StringNull(),
		SettingPreference: types.StringValue("manual"),
	}
	obj, diags := types.ObjectValueFrom(ctx, ntpAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building ntp object: %v", diags)
	}

	model := settingResourceModel{Ntp: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "ntp" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "ntp")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenNtp)
}

// TestNtpSection_DecodeRoundTrip proves decode() reads a snapshot section's
// fields into model.Ntp, and that leaves absent from the snapshot with no
// prior fall back to null.
func TestNtpSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := ntpSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data: map[string]any{
			"ntp_server_1":       "ntp1.example.com",
			"ntp_server_2":       "ntp2.example.com",
			"setting_preference": "manual",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Ntp.IsNull() || model.Ntp.IsUnknown() {
		t.Fatalf("model.Ntp is null/unknown after decode")
	}

	var got settingNtpModel
	if diags := model.Ntp.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingNtpModel: %v", diags)
	}

	if got.NtpServer1.ValueString() != "ntp1.example.com" {
		t.Errorf("NtpServer1 = %q, want %q", got.NtpServer1.ValueString(), "ntp1.example.com")
	}
	if got.NtpServer2.ValueString() != "ntp2.example.com" {
		t.Errorf("NtpServer2 = %q, want %q", got.NtpServer2.ValueString(), "ntp2.example.com")
	}
	if got.SettingPreference.ValueString() != "manual" {
		t.Errorf("SettingPreference = %q, want %q", got.SettingPreference.ValueString(), "manual")
	}
	if !got.NtpServer3.IsNull() {
		t.Errorf("NtpServer3 = %v, want null (absent from snapshot, no prior)", got.NtpServer3)
	}
	if !got.NtpServer4.IsNull() {
		t.Errorf("NtpServer4 = %v, want null (absent from snapshot, no prior)", got.NtpServer4)
	}
}

// TestNtpSection_PresentEmpty proves the present-empty codec contract
// (permitted delta 1): a snapshot with ntp_server_1:"" decodes to
// StringValue(""), never collapsed to null.
func TestNtpSection_PresentEmpty(t *testing.T) {
	ctx := context.Background()
	sec := ntpSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data: map[string]any{
			"ntp_server_1": "",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingNtpModel
	if diags := model.Ntp.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingNtpModel: %v", diags)
	}

	if got.NtpServer1.IsNull() {
		t.Errorf("NtpServer1 is null, want present-empty StringValue(\"\")")
	}
	if got.NtpServer1.ValueString() != "" {
		t.Errorf("NtpServer1 = %q, want empty string", got.NtpServer1.ValueString())
	}
}

// TestNtpSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data.
func TestNtpSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := ntpSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ntp"},
		Data: map[string]any{
			"ntp_server_1": "ntp1.example.com",
			"x_unmanaged":  "keep",
		},
	}})

	m := settingNtpModel{
		NtpServer1:        types.StringValue("ntp1.example.com"),
		NtpServer2:        types.StringNull(),
		NtpServer3:        types.StringNull(),
		NtpServer4:        types.StringNull(),
		SettingPreference: types.StringNull(),
	}
	obj, diags := types.ObjectValueFrom(ctx, ntpAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building ntp object: %v", diags)
	}

	model := settingResourceModel{Ntp: obj}
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

// TestNtpSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when the section is not configured (null
// object) in the model.
func TestNtpSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := ntpSection{}

	model := settingResourceModel{Ntp: types.ObjectNull(ntpAttrTypes)}
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

// TestNtpSection_InterfaceWiring is a light structural check that the
// section is registered and its key()/attrName() match the section name,
// matching the pattern the registry-level tests (TestRegistryKeysUnique,
// TestSectionOwnershipCoversSchema) already sweep over settingSections.
func TestNtpSection_InterfaceWiring(t *testing.T) {
	sec := ntpSection{}
	if sec.key() != "ntp" {
		t.Errorf("key() = %q, want %q", sec.key(), "ntp")
	}
	if sec.attrName() != "ntp" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "ntp")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(ntpSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("ntpSection not found in settingSections registry")
	}
}
