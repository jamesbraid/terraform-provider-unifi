package gounifipin

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SourceURL is the repository the pinned tag is cloned from.
//
// It is NOT the same string as ModuleOrigin, and the difference is deliberate
// rather than an oversight: the module path -- and therefore the origin recorded
// in the proxy metadata -- belongs to ubiquiti-community, while the tag we pin
// lives on a fork. The archive is still pinned by hash, so this cannot smuggle
// different content in; what it means is that the recorded origin describes the
// module rather than the clone.
const SourceURL = "https://github.com/jamesbraid/go-unifi.git"

// infoTime is a fixed timestamp, not the current time. The proxy metadata is
// read by a gate that compares artifacts, so a field that changed on every run
// would make two correct builds disagree.
const infoTime = "2026-08-09T07:07:00Z"

// ProxyOptions describes one proxy build. Every path is explicit so a test can
// point the whole operation at a fixture, which the shell version could only do
// through environment variables.
type ProxyOptions struct {
	Version        string
	SourceRoot     string
	ProxyRoot      string
	SourceURL      string
	ExpectedCommit string
	ExpectedSum    string
	Stderr         io.Writer
}

// Options fills in the defaults the pipelines rely on, deriving the version
// from the repository's go.mod.
func Options(repositoryRoot string) (ProxyOptions, error) {
	version, err := DeclaredVersion(repositoryRoot)
	if err != nil {
		return ProxyOptions{}, err
	}
	return ProxyOptions{
		Version:        version,
		SourceRoot:     envOr("GO_UNIFI_SOURCE_ROOT", "/tmp/go-unifi-"+version+"-source"),
		ProxyRoot:      envOr("GO_UNIFI_PROXY_ROOT", "/tmp/go-unifi-proxy"),
		SourceURL:      SourceURL,
		ExpectedCommit: ExpectedCommit(),
		ExpectedSum:    ExpectedSum(),
		Stderr:         os.Stderr,
	}, nil
}

// BuildProxy assembles a file:// Go module proxy serving exactly the pinned
// version, and proves it serves it by downloading through it.
//
// WHY A PROXY AT ALL. The pipelines build against a tag that the public module
// mirror may not carry, so they serve it themselves from a clone. Everything
// here exists to make sure the archive they serve is the one that was reviewed:
// the tag resolves to the expected commit, the working tree is unmodified, the
// module path in go.mod is the one being served, and the finished archive hashes
// to the sum recorded in go.sum.
func BuildProxy(options ProxyOptions) error {
	if options.Version == "" {
		return fmt.Errorf("the proxy needs a module version to serve")
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	sourceRoot, err := checkSourceRoot(options.SourceRoot)
	if err != nil {
		return err
	}
	proxyRoot, err := checkProxyRoot(options.ProxyRoot)
	if err != nil {
		return err
	}

	if err := refreshCheckout(sourceRoot, options); err != nil {
		return err
	}
	if err := verifyCheckout(sourceRoot, options); err != nil {
		return err
	}
	if err := writeProxy(sourceRoot, proxyRoot, options); err != nil {
		return err
	}
	return verifyProxyServes(proxyRoot, options)
}

// checkProxyRoot guards a directory that is about to be deleted outright.
//
// The shell version tested for a /tmp/ prefix and then, redundantly, for the
// literal strings /tmp and / -- redundantly because a /tmp/ prefix already
// excludes both, so those two lines could not fail. What the prefix test did NOT
// exclude was "/tmp/" itself, which matches with an empty remainder, or
// "/tmp/../etc", which matches and then escapes. Cleaning the path first closes
// both: they normalise to /tmp and /etc, and neither is under /tmp.
func checkProxyRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("the proxy root is unset, and it is a directory that gets deleted")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("proxy root %q must be absolute; it is deleted before it is written", path)
	}
	if !strings.HasPrefix(clean, "/tmp/") {
		return "", fmt.Errorf("proxy root %q resolves to %q, which is not under /tmp; "+
			"it is deleted before it is written, so it is confined on purpose", path, clean)
	}
	return clean, nil
}

func checkSourceRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("the source root is unset")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", fmt.Errorf("source root %q must be absolute", path)
	}
	return clean, nil
}

