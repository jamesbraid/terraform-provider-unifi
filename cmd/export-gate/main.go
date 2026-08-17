// Command export-gate answers, for a tree that is about to be published: what
// would go with it?
//
// It exits 0 or 1 and writes no receipt. A receipt here would be a claim about
// a publication that has not happened, and the previous attempt at this gate
// finished by emitting four literal `true` values, two of which corresponded to
// no code at all -- which the release qualifier then checked. There is nothing
// here to mistake for evidence.
//
// BUILDING THIS IS NOT PERMISSION TO USE IT. Nothing is pushed anywhere by this
// command; it reads a repository and reports. Whether the provider is ever
// published, and when, is a decision for its owner on a separate occasion. This
// exists so that decision can be taken with the answer in hand instead of a
// hope.
package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/exportgate"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "export gate: %v\n", err)
		os.Exit(1)
	}
}

func run(argv []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("export-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	policyPath := flags.String("policy", ".woodpecker/policy/export-denylist.json",
		"committed declaration of what must never ship")
	revision := flags.String("revision", "HEAD", "tree to archive and judge")
	since := flags.String("since", "",
		"start of the published commit range, exclusive; empty means every commit reachable from -revision")
	skipGitleaks := flags.Bool("skip-gitleaks", false,
		"skip the gitleaks pass; it still runs every other rule, and says so in the summary")
	if err := flags.Parse(argv); err != nil {
		return err
	}

	policy, err := loadPolicy(*policyPath)
	if err != nil {
		return err
	}

	// THE POLICY MUST NOT SHIP. It names, or stands in for, exactly the things
	// that must never be published; a declaration living in a path the archive
	// includes would be published along with everything else. Checked here
	// rather than trusted, because it is the one file whose exposure would be
	// caused by the gate itself.
	if !policyIsDenied(*policyPath, policy) {
		return fmt.Errorf("the policy file %s is not covered by its own denied_paths, so "+
			"publishing this tree would publish the denylist that describes what must not be "+
			"published", *policyPath)
	}

	archive, err := gitArchive(*revision)
	if err != nil {
		return err
	}
	findings, err := exportgate.Inspect(bytes.NewReader(archive), policy)
	if err != nil {
		return err
	}

	messages, err := commitMessages(*revision, *since)
	if err != nil {
		return err
	}
	messageFindings, err := exportgate.InspectCommitMessages(messages, policy)
	if err != nil {
		return err
	}
	findings = append(findings, messageFindings...)

	gitleaksRan := false
	if !*skipGitleaks {
		leaks, err := runGitleaks(archive)
		if err != nil {
			return err
		}
		gitleaksRan = true
		findings = append(findings, leaks...)
	}

	for _, finding := range findings {
		fmt.Fprintf(stdout, "%s\n", finding)
	}
	fmt.Fprintf(stdout, "\nexamined %s over %d commit(s); gitleaks %s\n",
		*revision, len(messages), map[bool]string{true: "ran", false: "SKIPPED"}[gitleaksRan])
	if len(findings) > 0 {
		return fmt.Errorf("%d finding(s); this tree must not be published as it stands", len(findings))
	}
	fmt.Fprintf(stdout, "no findings\n")
	return nil
}

func loadPolicy(path string) (exportgate.Policy, error) {
	var policy exportgate.Policy
	data, err := os.ReadFile(path)
	if err != nil {
		return policy, fmt.Errorf("reading policy: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, fmt.Errorf("%s: %w", path, err)
	}
	return policy, policy.Validate()
}

// policyIsDenied asks whether the policy's own path is covered by its
// denied_paths, using the same matcher the archive rules use.
func policyIsDenied(path string, policy exportgate.Policy) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	findings, err := exportgate.Inspect(singleFileArchive(clean), policy)
	if err != nil {
		return false
	}
	for _, finding := range findings {
		if finding.Rule == exportgate.RuleDeniedPath && finding.Path == clean {
			return true
		}
	}
	return false
}

// singleFileArchive builds a one-entry tar so the policy's own path can be run
// through the same matcher the archive rules use. Asking the real matcher is
// the point: a second implementation of "is this path denied" could disagree
// with the first, and then the self-check would be reassuring and wrong.
func singleFileArchive(name string) io.Reader {
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	body := []byte("policy self-check\n")
	_ = writer.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	})
	_, _ = writer.Write(body)
	_ = writer.Close()
	return bytes.NewReader(buffer.Bytes())
}

func gitArchive(revision string) ([]byte, error) {
	var out, errs bytes.Buffer
	command := exec.Command("git", "archive", "--format=tar", revision)
	command.Stdout = &out
	command.Stderr = &errs
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("git archive %s: %w: %s", revision, err, errs.String())
	}
	if out.Len() == 0 {
		return nil, fmt.Errorf("git archive %s produced no output", revision)
	}
	return out.Bytes(), nil
}

func commitMessages(revision, since string) (map[string]string, error) {
	spec := revision
	if since != "" {
		spec = since + ".." + revision
	}
	var out, errs bytes.Buffer
	command := exec.Command("git", "log", "--format=%H%x00%B%x1e", spec)
	command.Stdout = &out
	command.Stderr = &errs
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("git log %s: %w: %s", spec, err, errs.String())
	}

	messages := map[string]string{}
	for _, record := range strings.Split(out.String(), "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		commit, message, found := strings.Cut(record, "\x00")
		if !found {
			continue
		}
		messages[commit] = message
	}
	return messages, nil
}

// runGitleaks unpacks the archive and scans it. The archive is scanned rather
// than the working tree for the same reason every other rule uses it: the
// working tree is not what ships.
func runGitleaks(archive []byte) ([]exportgate.Finding, error) {
	directory, err := os.MkdirTemp("", "export-gate-archive.")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)

	extract := exec.Command("tar", "-x", "-C", directory)
	extract.Stdin = bytes.NewReader(archive)
	var extractErrs bytes.Buffer
	extract.Stderr = &extractErrs
	if err := extract.Run(); err != nil {
		return nil, fmt.Errorf("unpacking the archive for gitleaks: %w: %s", err, extractErrs.String())
	}
	// A gitleaks pass over an empty directory reports nothing, which is
	// indistinguishable from a clean tree. Establish there is something to scan.
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) == 0 {
		return nil, fmt.Errorf("the unpacked archive is empty, so gitleaks would scan nothing " +
			"and report clean")
	}

	var out bytes.Buffer
	command := exec.Command("gitleaks", "detect", "--no-git", "--redact", "--no-banner",
		"--source", directory)
	command.Stdout = &out
	command.Stderr = &out
	err = command.Run()
	if err == nil {
		return nil, nil
	}
	if _, isExit := err.(*exec.ExitError); !isExit {
		return nil, fmt.Errorf("running gitleaks: %w (is it installed?)", err)
	}
	return []exportgate.Finding{{
		Rule:   "gitleaks",
		Path:   "(archive)",
		Detail: strings.TrimSpace(out.String()),
	}}, nil
}
