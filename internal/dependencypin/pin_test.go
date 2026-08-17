package dependencypin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func goodResolved() Resolved {
	return Resolved{
		Path:          ModulePath,
		Version:       "v1.103.0",
		Sum:           "h1:aaaa",
		Dir:           "/cache/dir",
		OriginCommit:  strings.Repeat("a", 40),
		OriginURL:     ModuleOrigin,
		ArchiveSHA256: strings.Repeat("b", 64),
		TreeSHA256:    strings.Repeat("c", 64),
		ProxyDisabled: true,
	}
}

func goodPin() Pin {
	return Pin{ModulePath: ModulePath, ModuleOrigin: ModuleOrigin, ExpectedCommit: strings.Repeat("a", 40)}
}

func goodDeclared() Declared { return Declared{Version: "v1.103.0", Sum: "h1:aaaa"} }

// TestCheckAcceptsAPublishableDependency is the control. Without it every
// refusal below is satisfied by a function that refuses everything.
func TestCheckAcceptsAPublishableDependency(t *testing.T) {
	if problems := Check(goodPin(), goodDeclared(), goodResolved()); len(problems) != 0 {
		t.Fatalf("a publishable dependency was refused: %v", problems)
	}
}

// TestEachWayToBeUnpublishableFiresOnItsOwn. The shell was a column of bare
// `test` calls under set -e: the step died at the first one, so nothing
// established that the later ones could fail at all, and an operator fixing a
// repoint learned about the next problem a CI round trip later.
func TestEachWayToBeUnpublishableFiresOnItsOwn(t *testing.T) {
	for _, c := range []struct {
		name    string
		wants   string
		corrupt func(pin *Pin, declared *Declared, resolved *Resolved)
	}{
		{"the resolved path is a different module", "resolved module path",
			func(_ *Pin, _ *Declared, r *Resolved) { r.Path = "example.com/other" }},
		{"minimal version selection upgraded it", "resolved module version",
			func(_ *Pin, _ *Declared, r *Resolved) { r.Version = "v1.104.0" }},
		{"the archive no longer hashes to what go.sum recorded", "resolved module sum",
			func(_ *Pin, _ *Declared, r *Resolved) { r.Sum = "h1:different" }},
		{"a replace directive redirects it", "replace directive",
			func(_ *Pin, _ *Declared, r *Resolved) { r.ReplacePresent = true }},
		{"the tag moved to another commit", "module origin commit",
			func(_ *Pin, _ *Declared, r *Resolved) { r.OriginCommit = strings.Repeat("f", 40) }},
		{"the module came from somewhere else", "module origin URL",
			func(_ *Pin, _ *Declared, r *Resolved) { r.OriginURL = "https://example.com/elsewhere" }},
		{"the archive was not digested", "module archive was not digested",
			func(_ *Pin, _ *Declared, r *Resolved) { r.ArchiveSHA256 = "" }},
		{"the tree was not digested", "module tree was not digested",
			func(_ *Pin, _ *Declared, r *Resolved) { r.TreeSHA256 = "" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			pin, declared, resolved := goodPin(), goodDeclared(), goodResolved()
			c.corrupt(&pin, &declared, &resolved)
			problems := Check(pin, declared, resolved)
			if len(problems) == 0 {
				t.Fatalf("accepted; expected a refusal mentioning %q", c.wants)
			}
			if !strings.Contains(strings.Join(problems, "; "), c.wants) {
				t.Fatalf("refused with %v, which does not mention %q", problems, c.wants)
			}
		})
	}
}

// TestCheckReportsEveryProblemAtOnce. Under set -e the shell stopped at the
// first, so a repoint that broke three things took three CI runs to discover.
func TestCheckReportsEveryProblemAtOnce(t *testing.T) {
	resolved := goodResolved()
	resolved.Version = "v1.104.0"
	resolved.ReplacePresent = true
	resolved.OriginURL = "https://example.com/elsewhere"
	problems := Check(goodPin(), goodDeclared(), resolved)
	if len(problems) != 3 {
		t.Fatalf("reported %d problem(s), want 3: %v", len(problems), problems)
	}
}