// refreshCheckout makes sure the source tree is the pinned commit, cloning it if
// it is absent and discarding it if it is stale.
//
// A cached checkout left by an earlier pin is stale, not broken. The CI cache
// path no longer carries the version, so without this a bump would fail the
// commit assertion below on every run until someone cleared the cache by hand.
func refreshCheckout(sourceRoot string, options ProxyOptions) error {
	if isRepository(sourceRoot) {
		head, err := gitOutput(sourceRoot, "rev-parse", "HEAD")
		if err != nil || head != options.ExpectedCommit {
			if err := os.RemoveAll(sourceRoot); err != nil {
				return fmt.Errorf("discarding the stale checkout at %s: %w", sourceRoot, err)
			}
		}
	}
	if isRepository(sourceRoot) {
		return nil
	}
	if options.SourceURL == "" {
		return fmt.Errorf("%s is not a checkout and no source URL was given to clone one", sourceRoot)
	}
	clone := exec.Command("git", "clone", "--branch", options.Version, "--depth", "1",
		options.SourceURL, sourceRoot)
	clone.Stderr = options.Stderr
	if err := clone.Run(); err != nil {
		return fmt.Errorf("cloning %s at %s: %w", options.SourceURL, options.Version, err)
	}
	return nil
}

func isRepository(root string) bool {
	info, err := os.Stat(filepath.Join(root, ".git"))
	return err == nil && info.IsDir()
}

// verifyCheckout is the reviewed-code assertion, and every part of it names what
// it wanted when it fails.
//
// The shell version was a column of four bare `test` calls under set -e, so any
// one of them ending the run said nothing at all -- the operator saw a step
// exit non-zero at no particular line. That is the same defect the dependency
// gate next door already carries a paragraph about.
func verifyCheckout(sourceRoot string, options ProxyOptions) error {
	head, err := gitOutput(sourceRoot, "rev-parse", "HEAD")
	if err != nil {
		return fmt.Errorf("reading HEAD of %s: %w", sourceRoot, err)
	}
	if head != options.ExpectedCommit {
		return fmt.Errorf("%s is at commit %s, want the pinned %s",
			sourceRoot, head, options.ExpectedCommit)
	}

	// The tag is resolved separately from HEAD. They differ exactly when the tag
	// was moved after the checkout was made, which is the case a commit pin
	// exists to catch.
	tagged, err := gitOutput(sourceRoot, "rev-parse", options.Version+"^{commit}")
	if err != nil {
		return fmt.Errorf("resolving tag %s in %s: %w", options.Version, sourceRoot, err)
	}
	if tagged != options.ExpectedCommit {
		return fmt.Errorf("tag %s resolves to %s, want the pinned %s; the tag was moved",
			options.Version, tagged, options.ExpectedCommit)
	}

	declared, err := moduleDeclaredBy(filepath.Join(sourceRoot, "go.mod"))
	if err != nil {
		return err
	}
	if declared != ModulePath {
		return fmt.Errorf("%s/go.mod declares module %s, want %s", sourceRoot, declared, ModulePath)
	}

	dirty, err := gitOutput(sourceRoot, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fmt.Errorf("reading the state of %s: %w", sourceRoot, err)
	}
	if dirty != "" {
		return fmt.Errorf("%s has uncommitted changes, so the archive would not be the reviewed tree:\n%s",
			sourceRoot, dirty)
	}
	return nil
}

func moduleDeclaredBy(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("%s declares no module path", path)
}

type moduleOrigin struct {
	VCS  string
	URL  string
	Hash string
	Ref  string
}

type moduleInfo struct {
	Version string
	Time    string
	Origin  moduleOrigin
}

