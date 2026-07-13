package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// goldenMdns is this section's fresh golden PUT body constant (mode=custom,
// one predefined code, one custom service), captured from overlay()'s
// actual output against an empty snapshot — see
// TestMdnsSection_GoldenReproduction.
const goldenMdns = `{
  "key": "mdns",
  "mode": "custom",
  "predefined_services": [{"code": "apple_airPlay"}, {"code": "printers"}],
  "custom_services": [{"name": "_myservice._tcp", "address": "_myservice._tcp.local"}]
}`

// TestMdnsSection_GoldenReproduction proves overlay() reproduces this
// section's golden PUT body for the representative "mode=custom" model.
func TestMdnsSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	predefined, diags := types.ListValueFrom(ctx, types.StringType, []string{"apple_airPlay", "printers"})
	if diags.HasError() {
		t.Fatalf("building predefined_services: %v", diags)
	}

	customList, cDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, []settingMdnsCustomServiceModel{
		{Address: types.StringValue("_myservice._tcp.local"), Name: types.StringValue("_myservice._tcp")},
	})
	if cDiags.HasError() {
		t.Fatalf("building custom_services: %v", cDiags)
	}

	m := settingMdnsModel{
		Mode:               types.StringValue("custom"),
		PredefinedServices: predefined,
		CustomServices:     customList,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, mdnsAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building mdns object: %v", objDiags)
	}

	model := settingResourceModel{Mdns: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "mdns" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "mdns")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenMdns)
}

// TestMdnsSection_DecodeRoundTrip proves decode() reads mode, predefined
// codes, and custom services from a "custom"-mode snapshot.
func TestMdnsSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mdns"},
		Data: map[string]any{
			"mode":                "custom",
			"predefined_services": []any{map[string]any{"code": "apple_airPlay"}, map[string]any{"code": "printers"}},
			"custom_services":     []any{map[string]any{"name": "_myservice._tcp", "address": "_myservice._tcp.local"}},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Mdns.IsNull() || model.Mdns.IsUnknown() {
		t.Fatalf("model.Mdns is null/unknown after decode")
	}

	var got settingMdnsModel
	if diags := model.Mdns.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingMdnsModel: %v", diags)
	}

	if got.Mode.ValueString() != "custom" {
		t.Errorf("Mode = %q, want %q", got.Mode.ValueString(), "custom")
	}

	var predefined []string
	if diags := got.PredefinedServices.ElementsAs(ctx, &predefined, false); diags.HasError() {
		t.Fatalf("extracting PredefinedServices: %v", diags)
	}
	if len(predefined) != 2 || predefined[0] != "apple_airPlay" || predefined[1] != "printers" {
		t.Errorf("PredefinedServices = %v, want [apple_airPlay printers]", predefined)
	}

	var custom []settingMdnsCustomServiceModel
	if diags := got.CustomServices.ElementsAs(ctx, &custom, false); diags.HasError() {
		t.Fatalf("extracting CustomServices: %v", diags)
	}
	if len(custom) != 1 || custom[0].Name.ValueString() != "_myservice._tcp" || custom[0].Address.ValueString() != "_myservice._tcp.local" {
		t.Errorf("CustomServices = %+v, want [{_myservice._tcp _myservice._tcp.local}]", custom)
	}
}

// TestMdnsSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data.
func TestMdnsSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mdns"},
		Data: map[string]any{
			"x_unmanaged": "keep",
		},
	}})

	predefined, diags := types.ListValueFrom(ctx, types.StringType, []string{"printers"})
	if diags.HasError() {
		t.Fatalf("building predefined_services: %v", diags)
	}
	customList, cDiags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: mdnsCustomServiceAttrTypes}, []settingMdnsCustomServiceModel{})
	if cDiags.HasError() {
		t.Fatalf("building custom_services: %v", cDiags)
	}

	m := settingMdnsModel{
		Mode:               types.StringValue("custom"),
		PredefinedServices: predefined,
		CustomServices:     customList,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, mdnsAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building mdns object: %v", objDiags)
	}

	model := settingResourceModel{Mdns: obj}
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

// TestMdnsSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when model.Mdns is null.
func TestMdnsSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	model := settingResourceModel{Mdns: types.ObjectNull(mdnsAttrTypes)}
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

// TestMdnsSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "mdns".
func TestMdnsSection_InterfaceWiring(t *testing.T) {
	sec := mdnsSection{}
	if sec.key() != "mdns" {
		t.Errorf("key() = %q, want %q", sec.key(), "mdns")
	}
	if sec.attrName() != "mdns" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "mdns")
	}

	var found bool
	for _, s := range settingSections {
		if s.key() == "mdns" && s.attrName() == "mdns" {
			found = true
		}
	}
	if !found {
		t.Errorf("no section in settingSections registry has key()==attrName()==%q", "mdns")
	}
}

// TestMdnsSection_ModeDiscriminatorNormalization proves that decode() reads
// predefined_services/custom_services from the wire when mode == "custom",
// but overlays an empty (not null) list into state/wire when mode !=
// "custom" even if the controller's snapshot still holds stale values (e.g.
// left over from a prior "custom" period).
func TestMdnsSection_ModeDiscriminatorNormalization(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	// Controller snapshot: mode is "auto", but predefined_services/
	// custom_services still hold a stale array from a prior "custom"
	// period. decode() must NOT echo these back as live config.
	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mdns"},
		Data: map[string]any{
			"mode":                "auto",
			"predefined_services": []any{map[string]any{"code": "printers"}},
			"custom_services":     []any{map[string]any{"name": "_stale._tcp", "address": "_stale._tcp.local"}},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingMdnsModel
	if diags := model.Mdns.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingMdnsModel: %v", diags)
	}

	if got.Mode.ValueString() != "auto" {
		t.Errorf("Mode = %q, want %q", got.Mode.ValueString(), "auto")
	}
	if got.PredefinedServices.IsNull() {
		t.Error("PredefinedServices is null, want empty-not-null when mode != custom")
	}
	if len(got.PredefinedServices.Elements()) != 0 {
		t.Errorf("PredefinedServices = %v, want empty when mode != custom (stale controller value must not leak into state)", got.PredefinedServices.Elements())
	}
	if got.CustomServices.IsNull() {
		t.Error("CustomServices is null, want empty-not-null when mode != custom")
	}
	if len(got.CustomServices.Elements()) != 0 {
		t.Errorf("CustomServices = %v, want empty when mode != custom (stale controller value must not leak into state)", got.CustomServices.Elements())
	}

	// overlay() must also normalize outbound, independent of what state/
	// config holds: mode=auto sends empty lists, not whatever's in cfg.
	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatal("overlay configured = false, want true")
	}
	if ps, ok := rs.Data["predefined_services"].([]any); !ok || len(ps) != 0 {
		t.Errorf("rs.Data[predefined_services] = %v, want empty array when mode=auto", rs.Data["predefined_services"])
	}
	if cs, ok := rs.Data["custom_services"].([]any); !ok || len(cs) != 0 {
		t.Errorf("rs.Data[custom_services] = %v, want empty array when mode=auto", rs.Data["custom_services"])
	}
}

// TestMdnsSection_ModeCustomReadsLiveLists proves the reverse direction:
// mode == "custom" reads/writes predefined_services/custom_services live.
func TestMdnsSection_ModeCustomReadsLiveLists(t *testing.T) {
	ctx := context.Background()
	sec := mdnsSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "mdns"},
		Data: map[string]any{
			"mode":                "custom",
			"predefined_services": []any{map[string]any{"code": "printers"}},
			"custom_services":     []any{map[string]any{"name": "_myservice._tcp", "address": "_myservice._tcp.local"}},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingMdnsModel
	if diags := model.Mdns.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingMdnsModel: %v", diags)
	}

	if len(got.PredefinedServices.Elements()) != 1 {
		t.Errorf("PredefinedServices = %v, want 1 element under mode=custom", got.PredefinedServices.Elements())
	}
	if len(got.CustomServices.Elements()) != 1 {
		t.Errorf("CustomServices = %v, want 1 element under mode=custom", got.CustomServices.Elements())
	}

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatal("overlay configured = false, want true")
	}
	if ps, ok := rs.Data["predefined_services"].([]any); !ok || len(ps) != 1 {
		t.Errorf("rs.Data[predefined_services] = %v, want 1 element under mode=custom", rs.Data["predefined_services"])
	}
	if cs, ok := rs.Data["custom_services"].([]any); !ok || len(cs) != 1 {
		t.Errorf("rs.Data[custom_services] = %v, want 1 element under mode=custom", rs.Data["custom_services"])
	}
}
