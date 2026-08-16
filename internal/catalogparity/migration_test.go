package catalogparity

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseSchemaVersionsBindsCanonicalSchema(t *testing.T) {
	baseline := releasedBaseline(t)
	versions, err := ParseSchemaVersions(canonicalSchemaFixture(t), baseline)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions.Versions) != 67 {
		t.Fatalf("versions = %d, want 67", len(versions.Versions))
	}
	assertSchemaVersion(t, versions, SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"}, 1)
	assertSchemaVersion(t, versions, SurfaceKey{Kind: ManagedResource, Name: "unifi_device"}, 2)
	if version := versions.Versions[SurfaceKey{Kind: Action, Name: "unifi_port"}]; version != nil {
		t.Fatalf("action schema version = %d, want nil", *version)
	}

	mutated := strings.Replace(string(canonicalSchemaFixture(t)), `"version":2`, `"version":3`, 1)
	if _, err := ParseSchemaVersions([]byte(mutated), baseline); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("ParseSchemaVersions() error = %v, want digest failure", err)
	}
}

func TestExpandMigrationRecordsEveryReleasedSurface(t *testing.T) {
	baseline := releasedBaseline(t)
	versions, err := ParseSchemaVersions(canonicalSchemaFixture(t), baseline)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ExpandMigration(baseline, versions, validMigrationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) != 67 {
		t.Fatalf("entries = %d, want 67", len(manifest.Entries))
	}
	for _, entry := range manifest.Entries {
		if entry.Strategy != IdentityTransform || entry.OldName != entry.NewName || entry.DestructiveRisk != RiskNone {
			t.Fatalf("unsafe identity entry: %#v", entry)
		}
		if !reflect.DeepEqual(entry.ForwardAssertions, defaultForwardAssertions()) {
			t.Fatalf("forward assertions = %#v", entry.ForwardAssertions)
		}
		if entry.Recovery.Mode != "snapshot_restore" {
			t.Fatalf("recovery = %#v", entry.Recovery)
		}
	}
	report, err := BuildMigrationReport(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if report.SurfaceCount != 67 || report.StrategyCounts[string(IdentityTransform)] != 67 || report.RiskCounts[string(RiskNone)] != 67 {
		t.Fatalf("report = %#v", report)
	}
	if len(report.StateCommands) != 0 {
		t.Fatalf("state commands = %#v, want none", report.StateCommands)
	}
}

func TestExpandMigrationRejectsUnsafePolicy(t *testing.T) {
	baseline := releasedBaseline(t)
	versions, err := ParseSchemaVersions(canonicalSchemaFixture(t), baseline)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(*MigrationPolicy){
		"provider address": func(policy *MigrationPolicy) {
			policy.ProviderAddress = "registry.example.invalid/other/unifi"
		},
		"from version": func(policy *MigrationPolicy) {
			policy.FromVersion = "0.100.0"
		},
		"default strategy": func(policy *MigrationPolicy) {
			policy.DefaultStrategy = AddressRecipe
		},
		"unknown surface": func(policy *MigrationPolicy) {
			policy.Overrides = []MigrationOverride{{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_missing"},
				Strategy:   IdentityTransform,
			}}
		},
		"duplicate override": func(policy *MigrationPolicy) {
			override := MigrationOverride{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				Strategy:   IdentityTransform,
			}
			policy.Overrides = []MigrationOverride{override, override}
		},
		"identity rename": func(policy *MigrationPolicy) {
			policy.Overrides = []MigrationOverride{{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				Strategy:   IdentityTransform,
				NewName:    "unifi_dns_entry",
			}}
		},
		"address recipe without move": func(policy *MigrationPolicy) {
			policy.Overrides = []MigrationOverride{{
				SurfaceKey:        SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				Strategy:          AddressRecipe,
				NewName:           "unifi_dns_entry",
				ConfigurationEdit: "rename unifi_dns_record to unifi_dns_entry",
				Recovery:          defaultRecovery(),
			}}
		},
		"import bridge without identity": func(policy *MigrationPolicy) {
			policy.Overrides = []MigrationOverride{{
				SurfaceKey:        SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				Strategy:          ImportBridge,
				ConfigurationEdit: "replace the resource block",
				Recovery:          defaultRecovery(),
			}}
		},
		"unsupported without declared risk": func(policy *MigrationPolicy) {
			policy.Overrides = []MigrationOverride{{
				SurfaceKey:        SurfaceKey{Kind: ManagedResource, Name: "unifi_dns_record"},
				Strategy:          Unsupported,
				ConfigurationEdit: "operator decision required",
				Recovery:          defaultRecovery(),
			}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			policy := validMigrationPolicy()
			mutate(&policy)
			if _, err := ExpandMigration(baseline, versions, policy); err == nil {
				t.Fatal("ExpandMigration() succeeded")
			}
		})
	}
}

func TestParseMigrationPolicyIsStrict(t *testing.T) {
	valid := `{"format_version":1,"from_version":"0.101.2","to_version":"next","provider_address":"registry.terraform.io/ubiquiti-community/unifi","default_strategy":"identity","overrides":[]}`
	if _, err := ParseMigrationPolicy([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{
		strings.TrimSuffix(valid, "}") + `,"extra":true}`,
		valid + `{}`,
	} {
		if _, err := ParseMigrationPolicy([]byte(input)); err == nil {
			t.Fatalf("ParseMigrationPolicy(%q) succeeded", input)
		}
	}
}

func validMigrationPolicy() MigrationPolicy {
	return MigrationPolicy{
		FormatVersion:   1,
		FromVersion:     "0.101.2",
		ToVersion:       "next",
		ProviderAddress: CanonicalProviderAddress,
		DefaultStrategy: IdentityTransform,
		Overrides:       []MigrationOverride{},
	}
}

func canonicalSchemaFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "provider-contracts", "schema", "terraform-1.15.8.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertSchemaVersion(t *testing.T, versions SchemaVersions, key SurfaceKey, want int64) {
	t.Helper()
	got := versions.Versions[key]
	if got == nil || *got != want {
		t.Fatalf("schema version for %#v = %v, want %d", key, got, want)
	}
}