// writeProxy lays out the four files the module protocol asks for.
func writeProxy(sourceRoot, proxyRoot string, options ProxyOptions) error {
	if err := os.RemoveAll(proxyRoot); err != nil {
		return fmt.Errorf("clearing %s: %w", proxyRoot, err)
	}
	versionRoot := filepath.Join(proxyRoot, ModulePath, "@v")
	if err := os.MkdirAll(versionRoot, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", versionRoot, err)
	}

	goMod, err := os.ReadFile(filepath.Join(sourceRoot, "go.mod"))
	if err != nil {
		return fmt.Errorf("reading the module's go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(versionRoot, options.Version+".mod"), goMod, 0o644); err != nil {
		return err
	}

	archive := exec.Command("git", "-C", sourceRoot, "archive",
		"--format=zip",
		"--prefix="+ModulePath+"@"+options.Version+"/",
		"--output="+filepath.Join(versionRoot, options.Version+".zip"),
		options.Version)
	archive.Stderr = options.Stderr
	if err := archive.Run(); err != nil {
		return fmt.Errorf("archiving %s at %s: %w", sourceRoot, options.Version, err)
	}

	if err := os.WriteFile(filepath.Join(versionRoot, "list"),
		[]byte(options.Version+"\n"), 0o644); err != nil {
		return err
	}

	encoded, err := json.Marshal(moduleInfo{
		Version: options.Version,
		Time:    infoTime,
		Origin: moduleOrigin{
			VCS:  "git",
			URL:  ModuleOrigin,
			Hash: options.ExpectedCommit,
			Ref:  "refs/tags/" + options.Version,
		},
	})
	if err != nil {
		return fmt.Errorf("rendering the module metadata: %w", err)
	}
	return os.WriteFile(filepath.Join(versionRoot, options.Version+".info"),
		append(encoded, '\n'), 0o644)
}

type downloadResult struct {
	Path    string
	Version string
	Sum     string
	Error   string
	Dir     string
}

// verifyProxyServes downloads through the proxy that was just written, which is
// the only step here that exercises what the pipelines will actually do.
//
// THE SUM IS ASSERTED BECAUSE IT IS THE ONLY FIELD A FAILED DOWNLOAD OMITS.
// `go mod download -json` prints Path and Version even when it exits non-zero --
// they are echoed from the argument, not learned from the proxy -- so checking
// those two passes just as readily against a proxy that served nothing:
//
//	{"Path": "...", "Version": "v1.103.0", "Error": "module lookup disabled ..."}
//
// The shell script checked Path and Version unconditionally and the sum only
// when it was not "skip", which left the skip path asserting two fields that
// cannot disagree. Its own test file carried a paragraph explaining this hazard;
// the production script next to it did not act on it. Here Error and a non-empty
// Sum are always checked, and the recorded sum is compared on top of that.
func verifyProxyServes(proxyRoot string, options ProxyOptions) error {
	cache, err := os.MkdirTemp("/tmp", "go-unifi-proxy-validation.")
	if err != nil {
		return fmt.Errorf("creating a scratch module cache: %w", err)
	}
	defer removeModuleCache(cache)

	// An empty directory, not the repository: `go mod download module@version`
	// must resolve through GOPROXY rather than through whatever module happens
	// to enclose the working directory.
	workingDirectory, err := os.MkdirTemp("/tmp", "go-unifi-proxy-probe.")
	if err != nil {
		return fmt.Errorf("creating a scratch working directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(workingDirectory) }()

	command := exec.Command("go", "mod", "download", "-json",
		ModulePath+"@"+options.Version)
	command.Dir = workingDirectory
	command.Env = append(os.Environ(),
		"GOMODCACHE="+cache,
		"GOPROXY=file://"+proxyRoot,
		"GOSUMDB=off",
		"GOVCS=*:off",
		"GOFLAGS=",
		"GOWORK=off",
		"GIT_TERMINAL_PROMPT=0",
		"GOTOOLCHAIN=local",
	)
	output, runErr := command.Output()

	var result downloadResult
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("the proxy was built but `go mod download` did not answer with JSON: %w\n%s",
			err, strings.TrimSpace(string(output)))
	}
	if runErr != nil || result.Error != "" {
		return fmt.Errorf("the proxy just written at %s did not serve %s@%s:\n    %s",
			proxyRoot, ModulePath, options.Version, firstLine(result.Error, runErr))
	}
	if result.Path != ModulePath {
		return fmt.Errorf("the proxy served module %s, want %s", result.Path, ModulePath)
	}
	if result.Version != options.Version {
		return fmt.Errorf("the proxy served version %s, want %s", result.Version, options.Version)
	}
	if result.Sum == "" {
		return fmt.Errorf("the proxy reported no sum for %s@%s, so nothing was actually served",
			ModulePath, options.Version)
	}
	if options.ExpectedSum != "skip" && result.Sum != options.ExpectedSum {
		return fmt.Errorf("the archive built from %s hashes to %s, want the recorded %s",
			options.Version, result.Sum, options.ExpectedSum)
	}
	return nil
}

func firstLine(reported string, err error) string {
	if reported != "" {
		return reported
	}
	if err != nil {
		return err.Error()
	}
	return "no reason given"
}

// removeModuleCache makes the tree writable first. Go marks everything it
// extracts read-only, so a plain removal leaves the scratch cache behind.
func removeModuleCache(root string) {
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return nil
		}
		_ = os.Chmod(path, info.Mode().Perm()|0o200)
		return nil
	})
	_ = os.RemoveAll(root)
}

func gitOutput(dir string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}
