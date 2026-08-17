// Package dependencypin decides whether the go-unifi dependency this provider
// builds against is one a published release could depend on.
//
// It replaces .woodpecker/scripts/go-unifi-pin.sh and the judgement half of
// .woodpecker/scripts/catalog-dependency-publishability.sh.
//
// The question is narrower than "does it build". A module can build perfectly
// from a local replace directive, from a version minimal-version-selection
// picked rather than the one go.mod asked for, or from an archive that no
// longer hashes to what was reviewed -- and none of those can be depended on by
// anybody else.
package dependencypin

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The pin. Version is deliberately NOT here: which version we depend on is a
// decision recorded in go.mod, and restating it would create the second home
// this replaces. The commit and the archive hash are separate CLAIMS rather
// than restatements -- that the tag still resolves to the commit that was
// reviewed, and that the archive still hashes to what was recorded. go.mod can
// tell us neither.
const (
	ModulePath   = "github.com/ubiquiti-community/go-unifi"
	ModuleOrigin = "https://github.com/ubiquiti-community/go-unifi"
)

// Pin is the expectation a run is measured against.
type Pin struct {
	ModulePath     string
	ModuleOrigin   string
	ExpectedCommit string
	ExpectedSum    string
}

// Resolved is what the build actually resolved, as `go list -m -json` reports
// it plus what the module cache holds.
type Resolved struct {
	Path           string
	Version        string
	Sum            string
	Dir            string
	ReplacePresent bool

	OriginCommit string
	OriginURL    string

	ArchiveSHA256 string
	TreeSHA256    string

	// ProxyDisabled records whether the resolution ran with the module proxy
	// off. It is the measurable half of the receipt's network_boundary claim,
	// which the shell wrote as a literal.
	ProxyDisabled bool
}

// Declared is what the repository's own files ask for.
type Declared struct {
	Version string
	Sum     string
}

// ReadDeclared reads the required version out of go.mod and its hash out of
// go.sum.
//
// DELIBERATELY NOT `go list`. This must report what the files DECLARE so that
// Check can compare it against what the build RESOLVES. Asking the toolchain
// for both sides would make them agree by construction, and the two cases this
// gate exists to catch -- a minimal-version-selection upgrade and a replace
// directive -- are exactly the cases where they differ.
func ReadDeclared(root, modulePath string) (Declared, error) {
	var declared Declared
	version, err := firstField(filepath.Join(root, "go.mod"), func(fields []string) (string, bool) {
		if len(fields) >= 2 && fields[0] == modulePath {
			return fields[1], true
		}
		return "", false
	})
	if err != nil {
		return declared, err
	}
	if version == "" {
		return declared, fmt.Errorf("go.mod declares no requirement on %s", modulePath)
	}
	declared.Version = version

	sum, err := firstField(filepath.Join(root, "go.sum"), func(fields []string) (string, bool) {
		if len(fields) >= 3 && fields[0] == modulePath && fields[1] == version {
			return fields[2], true
		}
		return "", false
	})
	if err != nil {
		return declared, err
	}
	if sum == "" {
		return declared, fmt.Errorf("go.sum records no hash for %s %s", modulePath, version)
	}
	declared.Sum = sum
	return declared, nil
}

func firstField(path string, match func([]string) (string, bool)) (string, error) {
	handle, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer handle.Close()
	scanner := bufio.NewScanner(handle)
	for scanner.Scan() {
		if value, ok := match(strings.Fields(scanner.Text())); ok {
			return value, nil
		}
	}
	return "", scanner.Err()
}

// Check reports every way the resolved dependency fails to be publishable.
//
// It returns ALL of them rather than the first. The shell was a column of bare
// `test` calls under set -e, so the step died at the first one and an operator
// fixing a repoint learned about the next problem only after another CI round
// trip.
func Check(pin Pin, declared Declared, resolved Resolved) []string {
	var problems []string
	compare := func(what, want, got string) {
		if want != got {
			if got == "" {
				got = "empty"
			}
			problems = append(problems, fmt.Sprintf("%s is %s, want %s", what, got, want))
		}
	}

	compare("resolved module path", pin.ModulePath, resolved.Path)
	// Declared against resolved, not against a constant. They differ exactly
	// when minimal version selection upgraded the module or a replace
	// redirected it, which is what makes a dependency unpublishable and what a
	// hardcoded expectation could never detect.
	compare(fmt.Sprintf("resolved module version (go.mod declares %s)", declared.Version),
		declared.Version, resolved.Version)
	compare(fmt.Sprintf("resolved module sum (go.sum records %s)", declared.Sum),
		declared.Sum, resolved.Sum)
	if resolved.ReplacePresent {
		problems = append(problems, "a replace directive redirects the module, so nobody else "+
			"can build against this dependency")
	}
	// The commit is a claim about the tag rather than a restatement of it: a
	// tag moved underneath us keeps its version and changes its commit.
	compare("module origin commit", pin.ExpectedCommit, resolved.OriginCommit)
	compare("module origin URL", pin.ModuleOrigin, resolved.OriginURL)

	if len(resolved.ArchiveSHA256) != 64 {
		problems = append(problems, "the module archive was not digested")
	}
	if len(resolved.TreeSHA256) != 64 {
		problems = append(problems, "the extracted module tree was not digested")
	}
	sort.Strings(problems)
	return problems
}

// NetworkBoundary describes where the dependency could have come from.
//
// MEASURED, where the shell wrote "remote_ci_only" as a literal in its jq
// template. With the proxy off the resolution can only have used what was
// already in the module cache, which is the claim the field is making; with it
// on, the run could have fetched anything and the receipt should say so.
func NetworkBoundary(proxyDisabled bool) string {
	if proxyDisabled {
		return "remote_ci_only"
	}
	return "module_proxy_reachable"
}

// TreeDigest hashes a directory as the set of its files and their contents.
//
// Ordered by path under a byte comparison so two identical trees on two
// machines digest the same. The shell piped find into sort with LC_ALL=C for
// the same reason; without it the digest would depend on the locale of the
// machine that ran it.
func TreeDigest(root string) (string, error) {
	type member struct{ name, digest string }
	var members []member
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		raw, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		members = append(members, member{filepath.ToSlash(relative), hex.EncodeToString(sum[:])})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(members, func(i, j int) bool { return members[i].name < members[j].name })

	overall := sha256.New()
	for _, m := range members {
		fmt.Fprintf(overall, "%s  %s\n", m.digest, m.name)
	}
	return hex.EncodeToString(overall.Sum(nil)), nil
}

// FileDigest is the sha256 of a file's bytes.
func FileDigest(path string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
