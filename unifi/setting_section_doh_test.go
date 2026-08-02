package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestDohSection_GoldenReproduction proves overlay() reproduces the Task-9
// golden PUT body (byte-identical after stripping the routing "key" field)
// for the representative model used to capture that golden (TestGolden_doh):
// state="custom", server_names=["cloudflare","google"], one custom server
// with enabled=true/sdns_stamp="sdns://AQ"/server_name="test-doh-server".
func TestDohSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := dohSection{}

	serverNames, diags := types.ListValueFrom(ctx, types.StringType, []string{"cloudflare", "google"})
	if diags.HasError() {
		t.Fatalf("building server_names list: %v", diags)
	}
	customServers, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dohCustomServerAttrTypes},
		[]settingDohCustomServerModel{{
			Enabled:    types.BoolValue(true),
			SDNSStamp:  types.StringValue("sdns://AQ"),
			ServerName: types.StringValue("test-doh-server"),
		}})
	if diags.HasError() {
		t.Fatalf("building custom_servers list: %v", diags)
	}

	m := settingDohModel{
		State:         types.StringValue("custom"),
		ServerNames:   serverNames,
		CustomServers: customServers,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, dohAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building doh object: %v", objDiags)
	}

	model := settingResourceModel{Doh: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil) // empty snapshot: section absent

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "doh" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "doh")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenDoh)
}

// TestDohSection_DecodeRoundTrip proves decode() reads a snapshot section's
// fields, including the custom_servers nested object list (through the
// generalized decodeObjectList codec) and its bool leaf, into model.Doh.
func TestDohSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := dohSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "doh"},
		Data: map[string]any{
			"state":        "custom",
			"server_names": []any{"cloudflare", "google"},
			"custom_servers": []any{
				map[string]any{
					"enabled":     true,
					"sdns_stamp":  "sdns://AQ",
					"server_name": "test-doh-server",
				},
			},
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	if model.Doh.IsNull() || model.Doh.IsUnknown() {
		t.Fatalf("model.Doh is null/unknown after decode")
	}

	var got settingDohModel
	if diags := model.Doh.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingDohModel: %v", diags)
	}

	if got.State.ValueString() != "custom" {
		t.Errorf("State = %q, want %q", got.State.ValueString(), "custom")
	}

	if got.ServerNames.IsNull() || got.ServerNames.IsUnknown() {
		t.Fatalf("ServerNames is null/unknown after decode")
	}
	var serverNames []string
	if diags := got.ServerNames.ElementsAs(ctx, &serverNames, false); diags.HasError() {
		t.Fatalf("extracting ServerNames: %v", diags)
	}
	if len(serverNames) != 2 || serverNames[0] != "cloudflare" || serverNames[1] != "google" {
		t.Errorf("ServerNames = %v, want [cloudflare google]", serverNames)
	}

	if got.CustomServers.IsNull() || got.CustomServers.IsUnknown() {
		t.Fatalf("CustomServers is null/unknown after decode")
	}
	var customServers []settingDohCustomServerModel
	if diags := got.CustomServers.ElementsAs(ctx, &customServers, false); diags.HasError() {
		t.Fatalf("extracting CustomServers: %v", diags)
	}
	if len(customServers) != 1 {
		t.Fatalf("CustomServers = %v, want 1 element", customServers)
	}
	cs := customServers[0]
	if !cs.Enabled.ValueBool() {
		t.Errorf("CustomServers[0].Enabled = %v, want true", cs.Enabled)
	}
	if cs.SDNSStamp.ValueString() != "sdns://AQ" {
		t.Errorf("CustomServers[0].SDNSStamp = %q, want %q", cs.SDNSStamp.ValueString(), "sdns://AQ")
	}
	if cs.ServerName.ValueString() != "test-doh-server" {
		t.Errorf("CustomServers[0].ServerName = %q, want %q", cs.ServerName.ValueString(), "test-doh-server")
	}
}

