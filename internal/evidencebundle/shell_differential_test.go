package evidencebundle

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This file exists for exactly as long as the shell library does.
//
// The migration's acceptance criterion is that the Go implementation does what
// the shell did, and the only window in which that can be established is while
// both exist. So the comparison is a test rather than a note: it builds one
// fixture, hands an identical copy to each implementation, and requires them to
// agree on accept or reject.
//
// DELETE THIS FILE IN THE SAME COMMIT THAT DELETES m1-evidence-lib.sh. It does
// not skip when the script is missing -- a skip would turn "the comparison was
// never made" into a green run, which is the failure mode this whole migration
// keeps finding. A missing script fails here, and the fix is to delete the
// comparison deliberately rather than to let it evaporate.
//
// It compares the VERDICT, not the message. The two implementations word their
// refusals differently on purpose: the Go ones name the subject and say what
// goes wrong, which is the improvement. Requiring identical text would be
// requiring the port to keep the thing worth changing.

const shellLibrary = "../../.woodpecker/scripts/m1-evidence-lib.sh"

func TestPrepareAgreesWithTheShell(t *testing.T) {
	requireShellLibrary(t)
	for _, c := range []struct {
		name  string
		build func(t *testing.T, root string) (requested, repository string)
	}{
		{"a symlink", func(t *testing.T, root string) (string, string) {
			target := filepath.Join(root, "elsewhere")
			mkdir(t, target)
			link := filepath.Join(root, "evidence")
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			return link, mkdirIn(t, root, "repository")
		}},
		{"a path that is not a directory", func(t *testing.T, root string) (string, string) {
			file := filepath.Join(root, "evidence")
			write(t, file, "not a directory")
			return file, mkdirIn(t, root, "repository")
		}},
		{"inside the repository", func(t *testing.T, root string) (string, string) {
			repository := mkdirIn(t, root, "repository")
			return filepath.Join(repository, "build", "evidence"), repository
		}},
		{"the repository itself", func(t *testing.T, root string) (string, string) {
			repository := mkdirIn(t, root, "repository")
			return repository, repository
		}},
		{"not empty", func(t *testing.T, root string) (string, string) {
			evidence := mkdirIn(t, root, "evidence")
			write(t, filepath.Join(evidence, "left-over.json"), "from an earlier run")
			return evidence, mkdirIn(t, root, "repository")
		}},
		{"a fresh directory outside the repository", func(t *testing.T, root string) (string, string) {
			return filepath.Join(root, "evidence"), mkdirIn(t, root, "repository")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			shellRoot, goRoot := t.TempDir(), t.TempDir()
			shellRequested, shellRepository := c.build(t, shellRoot)
			goRequested, goRepository := c.build(t, goRoot)

			byShell := shellAccepts(t, "prepare_evidence_directory", shellRequested, shellRepository)
			_, err := Prepare(goRequested, goRepository)
			compareVerdicts(t, byShell, err)
		})
	}
}

func TestArchiveAgreesWithTheShell(t *testing.T) {
	requireShellLibrary(t)
	for _, c := range []struct {
		name  string
		build func(t *testing.T, root string) (bundle, archive string)
	}{
		{"a well-formed bundle", func(t *testing.T, root string) (string, string) {
			return goodBundle(t, root), filepath.Join(root, "evidence.tar.gz")
		}},
		{"a file present and unmanifested", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			write(t, filepath.Join(bundle, "stale.json"), "from an earlier run")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a file manifested and absent", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			if err := os.Remove(filepath.Join(bundle, "raw", "schema.json")); err != nil {
				t.Fatal(err)
			}
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a digest that does not match", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			write(t, filepath.Join(bundle, "raw", "schema.json"), "different bytes")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a missing manifest", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			if err := os.Remove(filepath.Join(bundle, checksumManifest)); err != nil {
				t.Fatal(err)
			}
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a manifest that is a symlink", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			path := filepath.Join(bundle, checksumManifest)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(bundle, "raw", "schema.json"), path); err != nil {
				t.Fatal(err)
			}
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"an absolute member name", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			rewriteManifestName(t, bundle, "/etc/passwd")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a member name that begins with a dash", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			rewriteManifestName(t, bundle, "-rf")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a member name that traverses upwards", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			rewriteManifestName(t, bundle, "../outside.json")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a symlink inside the bundle", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			if err := os.Symlink(filepath.Join(bundle, "raw", "schema.json"),
				filepath.Join(bundle, "raw", "alias.json")); err != nil {
				t.Fatal(err)
			}
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a malformed manifest line", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			write(t, filepath.Join(bundle, checksumManifest), "not a checksum line\n")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"a member name carrying a control character", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			rewriteManifestName(t, bundle, "raw/schema\x07.json")
			return bundle, filepath.Join(root, "evidence.tar.gz")
		}},
		{"an archive that already exists", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			archive := filepath.Join(root, "evidence.tar.gz")
			write(t, archive, "an earlier run")
			return bundle, archive
		}},
		{"an archive inside the evidence directory", func(t *testing.T, root string) (string, string) {
			bundle := goodBundle(t, root)
			return bundle, filepath.Join(bundle, "evidence.tar.gz")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			shellRoot, goRoot := t.TempDir(), t.TempDir()
			shellBundle, shellArchive := c.build(t, shellRoot)
			goBundle, goArchive := c.build(t, goRoot)

			byShell := shellAccepts(t, "archive_evidence_directory", shellBundle, shellArchive)
			compareVerdicts(t, byShell, Archive(goBundle, goArchive))
		})
	}
}

func compareVerdicts(t *testing.T, shellAccepted bool, goErr error) {
	t.Helper()
	goAccepted := goErr == nil
	if shellAccepted == goAccepted {
		return
	}
	if shellAccepted {
		t.Fatalf("the shell ACCEPTED this and the Go REFUSED it (%v). A port that refuses more "+
			"than the original blocks runs that used to pass, and the pressure will be to "+
			"delete the check rather than to fix the bundle", goErr)
	}
	t.Fatal("the shell REFUSED this and the Go ACCEPTED it. The port lost a refusal, which is " +
		"the failure that leaves no trace: everything stays green and the thing the check " +
		"existed to stop now ships")
}

// shellAccepts sources the library and calls one of its two functions, so the
// comparison is against the code that is actually deployed rather than against
// a description of it.
func shellAccepts(t *testing.T, function, first, second string) bool {
	t.Helper()
	script := "set -u; source \"$1\"; \"$2\" \"$3\" \"$4\" >/dev/null 2>&1"
	command := exec.Command("bash", "-c", script, "bash", shellLibrary, function, first, second)
	err := command.Run()
	if err == nil {
		return true
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false
	}
	t.Fatalf("could not run the shell library: %v", err)
	return false
}

func requireShellLibrary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(shellLibrary); err != nil {
		t.Fatalf("%s is gone, so this comparison is measuring nothing: %v.\n"+
			"If the shell library was deleted deliberately, delete this file in the same "+
			"commit. Leaving it to skip would record a comparison that never happened.",
			shellLibrary, err)
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Fatalf("sha256sum is required to run the shell library: %v", err)
	}
}
