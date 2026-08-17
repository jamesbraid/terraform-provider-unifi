package upgradeplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheCLIConfigNamesTheDirectoryAndTheProvider. dev_overrides addresses a
// DIRECTORY, and swapping which directory it names is how one configuration is
// planned by two different providers without re-initialising -- the whole
// mechanism of this gate.
func TestTheCLIConfigNamesTheDirectoryAndTheProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.tfrc")
	if err := WriteCLIConfig("/work/plugins/old", path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{"dev_overrides", ProviderAddress, "/work/plugins/old", "direct {}"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the CLI config does not contain %q:\n%s", want, body)
		}
	}
	// Two configs must differ in exactly the directory, or both plans run
	// against the same provider and the comparison is a program agreeing with
	// itself.
	other := filepath.Join(t.TempDir(), "new.tfrc")
	if err := WriteCLIConfig("/work/plugins/new", other); err != nil {
		t.Fatal(err)
	}
	otherRaw, err := os.ReadFile(other)
	if err != nil {
		t.Fatal(err)
	}
	if string(otherRaw) == body {
		t.Fatal("two plugin directories produced identical CLI configs")
	}
}

// TestAnEmptyPluginDirectoryIsRefused. An override naming nothing is not an
// error to the CLI: it resolves the provider from the registry instead, so the
// run measures a PUBLISHED provider and reports a clean upgrade.
func TestAnEmptyPluginDirectoryIsRefused(t *testing.T) {
	err := WriteCLIConfig("", filepath.Join(t.TempDir(), "empty.tfrc"))
	if err == nil {
		t.Fatal("an override naming no directory was accepted")
	}
	if !strings.Contains(err.Error(), "registry") {
		t.Fatalf("the refusal %q does not say what would happen instead", err)
	}
}

// TestTheFixtureIsStagedRatherThanPlannedInPlace. An apply writes
// terraform.tfstate beside the configuration; doing that in a committed fixture
// directory leaves state in the repository for the next run to find.
func TestTheFixtureIsStagedRatherThanPlannedInPlace(t *testing.T) {
	source := t.TempDir()
	write(t, filepath.Join(source, "main.tf"), "resource \"x\" \"y\" {}\n")
	write(t, filepath.Join(source, "other.tf"), "# second\n")
	write(t, filepath.Join(source, "EXPECT_OLD_PLAN"), "0\n")
	write(t, filepath.Join(source, "README.md"), "not configuration\n")

	fixture, err := LoadFixture(source)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "config")
	copied, err := StageFixture(fixture, destination)
	if err != nil {
		t.Fatal(err)
	}
	if copied != 2 {
		t.Fatalf("staged %d file(s), want the two .tf files", copied)
	}
	// Only the configuration travels. EXPECT_OLD_PLAN is a declaration about
	// the fixture, not part of it, and copying it would put a file the CLI does
	// not understand next to the configuration.
	for _, absent := range []string{"EXPECT_OLD_PLAN", "README.md"} {
		if _, err := os.Stat(filepath.Join(destination, absent)); err == nil {
			t.Fatalf("%s was staged; only .tf files are configuration", absent)
		}
	}
	// And the fixture directory is untouched.
	if _, err := os.Stat(filepath.Join(source, "terraform.tfstate")); err == nil {
		t.Fatal("state was written into the fixture directory")
	}
}

// TestAnApplyThatWroteNoStateIsCaught. Both plans would compare a configuration
// against an empty state and agree that everything needs creating -- a green
// control and a green subject describing nothing.
func TestAnApplyThatWroteNoStateIsCaught(t *testing.T) {
	directory := t.TempDir()
	if err := StateWasWritten(directory); err == nil {
		t.Fatal("a config directory with no state file was accepted")
	}
	write(t, filepath.Join(directory, "terraform.tfstate"), "")
	if err := StateWasWritten(directory); err == nil {
		t.Fatal("an empty state file was accepted, which compares against nothing just as " +
			"surely as an absent one")
	}
	write(t, filepath.Join(directory, "terraform.tfstate"), `{"version":4}`)
	if err := StateWasWritten(directory); err != nil {
		t.Fatalf("a written state was refused: %v", err)
	}
}
