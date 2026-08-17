// Command catalog-dependency-publishability records whether the go-unifi
// dependency this provider builds against is one a published release could
// depend on.
//
// It replaces .woodpecker/scripts/catalog-dependency-publishability.sh and the
// pin half of go-unifi-pin.sh. Everything here is orchestration: ask the
// toolchain what it resolved, read the module cache, digest, write. The
// judgement is internal/dependencypin.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/dependencypin"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "dependency publishability: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	repository := flag.String("repo", ".", "repository whose go.mod declares the dependency")
	output := flag.String("output", "", "write the receipt here; required")
	expectedCommit := flag.String("expected-commit", "a58839fe296859bbb0e91bd57efe54f9e954fe4e",
		"the commit the pinned tag must still resolve to")
	// resolutionRunner cannot be measured from inside this process, so it is an
	// input. It defaults to what the environment can show rather than to the
	// answer that passes: a run that cannot demonstrate it was CI records
	// local_workstation and is rejected downstream. The flag exists because
	// this has not been confirmed against the CI runner's environment, and a
	// default that wrongly blocks is recoverable where one that wrongly passes
	// is the defect this gate is full of.
	resolutionRunner := flag.String("resolution-runner", "",
		"who resolved the dependency (default: remote_ci when CI is set, else local_workstation)")
	flag.Parse()

	if *output == "" {
		return errors.New("-output is required")
	}
	repo, err := filepath.Abs(*repository)
	if err != nil {
		return err
	}

	pin := dependencypin.Pin{
		ModulePath:     dependencypin.ModulePath,
		ModuleOrigin:   dependencypin.ModuleOrigin,
		ExpectedCommit: *expectedCommit,
	}
	declared, err := dependencypin.ReadDeclared(repo, pin.ModulePath)
	if err != nil {
		return err
	}
	resolved, err := resolve(repo, pin.ModulePath, declared.Version)
	if err != nil {
		return err
	}
	providerCommit, err := gitOutput(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}

	runner := *resolutionRunner
	if runner == "" {
		runner = "local_workstation"
		if os.Getenv("CI") != "" {
			runner = "remote_ci"
		}
	}

	receipt := dependencypin.BuildReceipt(pin, declared, resolved, providerCommit, runner)
	encoded, err := catalogparity.MarshalReceipt(receipt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(*output, encoded, 0o600); err != nil {
		return err
	}

	// The receipt is written before the verdict is reported, so a blocked run
	// leaves behind the evidence of what blocked it. The shell exited first and
	// wrote nothing.
	if problems := dependencypin.Check(pin, declared, resolved); len(problems) > 0 {
		for _, problem := range problems {
			fmt.Fprintf(os.Stderr, "  %s\n", problem)
		}
		return fmt.Errorf("%d check(s) failed; the receipt is at %s", len(problems), *output)
	}
	fmt.Fprintf(os.Stderr, "dependency publishability: pass. %s %s resolved to %s, runner %s\n",
		resolved.Path, resolved.Version, resolved.OriginCommit, runner)
	return nil
}

// resolve asks the toolchain what it actually selected, with the proxy off.
//
// GOPROXY=off is not just hygiene: it is what makes the network_boundary claim
// in the receipt true rather than asserted. With it off, the answer can only
// have come from the module cache.
func resolve(repo, modulePath, declaredVersion string) (dependencypin.Resolved, error) {
	var resolved dependencypin.Resolved
	resolved.ProxyDisabled = true

	command := exec.Command("go", "list", "-mod=readonly", "-m", "-json", modulePath)
	command.Dir = repo
	command.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOVCS=*:off",
		"GIT_TERMINAL_PROMPT=0", "GOTOOLCHAIN=local")
	out, err := command.Output()
	if err != nil {
		return resolved, fmt.Errorf("go list -m %s: %w", modulePath, err)
	}

	var listed struct {
		Path    string          `json:"Path"`
		Version string          `json:"Version"`
		Sum     string          `json:"Sum"`
		Dir     string          `json:"Dir"`
		Replace json.RawMessage `json:"Replace"`
	}
	if err := json.Unmarshal(out, &listed); err != nil {
		return resolved, fmt.Errorf("parse go list output: %w", err)
	}
	resolved.Path, resolved.Version, resolved.Sum, resolved.Dir = listed.Path, listed.Version, listed.Sum, listed.Dir
	// MEASURED, where the shell wrote the literal false into its template after
	// checking the same thing and throwing the answer away.
	resolved.ReplacePresent = len(listed.Replace) > 0

	if resolved.Dir == "" {
		return resolved, errors.New("go list reports no extracted module directory")
	}
	if _, err := os.Stat(resolved.Dir); err != nil {
		return resolved, fmt.Errorf("extracted module directory: %w", err)
	}

	cache, err := goEnv("GOMODCACHE")
	if err != nil {
		return resolved, err
	}
	versionRoot := catalogparity.DefaultSDKDownloadRoot(cache, modulePath)
	archive := filepath.Join(versionRoot, declaredVersion+".zip")
	info := filepath.Join(versionRoot, declaredVersion+".info")

	if resolved.ArchiveSHA256, err = dependencypin.FileDigest(archive); err != nil {
		return resolved, fmt.Errorf("module archive: %w", err)
	}
	if resolved.TreeSHA256, err = dependencypin.TreeDigest(resolved.Dir); err != nil {
		return resolved, fmt.Errorf("extracted module tree: %w", err)
	}

	rawInfo, err := os.ReadFile(filepath.Clean(info))
	if err != nil {
		return resolved, fmt.Errorf("module metadata: %w", err)
	}
	var metadata struct {
		Origin struct {
			Hash string `json:"Hash"`
			URL  string `json:"URL"`
		} `json:"Origin"`
	}
	if err := json.Unmarshal(rawInfo, &metadata); err != nil {
		return resolved, fmt.Errorf("parse module metadata: %w", err)
	}
	resolved.OriginCommit, resolved.OriginURL = metadata.Origin.Hash, metadata.Origin.URL
	return resolved, nil
}

func goEnv(name string) (string, error) {
	out, err := exec.Command("go", "env", name).Output()
	if err != nil {
		return "", fmt.Errorf("go env %s: %w", name, err)
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", fmt.Errorf("go env %s is empty", name)
	}
	return value, nil
}

func gitOutput(repo string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
