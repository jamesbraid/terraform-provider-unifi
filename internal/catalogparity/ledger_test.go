package catalogparity

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const testReceiptSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestBuildLedgerExpandsEverySurface(t *testing.T) {
	baseline := releasedBaseline(t)
	overlay := StatusOverlay{
		FormatVersion: 1,
		DefaultState:  LegacyAuthoritative,
		Overrides: []StatusOverride{
			{
				SurfaceKey:    SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				State:         Admitted,
				ReceiptSHA256: testReceiptSHA256,
			},
			{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_port_forward"},
				State:      ShadowOnly,
			},
			{
				SurfaceKey: SurfaceKey{Kind: ListResource, Name: "unifi_dns_record"},
				State:      ShadowOnly,
			},
		},
	}

	ledger, err := BuildLedger(baseline, overlay)
	if err != nil {
		t.Fatal(err)
	}
	if len(ledger.Entries) != 67 {
		t.Fatalf("entries = %d, want 67", len(ledger.Entries))
	}
	if ledger.BaselineSHA256 != "1f5aff6f889192dd671e1dfd30fd0cf0e6c1b1e0addfe8664d7f17b516d77946" {
		t.Fatalf("baseline digest = %q", ledger.BaselineSHA256)
	}
	if err := ledger.Require(SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}, Admitted); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Require(SurfaceKey{Kind: ManagedResource, Name: "unifi_port_forward"}, ShadowOnly); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Require(SurfaceKey{Kind: DataSource, Name: "unifi_firewall_zone"}, LegacyAuthoritative); err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseLedger(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed, ledger) {
		t.Fatalf("ParseLedger() = %#v, want %#v", parsed, ledger)
	}
}

func TestBuildLedgerRejectsInvalidOverlay(t *testing.T) {
	valid := func() StatusOverlay {
		return StatusOverlay{
			FormatVersion: 1,
			DefaultState:  LegacyAuthoritative,
			Overrides: []StatusOverride{{
				SurfaceKey:    SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				State:         Admitted,
				ReceiptSHA256: testReceiptSHA256,
			}},
		}
	}
	tests := map[string]func(*StatusOverlay){
		"format": func(overlay *StatusOverlay) {
			overlay.FormatVersion = 2
		},
		"empty default": func(overlay *StatusOverlay) {
			overlay.DefaultState = ""
		},
		"admitted default": func(overlay *StatusOverlay) {
			overlay.DefaultState = Admitted
		},
		"duplicate override": func(overlay *StatusOverlay) {
			overlay.Overrides = append(overlay.Overrides, overlay.Overrides[0])
		},
		"stale surface": func(overlay *StatusOverlay) {
			overlay.Overrides[0].Name = "unifi_missing"
		},
		"invalid state": func(overlay *StatusOverlay) {
			overlay.Overrides[0].State = "promoted"
		},
		"missing admitted receipt": func(overlay *StatusOverlay) {
			overlay.Overrides[0].ReceiptSHA256 = ""
		},
		"invalid receipt": func(overlay *StatusOverlay) {
			overlay.Overrides[0].ReceiptSHA256 = "short"
		},
		"invalid blocking receipt": func(overlay *StatusOverlay) {
			overlay.Overrides[0].State = ShadowOnly
			overlay.Overrides[0].ReceiptSHA256 = "short"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			overlay := valid()
			mutate(&overlay)
			if _, err := BuildLedger(releasedBaseline(t), overlay); err == nil {
				t.Fatal("BuildLedger() succeeded")
			}
		})
	}
}

func TestParseStatusOverlayIsStrict(t *testing.T) {
	valid := `{"format_version":1,"default_state":"legacy_authoritative","overrides":[]}`
	overlay, err := ParseStatusOverlay([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	if overlay.DefaultState != LegacyAuthoritative {
		t.Fatalf("default state = %q", overlay.DefaultState)
	}
	for _, input := range []string{
		`{"format_version":1,"default_state":"legacy_authoritative","overrides":[],"extra":true}`,
		valid + `{}`,
	} {
		if _, err := ParseStatusOverlay([]byte(input)); err == nil {
			t.Fatalf("ParseStatusOverlay(%q) succeeded", input)
		}
	}
}

func TestLedgerRequireRejectsMissingOrWrongState(t *testing.T) {
	ledger, err := BuildLedger(releasedBaseline(t), StatusOverlay{
		FormatVersion: 1,
		DefaultState:  LegacyAuthoritative,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Require(SurfaceKey{Kind: ManagedResource, Name: "unifi_missing"}, LegacyAuthoritative); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing Require() error = %v", err)
	}
	if err := ledger.Require(SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}, Admitted); err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("wrong-state Require() error = %v", err)
	}
}
