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

// A surface being migrated must rest at generated_shadow: the compiler emits
// its schema there and admission waits on a campaign run. What must not happen
// is a surface resting there because someone forgot it. The declaration is what
// tells those two apart, so it is required in one direction and forbidden in
// the other, and each failure has to say which.
func TestInFlightStatesRequireAMigrationDeclaration(t *testing.T) {
	const reason = "schema compiles from catalog and policy; admission awaits a campaign run"
	key := SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_policy"}

	overlay := func(state AdmissionState, migration string) StatusOverlay {
		return StatusOverlay{
			FormatVersion: 1,
			DefaultState:  LegacyAuthoritative,
			Overrides: []StatusOverride{{
				SurfaceKey: key,
				State:      state,
				Migration:  migration,
			}},
		}
	}

	declared, err := BuildLedger(releasedBaseline(t), overlay(GeneratedShadow, reason))
	if err != nil {
		t.Fatalf("a declared in-flight surface was rejected: %v", err)
	}
	if err := declared.Require(key, GeneratedShadow); err != nil {
		t.Fatal(err)
	}
	if carried := migrationFor(t, declared, key); carried != reason {
		t.Fatalf("ledger carried migration %q, want the declared reason", carried)
	}

	tests := map[string]struct {
		state     AdmissionState
		migration string
		wants     []string
	}{
		"undeclared generated_shadow": {
			state: GeneratedShadow,
			wants: []string{"unifi_firewall_policy", "generated_shadow", "in flight", "requires a migration reason"},
		},
		"undeclared adapter_parity": {
			state: AdapterParity,
			wants: []string{"unifi_firewall_policy", "adapter_parity", "in flight", "requires a migration reason"},
		},
		// A reason made of spaces is not a reason. Accepting it would leave the
		// gate passable by anyone who noticed the field was a string.
		"blank declaration": {
			state:     GeneratedShadow,
			migration: "   \t\n",
			wants:     []string{"generated_shadow", "requires a migration reason"},
		},
		// The stale direction. A settled surface carrying a migration note reads
		// as in flight to anyone who greps for it, and nothing would correct it.
		"declaration on a settled state": {
			state:     PolicyComplete,
			migration: reason,
			wants:     []string{"unifi_firewall_policy", "policy_complete", "not in flight", "must not carry a migration reason"},
		},
		"declaration on shadow_only": {
			state:     ShadowOnly,
			migration: reason,
			wants:     []string{"shadow_only", "not in flight"},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := BuildLedger(releasedBaseline(t), overlay(test.state, test.migration))
			if err == nil {
				t.Fatal("BuildLedger() succeeded")
			}
			for _, want := range test.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not say %q", err, want)
				}
			}
		})
	}

	// The default state applies to every surface at once, so no per-surface
	// declaration could cover it. An overlay that defaulted to an in-flight
	// state would put the whole estate in limbo and satisfy nothing.
	_, err = BuildLedger(releasedBaseline(t), StatusOverlay{
		FormatVersion: 1,
		DefaultState:  GeneratedShadow,
	})
	if err == nil {
		t.Fatal("BuildLedger() accepted an in-flight default state")
	}
	for _, want := range []string{"default admission state", "generated_shadow", "declared per surface"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %q", err, want)
		}
	}
}

// The declaration has to survive the round trip, or the ledger a reader parses
// would not carry the fact the checkpoint judges it by.
func TestParseLedgerEnforcesTheMigrationDeclaration(t *testing.T) {
	key := SurfaceKey{Kind: ManagedResource, Name: "unifi_firewall_policy"}
	ledger, err := BuildLedger(releasedBaseline(t), StatusOverlay{
		FormatVersion: 1,
		DefaultState:  LegacyAuthoritative,
		Overrides: []StatusOverride{{
			SurfaceKey: key,
			State:      GeneratedShadow,
			Migration:  "awaiting a campaign run",
		}},
	})
	if err != nil {
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
		t.Fatalf("ParseLedger() lost the declaration: %#v", parsed)
	}

	stripped := strings.Replace(string(encoded), `"migration":"awaiting a campaign run",`, "", 1)
	if stripped == string(encoded) {
		t.Fatal("the declaration was not present in the encoded ledger")
	}
	_, err = ParseLedger([]byte(stripped))
	if err == nil {
		t.Fatal("ParseLedger() accepted an undeclared generated_shadow entry")
	}
	for _, want := range []string{"unifi_firewall_policy", "generated_shadow", "requires a migration reason"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not say %q", err, want)
		}
	}
}

func migrationFor(t *testing.T, ledger Ledger, key SurfaceKey) string {
	t.Helper()
	for _, entry := range ledger.Entries {
		if entry.SurfaceKey == key {
			return entry.Migration
		}
	}
	t.Fatalf("surface %s/%s is missing from the ledger", key.Kind, key.Name)
	return ""
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
