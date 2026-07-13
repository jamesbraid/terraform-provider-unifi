package unifi

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

const goldenSslInspection = `{"key":"ssl_inspection","state":"simple"}`

func TestSslInspectionSection_GoldenReproduction(t *testing.T) {
	ctx := context.Background()
	sec := sslInspectionSection{}

	m := settingSslInspectionModel{State: types.StringValue("simple")}
	obj, diags := types.ObjectValueFrom(ctx, sslInspectionAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building ssl_inspection object: %v", diags)
	}

	model := settingResourceModel{SslInspection: obj}
	prior := settingResourceModel{}
	snap := newRawSettings(nil)

	rs, configured, oDiags := sec.overlay(ctx, model, prior, snap)
	if oDiags.HasError() {
		t.Fatalf("overlay diagnostics: %v", oDiags)
	}
	if !configured {
		t.Fatalf("overlay configured = false, want true")
	}
	if rs.Key != "ssl_inspection" {
		t.Errorf("rs.Key = %q, want %q", rs.Key, "ssl_inspection")
	}

	assertPUTBodyMatchesGolden(t, rs, goldenSslInspection)
}

func TestSslInspectionSection_DecodeRoundTrip(t *testing.T) {
	ctx := context.Background()
	sec := sslInspectionSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ssl_inspection"},
		Data: map[string]any{
			"state": "simple",
		},
	}})

	prior := settingResourceModel{}
	model := settingResourceModel{}

	diags := sec.decode(ctx, snap, prior, &model)
	if diags.HasError() {
		t.Fatalf("decode diagnostics: %v", diags)
	}

	var got settingSslInspectionModel
	if diags := model.SslInspection.As(ctx, &got, basetypes.ObjectAsOptions{}); diags.HasError() {
		t.Fatalf("extracting settingSslInspectionModel: %v", diags)
	}

	if got.State.ValueString() != "simple" {
		t.Errorf("State = %q, want %q", got.State.ValueString(), "simple")
	}
}

func TestSslInspectionSection_Preservation(t *testing.T) {
	ctx := context.Background()
	sec := sslInspectionSection{}

	snap := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "ssl_inspection"},
		Data: map[string]any{
			"state":       "simple",
			"x_unmanaged": "keep",
		},
	}})

	m := settingSslInspectionModel{State: types.StringValue("simple")}
	obj, diags := types.ObjectValueFrom(ctx, sslInspectionAttrTypes, m)
	if diags.HasError() {
		t.Fatalf("building ssl_inspection object: %v", diags)
	}

	model := settingResourceModel{SslInspection: obj}
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

func TestSslInspectionSection_NotConfigured(t *testing.T) {
	ctx := context.Background()
	sec := sslInspectionSection{}

	model := settingResourceModel{SslInspection: types.ObjectNull(sslInspectionAttrTypes)}
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

func TestSslInspectionSection_InterfaceWiring(t *testing.T) {
	sec := sslInspectionSection{}
	if sec.key() != "ssl_inspection" {
		t.Errorf("key() = %q, want %q", sec.key(), "ssl_inspection")
	}
	if sec.attrName() != "ssl_inspection" {
		t.Errorf("attrName() = %q, want %q", sec.attrName(), "ssl_inspection")
	}

	var found bool
	for _, s := range settingSections {
		if _, ok := s.(sslInspectionSection); ok {
			found = true
		}
	}
	if !found {
		t.Errorf("sslInspectionSection not found in settingSections registry")
	}
}