// TestDohSection_Preservation proves overlay() preserves an unmodeled key
// already present in the snapshot's section data.
func TestDohSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := dohSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "doh"},
		Data: map[string]any{
			"state":       "custom",
			"x_unmanaged": "keep",
		},
	}})

	serverNames, diags := types.ListValueFrom(ctx, types.StringType, []string{"cloudflare"})
	if diags.HasError() {
		t.Fatalf("building server_names list: %v", diags)
	}
	customServers, diags := types.ListValueFrom(ctx, types.ObjectType{AttrTypes: dohCustomServerAttrTypes},
		[]settingDohCustomServerModel{{
			Enabled:    types.BoolValue(true),
			SDNSStamp:  types.StringValue("sdns://AQ"),
			ServerName: types.StringValue("test-doh-server"),
		}})
	if diags.HasError() {
		t.Fatalf("building custom_servers list: %v", diags)
	}

	m := settingDohModel{
		State:         types.StringValue("custom"),
		ServerNames:   serverNames,
		CustomServers: customServers,
	}
	obj, objDiags := types.ObjectValueFrom(ctx, dohAttrTypes, m)
	if objDiags.HasError() {
		t.Fatalf("building doh object: %v", objDiags)
	}

	model := settingResourceModel{Doh: obj}
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

// TestDohSection_CustomServersListSemantics proves the object-list
// null-vs-empty contract for custom_servers: absent key decodes to a null
// list, an explicit empty array decodes to an empty (non-null) list.
func TestDohSection_CustomServersListSemantics(t *testing.T) {
	ctx := context.Background()
	sec := dohSection{}

	t.Run("absent", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "doh"},
			Data:        map[string]any{},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingDohModel
		if diags := model.Doh.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingDohModel: %v", diags)
		}

		if !got.CustomServers.IsNull() {
			t.Errorf("CustomServers = %v, want null when absent", got.CustomServers)
		}
	})

	t.Run("present empty", func(t *testing.T) {
		snap := newRawSettings([]settings.RawSetting{{
			BaseSetting: settings.BaseSetting{Key: "doh"},
			Data: map[string]any{
				"custom_servers": []any{},
			},
		}})

		prior := settingResourceModel{}
		model := settingResourceModel{}

		diags := sec.decode(ctx, snap, prior, &model)
		if diags.HasError() {
			t.Fatalf("decode diagnostics: %v", diags)
		}

		var got settingDohModel
		if diags := model.Doh.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
			t.Fatalf("extracting settingDohModel: %v", diags)
		}

		if got.CustomServers.IsNull() {
			t.Errorf("CustomServers is null, want present-empty (non-null) list")
		}
		if got.CustomServers.IsUnknown() {
			t.Errorf("CustomServers is unknown, want present-empty (non-null) list")
		}
		var customServers []settingDohCustomServerModel
		if diags := got.CustomServers.ElementsAs(ctx, &customServers, false); diags.HasError() {
			t.Fatalf("extracting CustomServers: %v", diags)
		}
		if len(customServers) != 0 {
			t.Errorf("CustomServers = %v, want empty", customServers)
		}
	})
}

// TestDohSection_NotConfigured proves overlay() returns configured == false
// and a zero-value RawSetting when the section is not configured (null
// object) in the model.
func TestDohSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := dohSection{}

	model := settingResourceModel{Doh: types.ObjectNull(dohAttrTypes)}
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

// TestDohSection_InterfaceWiring is a light structural check that the
// section is registered and key()/attrName() both return "doh" (no wire-key
// remap, unlike syslog).
func TestDohSection_InterfaceWiring(t *testing.T) {
	sec := dohSection{}
	if sec.key() != "doh" {
		t.Errorf("key() = %q, want %q", sec.key(), "doh")
	}
	if sec.attrName() != "doh" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "doh")
	}

	var foundByKey, foundByAttrName bool
	for _, s := range settingSections {
		if s.key() == "doh" {
			foundByKey = true
		}
		if s.attrName() == "doh" {
			foundByAttrName = true
		}
	}
	if !foundByKey {
		t.Errorf("no section in settingSections registry has key() == %q", "doh")
	}
	if !foundByAttrName {
		t.Errorf("no section in settingSections registry has attrName() == %q", "doh")
	}
}