// TestDeclaredIsComparedAgainstResolvedNotAgainstItself is the assertion that
// makes this gate mean anything. If the expectation were also read from the
// toolchain, both sides would agree by construction and the two failures this
// exists to catch -- an MVS upgrade and a replace -- would be invisible.
func TestDeclaredIsComparedAgainstResolvedNotAgainstItself(t *testing.T) {
	declared := Declared{Version: "v1.103.0", Sum: "h1:aaaa"}
	resolved := goodResolved()
	resolved.Version = "v1.103.1" // what MVS actually selected
	problems := Check(goodPin(), declared, resolved)
	joined := strings.Join(problems, "; ")
	if !strings.Contains(joined, "v1.103.1") || !strings.Contains(joined, "v1.103.0") {
		t.Fatalf("the refusal %q does not name BOTH the declared and the resolved version, so a "+
			"reader cannot tell which side moved", joined)
	}
}

func TestNetworkBoundaryIsMeasuredNotAsserted(t *testing.T) {
	if got := NetworkBoundary(true); got != "remote_ci_only" {
		t.Fatalf("proxy off reported %q", got)
	}
	if got := NetworkBoundary(false); got == "remote_ci_only" {
		t.Fatal("a run that could reach the module proxy still claimed remote_ci_only, which is " +
			"the literal the shell wrote regardless of what happened")
	}
}

func TestReadDeclaredReadsThisRepository(t *testing.T) {
	declared, err := ReadDeclared(filepath.Join("..", ".."), ModulePath)
	if err != nil {
		t.Fatalf("read the declared dependency: %v", err)
	}
	if !strings.HasPrefix(declared.Version, "v") {
		t.Fatalf("go.mod declares %q as the version", declared.Version)
	}
	if !strings.HasPrefix(declared.Sum, "h1:") {
		t.Fatalf("go.sum records %q, which is not a module hash", declared.Sum)
	}
}

func TestReadDeclaredRefusesWhatIsNotThere(t *testing.T) {
	root := filepath.Join("..", "..")
	if _, err := ReadDeclared(root, "example.com/not-a-dependency"); err == nil {
		t.Fatal("a module go.mod does not require was accepted, so this gate would report on a " +
			"dependency that is not there")
	}
	if _, err := ReadDeclared(t.TempDir(), ModulePath); err == nil {
		t.Fatal("a directory with no go.mod was accepted")
	}
}

func TestTreeDigestDescribesContentAndNames(t *testing.T) {
	build := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			path := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		digest, err := TreeDigest(root)
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}

	base := map[string]string{"a.go": "package a\n", "sub/b.go": "package b\n"}
	first := build(t, base)
	// The digest must not depend on WHERE the tree is: it is compared between
	// runs on different machines with different module caches.
	if again := build(t, base); again != first {
		t.Fatal("the same tree in two directories digested differently")
	}
	if changed := build(t, map[string]string{"a.go": "package a // edited\n", "sub/b.go": "package b\n"}); changed == first {
		t.Fatal("editing a file did not move the digest")
	}
	// Names matter as well as contents: two files whose bodies are swapped are
	// a different tree.
	if renamed := build(t, map[string]string{"a.go": "package b\n", "sub/b.go": "package a\n"}); renamed == first {
		t.Fatal("swapping two files' contents did not move the digest, so the digest describes " +
			"the set of bodies and not the tree")
	}
}

func TestBuildReceiptDerivesResultFromTheChecks(t *testing.T) {
	pin, declared, resolved := goodPin(), goodDeclared(), goodResolved()
	if got := BuildReceipt(pin, declared, resolved, strings.Repeat("d", 40), "remote_ci", nil).Result; got != "pass" {
		t.Fatalf("a publishable dependency produced result %q", got)
	}
	resolved.ReplacePresent = true
	receipt := BuildReceipt(pin, declared, resolved, strings.Repeat("d", 40), "remote_ci", nil)
	if receipt.Result == "pass" {
		t.Fatal("a replaced module produced a passing receipt. In the shell result was a literal, " +
			"so it said pass whatever the checks had found")
	}
	if !receipt.ReplacePresent {
		t.Fatal("replace_present was written as false while a replace was present -- the shell's " +
			"literal, reproduced")
	}
}
