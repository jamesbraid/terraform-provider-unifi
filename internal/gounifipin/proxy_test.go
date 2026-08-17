package gounifipin

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckProxyRoot covers a guard that deletes a directory outright, and two
// of these cases got through the shell version.
//
// It tested `case ${proxy_root} in /tmp/*)` and then, separately, that the value
// was neither "/tmp" nor "/". Those two tests could not fail -- a /tmp/ prefix
// already excludes both -- while the prefix itself accepted "/tmp/", whose
// remainder is empty, and "/tmp/../etc", which matches the prefix and then
// leaves. Both would have been passed to rm -rf.
func TestCheckProxyRoot(t *testing.T) {
	accepted := []string{
		"/tmp/go-unifi-proxy",
		"/tmp/nested/deeper",
	}
	for _, path := range accepted {
		if _, err := checkProxyRoot(path); err != nil {
			t.Errorf("checkProxyRoot(%q) = %v, want it accepted", path, err)
		}
	}

	refused := map[string]string{
		"/tmp/":         "an empty remainder after the prefix",
		"/tmp":          "the shared directory itself",
		"/":             "the root",
		"/tmp/../etc":   "escapes /tmp after normalising",
		"/tmp/..":       "escapes /tmp after normalising",
		"/etc":          "not under /tmp at all",
		"relative/path": "not absolute",
		"":              "unset",
	}
	for path, why := range refused {
		if _, err := checkProxyRoot(path); err == nil {
			t.Errorf("checkProxyRoot(%q) was accepted; it is %s, and this path is deleted", path, why)
		}
	}
}

// TestBuildProxyServesThePinnedVersion is the end-to-end replacement for
// bootstrap-go-unifi-proxy_test.sh: build a fixture module, serve it, and prove
// `go mod download` can fetch it back through the proxy.
//
// The fixture is what makes this hermetic. The production pin points at a tag on
// a remote, so a test that used it would need the network and would fail for
// reasons that have nothing to do with the code.
func TestBuildProxyServesThePinnedVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go toolchain against a scratch module cache; skipped under -short")
	}
	source, commit := fixtureModule(t)
	proxyRoot := scratchDirectory(t, "proxy")

	options := ProxyOptions{
		Version:        "v1.103.0",
		SourceRoot:     source,
		ProxyRoot:      proxyRoot,
		ExpectedCommit: commit,
		ExpectedSum:    "skip",
	}
	if err := BuildProxy(options); err != nil {
		t.Fatalf("BuildProxy() error = %v", err)
	}

	versionRoot := filepath.Join(proxyRoot, ModulePath, "@v")
	for _, name := range []string{"v1.103.0.mod", "v1.103.0.zip", "v1.103.0.info", "list"} {
		if _, err := os.Stat(filepath.Join(versionRoot, name)); err != nil {
			t.Errorf("the proxy is missing %s: %v", name, err)
		}
	}

	body, err := os.ReadFile(filepath.Join(versionRoot, "v1.103.0.info"))
	if err != nil {
		t.Fatal(err)
	}
	var info moduleInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("the module metadata is not JSON: %v\n%s", err, body)
	}
	if info.Origin.Hash != commit {
		t.Errorf("metadata records commit %s, want %s", info.Origin.Hash, commit)
	}
	if info.Origin.URL != ModuleOrigin {
		t.Errorf("metadata records origin %s, want %s", info.Origin.URL, ModuleOrigin)
	}
	if info.Version != "v1.103.0" {
		t.Errorf("metadata records version %s, want v1.103.0", info.Version)
	}
}

