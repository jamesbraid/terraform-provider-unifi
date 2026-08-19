package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

func TestRunRejectsIncompleteArguments(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(nil, &stderr); code != 2 {
		t.Fatalf("run(nil) = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "ledger") ||
		!strings.Contains(stderr.String(), "contract-output") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	// Confidentiality is deliberately absent from this list: nothing produces
	// that receipt, so requiring it made the gate unsatisfiable. If it returns
	// to the required set, it should be because a producer exists.
	if strings.Contains(stderr.String(), "confidentiality") {
		t.Errorf("confidentiality is listed as required again, but no producer writes one:\n  %s",
			stderr.String())
	}
}

func TestDecodeStrictFileRejectsUnknownAndTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	for name, data := range map[string]string{
		"unknown":  `{"format_version":1,"extra":true}`,
		"trailing": `{"format_version":1}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name+".json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			// Any receipt type this command still decodes will do: the assertion is
			// about DisallowUnknownFields and the trailing-value check, not about
			// which receipt. FleetSoakReceipt was the original target and was pruned.
			var receipt releasequalification.MigrationRecoveryReceipt
			if _, err := cmdio.DecodeStrictFile(path, &receipt); err == nil {
				t.Fatal("cmdio.DecodeStrictFile() succeeded")
			}
		})
	}
}

// TestWriteAtomicUsesPrivatePermissions now exercises the shared writer through
// the exact call this command makes -- the bare form, no options. It asserts
// what it always asserted: the parent directory is created and the artifact is
// readable only by its owner. Pointed at cmdio rather than deleted, so the
// assertion still runs for THIS command's chosen behaviour.
func TestWriteAtomicUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "receipt.json")
	if err := cmdio.WriteAtomic(path, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o, want 600", got)
	}
}

// TestEveryInputIsRequiredAndItsAbsenceIsNamed is the refusal half of the gate.
//
// TestRunRejectsIncompleteArguments above covers an EMPTY invocation, which the
// flag block catches before a single file is opened. Nothing covered the case
// that actually happens in a pipeline: every flag supplied, and one of the six
// receipts not written by the step above. A gate that skipped a missing input
// would report release-ready on five receipts and one absence, and its own
// tests would stay green -- which is the shape this repository keeps finding.
//
// THE BASELINE IS THE POSITIVE CONTROL, and it is what stops each case below
// passing for the wrong reason. With all six files present the run must get
// past every decode and fail in BuildReleaseReadyArtifacts instead, so the
// error is labelled "release-ready". If that assertion ever fails because the
// build SUCCEEDED, the gate accepts six empty receipts and the failure is real
// rather than a broken test.
//
// The receipts are `{}` on purpose. What is under test is that an absent file
// stops the run and says which one, and that needs no valid receipt -- only a
// decodable one. Whether a populated receipt is judged correctly is the
// subject of releasequalification's own mutation tests.
func TestEveryInputIsRequiredAndItsAbsenceIsNamed(t *testing.T) {
	inputs := []struct{ flag, label string }{
		{"ledger", "ledger:"},
		{"management-contract", "management contract:"},
		{"migration-recovery", "migration/recovery:"},
		{"hardware-disposition", "hardware disposition:"},
		{"dependency-publishability", "dependency publishability:"},
		{"confidentiality", "confidentiality:"},
	}

	build := func(t *testing.T) (args []string, files map[string]string, outputs []string) {
		t.Helper()
		root := t.TempDir()
		files = map[string]string{}
		for _, input := range inputs {
			path := filepath.Join(root, input.flag+".json")
			if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			files[input.flag] = path
			args = append(args, "-"+input.flag, path)
		}
		for _, name := range []string{"receipt-output", "ledger-output", "contract-output"} {
			path := filepath.Join(root, name+".json")
			outputs = append(outputs, path)
			args = append(args, "-"+name, path)
		}
		return args, files, outputs
	}

	t.Run("all six present reaches the judgement", func(t *testing.T) {
		args, _, _ := build(t)
		var stderr bytes.Buffer
		code := run(args, &stderr)
		if code != 1 {
			t.Fatalf("run() = %d, want 1: six empty receipts must not be judged release-ready", code)
		}
		if !strings.Contains(stderr.String(), "release-ready:") {
			t.Fatalf("run() failed before reaching the judgement, so the cases below would\n"+
				"prove nothing about a missing file.\n stderr: %s", stderr.String())
		}
	})

	// Omitting the flag is not the same as naming a file that is not there. The
	// first is the only shape this gate can run in today; the second is a broken
	// invocation and still has to fail.
	t.Run("omitting the confidentiality flag entirely is permitted", func(t *testing.T) {
		args, files, _ := build(t)
		var kept []string
		for i := 0; i < len(args); i += 2 {
			if args[i] != "-confidentiality" {
				kept = append(kept, args[i], args[i+1])
			}
		}
		if err := os.Remove(files["confidentiality"]); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		code := run(kept, &stderr)
		if code == 2 {
			t.Fatalf("run() = 2 without -confidentiality; it is no longer a required flag.\n stderr: %s",
				stderr.String())
		}
		if strings.Contains(stderr.String(), "confidentiality:") {
			t.Errorf("run() tried to read a confidentiality receipt that was never named.\n stderr: %s",
				stderr.String())
		}
	})

	for _, missing := range inputs {
		t.Run("naming a missing "+missing.flag, func(t *testing.T) {
			args, files, outputs := build(t)
			if err := os.Remove(files[missing.flag]); err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			code := run(args, &stderr)
			if code == 0 {
				t.Fatalf("run() = 0 with %s absent; the gate reported release-ready on five receipts", missing.flag)
			}
			if !strings.Contains(stderr.String(), missing.label) {
				t.Errorf("run() refused, but did not name the absent input.\n want mention of: %s\n stderr: %s",
					missing.label, stderr.String())
			}
			// A refused run must leave no artifact behind: a half-written set is
			// worse than none, because the next step reads it as evidence.
			for _, path := range outputs {
				if _, err := os.Stat(path); err == nil {
					t.Errorf("%s was written even though the run was refused", filepath.Base(path))
				}
			}
		})
	}
}
