package evidencebundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The shell library this replaces had a self-test covering FOUR things: a
// symlinked output directory, an unmanifested file, the happy path, and a bash
// dynamic-scoping quirk that does not exist in Go. Its other eight refusals --
// containment, emptiness, traversal, unsafe names, a symlink inside the bundle,
// an existing archive, an archive inside the bundle, and a digest mismatch --
// were never exercised. Each has a test here, because a refusal nothing
// exercises is indistinguishable from a refusal that was quietly lost.

func TestPrepareRefusesWhatWouldContaminateTheRun(t *testing.T) {
	t.Run("a symlink", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "elsewhere")
		mkdir(t, target)
		link := filepath.Join(root, "evidence")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		mustRefuse(t, "symlink", func() error {
			_, err := Prepare(link, mkdirIn(t, root, "repository"))
			return err
		})
	})

	t.Run("a path that is not a directory", func(t *testing.T) {
		root := t.TempDir()
		file := filepath.Join(root, "evidence")
		write(t, file, "not a directory")
		mustRefuse(t, "not a directory", func() error {
			_, err := Prepare(file, mkdirIn(t, root, "repository"))
			return err
		})
	})

	t.Run("inside the repository", func(t *testing.T) {
		root := t.TempDir()
		repository := mkdirIn(t, root, "repository")
		mustRefuse(t, "inside the provider repository", func() error {
			_, err := Prepare(filepath.Join(repository, "build", "evidence"), repository)
			return err
		})
	})

	t.Run("the repository itself", func(t *testing.T) {
		root := t.TempDir()
		repository := mkdirIn(t, root, "repository")
		mustRefuse(t, "inside the provider repository", func() error {
			_, err := Prepare(repository, repository)
			return err
		})
	})

	t.Run("not empty", func(t *testing.T) {
		root := t.TempDir()
		evidence := mkdirIn(t, root, "evidence")
		write(t, filepath.Join(evidence, "left-over.json"), "from an earlier run")
		mustRefuse(t, "not empty", func() error {
			_, err := Prepare(evidence, mkdirIn(t, root, "repository"))
			return err
		})
	})
}

// TestPrepareAcceptsAndResolves is the control for the five refusals above. It
// also pins the RESOLVED path being returned rather than the requested one:
// every later containment check compares against it, and a path that still
// contains a symlink makes those comparisons about the wrong directory.
func TestPrepareAcceptsAndResolves(t *testing.T) {
	root := t.TempDir()
	requested := filepath.Join(root, "evidence")
	resolved, err := Prepare(requested, mkdirIn(t, root, "repository"))
	if err != nil {
		t.Fatalf("a fresh directory outside the repository was refused: %v", err)
	}
	physical, err := filepath.EvalSymlinks(requested)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != physical {
		t.Fatalf("Prepare returned %s, want the resolved path %s", resolved, physical)
	}
}

