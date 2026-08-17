package gounifipin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeclaredVersion(t *testing.T) {
	t.Run("reads the require line", func(t *testing.T) {
		root := writeModule(t, "module example.invalid/consumer\n\ngo 1.25.0\n\nrequire (\n\t"+
			ModulePath+" v1.103.0\n)\n", "")
		version, err := DeclaredVersion(root)
		if err != nil {
			t.Fatalf("DeclaredVersion() error = %v", err)
		}
		if version != "v1.103.0" {
			t.Errorf("version = %q, want v1.103.0", version)
		}
	})

	t.Run("ignores a trailing comment", func(t *testing.T) {
		root := writeModule(t, "module example.invalid/consumer\n\nrequire "+
			ModulePath+" v1.103.0 // indirect\n", "")
		version, err := DeclaredVersion(root)
		if err != nil {
			t.Fatalf("DeclaredVersion() error = %v", err)
		}
		if version != "v1.103.0" {
			t.Errorf("version = %q, want v1.103.0", version)
		}
	})

	t.Run("no requirement is an error", func(t *testing.T) {
		root := writeModule(t, "module example.invalid/consumer\n\ngo 1.25.0\n", "")
		if _, err := DeclaredVersion(root); err == nil {
			t.Fatal("DeclaredVersion() succeeded with no requirement on the module")
		}
	})

	// THE CASE awk COULD NOT SEE. `$1 == path {print $2; exit}` matches an entry
	// inside a replace block exactly as readily as a require line, and reports
	// the version with no indication that the module was redirected. A pin that
	// describes a module the build does not use is worse than no pin.
	t.Run("a replace directive is refused, not silently read", func(t *testing.T) {
		for _, spelling := range []string{
			"replace " + ModulePath + " => ../local\n",
			"replace (\n\t" + ModulePath + " v1.103.0 => ../local\n)\n",
		} {
			root := writeModule(t, "module example.invalid/consumer\n\nrequire "+
				ModulePath+" v1.103.0\n\n"+spelling, "")
			_, err := DeclaredVersion(root)
			if err == nil {
				t.Fatalf("DeclaredVersion() accepted a replaced module:\n%s", spelling)
			}
			if !strings.Contains(err.Error(), "replace") {
				t.Errorf("error does not mention the replace:\n%v", err)
			}
		}
	})
}

func TestDeclaredSum(t *testing.T) {
	// go.sum carries two lines per version, differing only by a /go.mod suffix.
	// Picking the wrong one produces a hash that will never match an archive.
	const sums = "github.com/other/module v1.0.0 h1:other=\n" +
		ModulePath + " v1.103.0 h1:archive=\n" +
		ModulePath + " v1.103.0/go.mod h1:gomod=\n"

	t.Run("selects the archive hash", func(t *testing.T) {
		root := writeModule(t, "module example.invalid/consumer\n", sums)
		sum, err := DeclaredSum(root, "v1.103.0")
		if err != nil {
			t.Fatalf("DeclaredSum() error = %v", err)
		}
		if sum != "h1:archive=" {
			t.Errorf("sum = %q, want the archive hash h1:archive=", sum)
		}
	})

	t.Run("an unrecorded version is an error", func(t *testing.T) {
		root := writeModule(t, "module example.invalid/consumer\n", sums)
		if _, err := DeclaredSum(root, "v1.104.0"); err == nil {
			t.Fatal("DeclaredSum() invented a hash for a version go.sum does not record")
		}
	})
}

// TestThePinAgreesWithTheRepositoryItPins is the check the shell version had no
// way to run, and it is the whole reason a single home was wanted.
//
// The declared sum comes from go.sum and the expected sum is a constant in this
// package, so the two sides are independent: a repoint that updates go.mod and
// go.sum but leaves the constant behind fails here, in the fast loop, rather
// than an hour into a pipeline when the proxy serves an archive that hashes to
// something else.
func TestThePinAgreesWithTheRepositoryItPins(t *testing.T) {
	if os.Getenv("GO_UNIFI_EXPECTED_SUM") != "" || os.Getenv("GO_UNIFI_EXPECTED_COMMIT") != "" {
		t.Skip("the pin is overridden in this environment, so it describes a fixture rather than the repository")
	}
	root := repositoryRoot(t)

	version, err := DeclaredVersion(root)
	if err != nil {
		t.Fatalf("the repository's own go.mod: %v", err)
	}
	declared, err := DeclaredSum(root, version)
	if err != nil {
		t.Fatalf("the repository's own go.sum: %v", err)
	}
	if declared != ExpectedSum() {
		t.Errorf("go.sum records %s for %s@%s, but the pin expects %s.\n\n"+
			"    The proxy builds an archive and asserts it hashes to the pin, so these\n"+
			"    disagreeing means the pipelines will refuse the dependency this repository\n"+
			"    actually declares. Update defaultExpectedSum in pin.go.",
			declared, ModulePath, version, ExpectedSum())
	}
}

func writeModule(t *testing.T, goMod, goSum string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if goSum != "" {
		if err := os.WriteFile(filepath.Join(root, "go.sum"), []byte(goSum), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	// This package sits two levels down, and the test binary runs in its own
	// directory. Resolved by walking rather than assuming, so a move shows up as
	// a failure here rather than as a silently skipped comparison.
	directory, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("no go.mod found above the test directory, so the pin cannot be compared to anything")
		}
		directory = parent
	}
}