// TestBuildProxyRefusesAnUnreviewedTree covers each assertion separately, so a
// failure names which property stopped it.
//
// In shell these were four bare `test` calls under set -e. Every one of them
// failed the same way -- an exit status at no particular line -- so nothing
// distinguished a moved tag from a dirty checkout, and nothing could tell
// whether the column had run at all.
func TestBuildProxyRefusesAnUnreviewedTree(t *testing.T) {
	if testing.Short() {
		t.Skip("builds fixture repositories; skipped under -short")
	}

	t.Run("a checkout at the wrong commit", func(t *testing.T) {
		source, _ := fixtureModule(t)
		err := BuildProxy(ProxyOptions{
			Version:        "v1.103.0",
			SourceRoot:     source,
			ProxyRoot:      scratchDirectory(t, "proxy"),
			ExpectedCommit: strings.Repeat("0", 40),
			ExpectedSum:    "skip",
		})
		// The stale-checkout rule discards a tree at the wrong commit and then
		// has nothing to clone from, which is still a refusal and still names
		// the commit that was wanted.
		requireRefusal(t, err, "clone")
	})

	t.Run("a moved tag", func(t *testing.T) {
		source, _ := fixtureModule(t)
		// A second commit, with the tag left behind on the first: HEAD and the
		// pin agree, and only the tag disagrees.
		writeAndCommit(t, source, "extra.go", "package fixture\n\n// second commit\n")
		head := gitInFixture(t, source, "rev-parse", "HEAD")
		err := BuildProxy(ProxyOptions{
			Version:        "v1.103.0",
			SourceRoot:     source,
			ProxyRoot:      scratchDirectory(t, "proxy"),
			ExpectedCommit: head,
			ExpectedSum:    "skip",
		})
		requireRefusal(t, err, "the tag was moved")
	})

	t.Run("a dirty checkout", func(t *testing.T) {
		source, commit := fixtureModule(t)
		if err := os.WriteFile(filepath.Join(source, "fixture.go"),
			[]byte("package fixture\n\n// uncommitted\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := BuildProxy(ProxyOptions{
			Version:        "v1.103.0",
			SourceRoot:     source,
			ProxyRoot:      scratchDirectory(t, "proxy"),
			ExpectedCommit: commit,
			ExpectedSum:    "skip",
		})
		requireRefusal(t, err, "uncommitted changes")
	})

	t.Run("a different module", func(t *testing.T) {
		source, _ := fixtureModule(t)
		writeAndCommit(t, source, "go.mod", "module example.invalid/impostor\n\ngo 1.25.0\n")
		head := gitInFixture(t, source, "rev-parse", "HEAD")
		gitInFixture(t, source, "tag", "-f", "v1.103.0")
		err := BuildProxy(ProxyOptions{
			Version:        "v1.103.0",
			SourceRoot:     source,
			ProxyRoot:      scratchDirectory(t, "proxy"),
			ExpectedCommit: head,
			ExpectedSum:    "skip",
		})
		requireRefusal(t, err, "declares module")
	})

	// THE CASE THE SHELL SCRIPT LET THROUGH. A wrong sum has to be caught by
	// comparison, because the two fields that would otherwise be checked --
	// Path and Version -- are echoed from the argument and agree even when the
	// proxy served nothing at all.
	t.Run("an archive that hashes to something else", func(t *testing.T) {
		source, commit := fixtureModule(t)
		err := BuildProxy(ProxyOptions{
			Version:        "v1.103.0",
			SourceRoot:     source,
			ProxyRoot:      scratchDirectory(t, "proxy"),
			ExpectedCommit: commit,
			ExpectedSum:    "h1:thisIsNotTheHashOfAnything=",
		})
		requireRefusal(t, err, "hashes to")
	})
}

func requireRefusal(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("BuildProxy() succeeded; it should have refused with %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("BuildProxy() refused, but not for the reason under test.\n want mention of: %s\n got: %v",
			want, err)
	}
}

// fixtureModule builds a one-commit repository carrying the pinned module path,
// tagged at the version the tests serve.
func fixtureModule(t *testing.T) (root, commit string) {
	t.Helper()
	root = scratchDirectory(t, "source")
	gitInFixture(t, root, "init", "--quiet")
	gitInFixture(t, root, "config", "user.email", "fixture@example.invalid")
	gitInFixture(t, root, "config", "user.name", "module proxy fixture")
	gitInFixture(t, root, "config", "commit.gpgsign", "false")

	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+ModulePath+"\n\ngo 1.25.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeAndCommit(t, root, "fixture.go", "package fixture\n")
	gitInFixture(t, root, "tag", "v1.103.0")
	return root, gitInFixture(t, root, "rev-parse", "HEAD")
}

func writeAndCommit(t *testing.T, root, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	gitInFixture(t, root, "add", "-A")
	gitInFixture(t, root, "commit", "--quiet", "-m", "fixture "+name)
}

func gitInFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// scratchDirectory returns a directory under /tmp rather than t.TempDir().
//
// The proxy root is confined to /tmp on purpose, because it is deleted outright,
// and t.TempDir() sits elsewhere on macOS. Relaxing the guard so the test could
// use it would weaken the thing being tested.
func scratchDirectory(t *testing.T, purpose string) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "gounifipin-"+purpose+".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { removeModuleCache(directory) })
	return directory
}
