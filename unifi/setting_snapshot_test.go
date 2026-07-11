package unifi

import (
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func TestRawSettings_dataCopyIsDeep(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "x"},
		Data:        map[string]any{"nested": map[string]any{"a": float64(1)}},
	}})
	cp := rs.dataCopy("x")
	cp["nested"].(map[string]any)["a"] = float64(2)
	orig, _ := rs.section("x")
	if orig.Data["nested"].(map[string]any)["a"] != float64(1) {
		t.Fatalf("dataCopy must be deep; snapshot was mutated")
	}
}

func TestRawSettings_sectionAndHas(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{
		{
			BaseSetting: settings.BaseSetting{Key: "mgmt"},
			Data:        map[string]any{"foo": "bar"},
		},
		{
			BaseSetting: settings.BaseSetting{Key: "usg"},
			Data:        map[string]any{"baz": float64(42)},
		},
	})

	if !rs.has("mgmt") {
		t.Errorf("has(%q) = false, want true", "mgmt")
	}
	if !rs.has("usg") {
		t.Errorf("has(%q) = false, want true", "usg")
	}
	if rs.has("nonexistent") {
		t.Errorf("has(%q) = true, want false", "nonexistent")
	}

	sec, ok := rs.section("mgmt")
	if !ok {
		t.Fatalf("section(%q) ok = false, want true", "mgmt")
	}
	if sec.Key != "mgmt" {
		t.Errorf("section(%q).Key = %q, want %q", "mgmt", sec.Key, "mgmt")
	}
	if sec.Data["foo"] != "bar" {
		t.Errorf("section(%q).Data[%q] = %v, want %q", "mgmt", "foo", sec.Data["foo"], "bar")
	}

	if _, ok := rs.section("nonexistent"); ok {
		t.Errorf("section(%q) ok = true, want false", "nonexistent")
	}
}

func TestRawSettings_dataCopyAbsentKey(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{{
		BaseSetting: settings.BaseSetting{Key: "x"},
		Data:        map[string]any{"a": float64(1)},
	}})

	cp := rs.dataCopy("nonexistent")
	if cp == nil {
		t.Fatalf("dataCopy for absent key = nil, want non-nil empty map")
	}
	if len(cp) != 0 {
		t.Errorf("dataCopy for absent key = %v, want empty map", cp)
	}

	// Must be safe to overlay onto without a nil-map panic.
	cp["new"] = "value"
	if cp["new"] != "value" {
		t.Errorf("overlay onto absent-key dataCopy failed")
	}
}