func TestArchiveRefusesABundleNobodyVouchedFor(t *testing.T) {
	for _, c := range []struct {
		name    string
		wants   string
		corrupt func(t *testing.T, bundle string)
	}{
		{
			name:  "a file present and unmanifested",
			wants: "present and unmanifested",
			corrupt: func(t *testing.T, bundle string) {
				write(t, filepath.Join(bundle, "stale.json"), "from an earlier run")
			},
		},
		{
			name:  "a file manifested and absent",
			wants: "manifested and absent",
			corrupt: func(t *testing.T, bundle string) {
				if err := os.Remove(filepath.Join(bundle, "raw", "schema.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "a digest that does not match",
			wants: "do not match their recorded digest",
			corrupt: func(t *testing.T, bundle string) {
				write(t, filepath.Join(bundle, "raw", "schema.json"), "different bytes")
			},
		},
		{
			name:  "a missing manifest",
			wants: "checksum manifest",
			corrupt: func(t *testing.T, bundle string) {
				if err := os.Remove(filepath.Join(bundle, checksumManifest)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "a manifest that is a symlink",
			wants: "not a regular file",
			corrupt: func(t *testing.T, bundle string) {
				path := filepath.Join(bundle, checksumManifest)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(bundle, "raw", "schema.json"), path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "a malformed manifest line",
			wants: "checksum entry",
			corrupt: func(t *testing.T, bundle string) {
				write(t, filepath.Join(bundle, checksumManifest), "not a checksum line\n")
			},
		},
		{
			name:    "an absolute member name",
			wants:   "absolute path",
			corrupt: func(t *testing.T, bundle string) { rewriteManifestName(t, bundle, "/etc/passwd") },
		},
		{
			name:    "a member name that begins with a dash",
			wants:   "read as an option",
			corrupt: func(t *testing.T, bundle string) { rewriteManifestName(t, bundle, "-rf") },
		},
		{
			name:    "a member name carrying a control character",
			wants:   "control character",
			corrupt: func(t *testing.T, bundle string) { rewriteManifestName(t, bundle, "raw/schema\x07.json") },
		},
		{
			name:    "a member name that traverses upwards",
			wants:   "traverses out of",
			corrupt: func(t *testing.T, bundle string) { rewriteManifestName(t, bundle, "../outside.json") },
		},
		{
			name:  "a symlink inside the bundle",
			wants: "contains a symlink",
			corrupt: func(t *testing.T, bundle string) {
				if err := os.Symlink(filepath.Join(bundle, "raw", "schema.json"),
					filepath.Join(bundle, "raw", "alias.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			bundle := goodBundle(t, root)
			c.corrupt(t, bundle)
			archive := filepath.Join(root, "evidence.tar.gz")

			mustRefuse(t, c.wants, func() error { return Archive(bundle, archive) })

			// A rejected bundle must not leave an artifact behind. The shell
			// removed it on failure and its self-test checked that for one case;
			// every case is checked here, because a half-written archive found
			// later reads as a successful run.
			if _, err := os.Lstat(archive); err == nil {
				t.Fatalf("a rejected bundle left %s behind", archive)
			}
		})
	}
}

func TestArchiveRefusesAnUnusableDestination(t *testing.T) {
	t.Run("one that already exists", func(t *testing.T) {
		root := t.TempDir()
		bundle := goodBundle(t, root)
		archive := filepath.Join(root, "evidence.tar.gz")
		write(t, archive, "an earlier run")
		mustRefuse(t, "already exists", func() error { return Archive(bundle, archive) })
		// The pre-existing file must survive: removing it would destroy the
		// artifact this refusal exists to protect.
		if raw, err := os.ReadFile(archive); err != nil || string(raw) != "an earlier run" {
			t.Fatalf("the refusal destroyed the existing archive (%v, %q)", err, raw)
		}
	})

	t.Run("one inside the evidence directory", func(t *testing.T) {
		root := t.TempDir()
		bundle := goodBundle(t, root)
		mustRefuse(t, "a member of itself", func() error {
			return Archive(bundle, filepath.Join(bundle, "evidence.tar.gz"))
		})
	})
}

// TestArchiveWritesExactlyTheManifestedMembers is the control. Without it every
// refusal above is satisfied by a function that refuses everything.
func TestArchiveWritesExactlyTheManifestedMembers(t *testing.T) {
	root := t.TempDir()
	bundle := goodBundle(t, root)
	archive := filepath.Join(root, "evidence.tar.gz")
	if err := Archive(bundle, archive); err != nil {
		t.Fatalf("a well-formed bundle was refused: %v", err)
	}
	members, err := archiveMembers(archive)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{checksumManifest, "raw/schema.json"}
	if len(members) != len(want) {
		t.Fatalf("archive holds %v, want %v", members, want)
	}
	for i := range want {
		if members[i] != want[i] {
			t.Fatalf("archive holds %v, want %v", members, want)
		}
	}
}

// goodBundle builds an evidence directory that Archive accepts, so each test
// above changes exactly one thing about it.
func goodBundle(t *testing.T, root string) string {
	t.Helper()
	bundle := mkdirIn(t, root, "bundle")
	mkdir(t, filepath.Join(bundle, "raw"))
	const body = "schema\n"
	write(t, filepath.Join(bundle, "raw", "schema.json"), body)
	sum := sha256.Sum256([]byte(body))
	write(t, filepath.Join(bundle, checksumManifest),
		hex.EncodeToString(sum[:])+"  raw/schema.json\n")
	return bundle
}

// rewriteManifestName keeps the digest and replaces only the member name, so
// the test is about the name and not about a checksum that stopped matching.
func rewriteManifestName(t *testing.T, bundle, name string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(bundle, checksumManifest))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bundle, checksumManifest), string(raw[:66])+name+"\n")
}

func mustRefuse(t *testing.T, wants string, call func() error) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("accepted; expected a refusal mentioning %q", wants)
	}
	if !strings.Contains(err.Error(), wants) {
		t.Fatalf("refused with %q, which does not mention %q. A refusal that names the wrong "+
			"subject sends the reader after the wrong problem", err, wants)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

func mkdirIn(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	mkdir(t, path)
	return path
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
