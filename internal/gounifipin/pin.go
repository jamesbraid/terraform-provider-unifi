// Package gounifipin is the single home for the go-unifi dependency pin, and it
// builds the file:// module proxy the pipelines download that dependency from.
//
// It replaces .woodpecker/scripts/go-unifi-pin.sh and
// .woodpecker/scripts/bootstrap-go-unifi-proxy.sh. The pin half was already a
// deliberate single home in shell: both the proxy bootstrap and the
// publishability gate used to carry their own copies of these facts, so a
// repoint that updated one and not the other produced a green tree locally and
// a failure an hour into CI. Moving it to Go keeps that property and makes it
// enforceable -- a second copy of the version is now a second package, not a
// second variable assignment nobody notices.
package gounifipin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ModulePath and ModuleOrigin identify the dependency itself.
const (
	ModulePath   = "github.com/ubiquiti-community/go-unifi"
	ModuleOrigin = "https://github.com/ubiquiti-community/go-unifi"
)

// The commit and the sum are DECLARED rather than derived, because they are
// separate claims rather than restatements of go.mod: that the tag we pin still
// resolves to the commit we reviewed, and that the module archive still hashes
// to what we recorded. Those catch a tag moved underneath us and a rebuilt
// archive, neither of which go.mod can tell us.
//
// The environment overrides exist for tests, which build a fixture repository
// and cannot know its commit in advance.
const (
	defaultExpectedCommit = "a58839fe296859bbb0e91bd57efe54f9e954fe4e"
	defaultExpectedSum    = "h1:12Qa0zjI2Rn8FT4lnieWrpXYdy29ALIDoy/afKXOohY="
)

// ExpectedCommit is the commit the pinned tag must resolve to.
func ExpectedCommit() string {
	return envOr("GO_UNIFI_EXPECTED_COMMIT", defaultExpectedCommit)
}

// ExpectedSum is the module hash the built archive must produce. The literal
// "skip" disables the comparison, which only the tests use.
func ExpectedSum() string {
	return envOr("GO_UNIFI_EXPECTED_SUM", defaultExpectedSum)
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// DeclaredVersion reads the required version straight out of go.mod.
//
// Deliberately not `go list`: this must report what the file DECLARES, so the
// publishability gate can compare it against what the build RESOLVES and notice
// a minimal-version-selection upgrade or a replace directive. `go list` answers
// the second question, and a gate whose two sides come from the same command
// cannot tell them apart.
//
// A replace directive is refused rather than reported. The shell version used
// awk '$1 == path {print $2; exit}', which reads the version out of a replace
// block just as readily as out of a require block and cannot tell which it
// found -- so a redirected module would have been reported as a plain
// requirement.
func DeclaredVersion(root string) (string, error) {
	lines, err := goModLines(root)
	if err != nil {
		return "", err
	}
	for _, fields := range lines {
		if len(fields) >= 2 && fields[0] == ModulePath && strings.HasPrefix(fields[1], "v") {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("go.mod declares no requirement on %s", ModulePath)
}

func goModLines(root string) ([][]string, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}
	var parsed [][]string
	for _, line := range strings.Split(string(body), "\n") {
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// Both spellings of a replace: the directive form and the entry inside
		// a replace ( ... ) block, which begins with the module path.
		if fields[0] == "replace" || containsArrow(fields) {
			if lineNames(fields, ModulePath) {
				return nil, fmt.Errorf("go.mod replaces %s; the pin cannot describe a redirected module", ModulePath)
			}
			continue
		}
		// Both spellings of a requirement: the one-line directive and the entry
		// inside a require ( ... ) block, which begins with the module path.
		// awk '$1 == path' only ever saw the second, so the one-line form read
		// as no requirement at all.
		if fields[0] == "require" {
			fields = fields[1:]
		}
		parsed = append(parsed, fields)
	}
	return parsed, nil
}

func containsArrow(fields []string) bool {
	for _, field := range fields {
		if field == "=>" {
			return true
		}
	}
	return false
}

func lineNames(fields []string, path string) bool {
	for _, field := range fields {
		if field == path {
			return true
		}
	}
	return false
}

// DeclaredSum reads the module hash go.sum recorded for that version.
//
// The exact version match is what selects the module archive hash rather than
// the go.mod hash: go.sum carries both, on lines that differ only by a "/go.mod"
// suffix on the version.
func DeclaredSum(root, version string) (string, error) {
	body, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		return "", fmt.Errorf("reading go.sum: %w", err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == ModulePath && fields[1] == version {
			return fields[2], nil
		}
	}
	return "", fmt.Errorf("go.sum records no hash for %s %s", ModulePath, version)
}
