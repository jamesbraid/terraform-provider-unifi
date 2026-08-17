package upgradeplan

import (
	"fmt"
	"os"
	"path/filepath"
)

// ProviderBinaryName is what dev_overrides requires the binary inside the
// directory to be called.
//
// Release artifacts carry a version suffix, so an injected published binary is
// COPIED UNDER THIS NAME rather than linked under its published one. Getting
// this wrong does not error: the CLI simply does not find an override and
// resolves the provider from the registry instead, so the run measures a
// published provider twice and reports a clean upgrade.
const ProviderBinaryName = "terraform-provider-unifi"

// ProviderAddress is the source address the fixtures name.
const ProviderAddress = "registry.terraform.io/ubiquiti-community/unifi"

// WriteCLIConfig writes a CLI configuration whose dev_overrides points at one
// plugin directory.
//
// dev_overrides addresses a DIRECTORY, and swapping which directory the
// override names is how one configuration is planned by two different providers
// without re-initialising. That is the whole mechanism of this gate: the state
// stays put and the provider underneath it changes.
func WriteCLIConfig(pluginDirectory, destination string) error {
	if pluginDirectory == "" {
		return fmt.Errorf("no plugin directory: dev_overrides would name nothing and the CLI " +
			"would resolve the provider from the registry, so the run would measure a published " +
			"provider instead of the one it built")
	}
	body := fmt.Sprintf(`provider_installation {
  dev_overrides { %q = %q }
  direct {}
}
`, ProviderAddress, pluginDirectory)
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	return os.WriteFile(destination, []byte(body), 0o600)
}

// StageFixture copies the fixture's .tf files into a working directory and
// reports how many it copied.
//
// The fixture directory itself is never planned in place: an apply writes
// terraform.tfstate beside the configuration, and writing that into a committed
// fixture directory would leave state in the repository for the next run to
// find.
func StageFixture(fixture Fixture, destination string) (int, error) {
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(fixture.Directory)
	if err != nil {
		return 0, err
	}
	copied := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tf" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(fixture.Directory, entry.Name()))
		if err != nil {
			return copied, err
		}
		if err := os.WriteFile(filepath.Join(destination, entry.Name()), raw, 0o600); err != nil {
			return copied, err
		}
		copied++
	}
	if copied != fixture.Files {
		return copied, fmt.Errorf("staged %d of the fixture's %d .tf file(s); a configuration "+
			"that is not all there plans differently and the result would describe neither",
			copied, fixture.Files)
	}
	return copied, nil
}

// StateWasWritten reports whether the apply left state for the two plans to
// compare against.
//
// An apply that reports success and writes nothing is the case that makes both
// plans meaningless: they would each compare a configuration against an empty
// state and agree that everything needs creating.
func StateWasWritten(configDirectory string) error {
	info, err := os.Stat(filepath.Join(configDirectory, "terraform.tfstate"))
	if err != nil {
		return fmt.Errorf("the apply reported success but wrote no state; the plans would " +
			"compare against nothing")
	}
	if info.Size() == 0 {
		return fmt.Errorf("the apply wrote an empty state file; the plans would compare against " +
			"nothing")
	}
	return nil
}
