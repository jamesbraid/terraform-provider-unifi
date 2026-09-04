package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// behaviorModuleDir lays out a fake module root carrying just
// schemas/behavior.json, the only file writeBehaviorArtifact reads.
func behaviorModuleDir(t *testing.T, artifact string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "schemas"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schemas", "behavior.json"), []byte(artifact), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func Test_writeBehaviorArtifactWrapsTheArtifactAndStampsItsSource(t *testing.T) {
	artifact := `{"controller_version":"10.6.101","writes":{"Nat":{"required_on_create":["protocol"]}}}`
	moduleDir := behaviorModuleDir(t, artifact)
	output := filepath.Join(t.TempDir(), "behavior.json")
	source := bootstrapSource{
		Repository: "github.com/jamesbraid/go-unifi",
		Version:    "v1.113.1",
		Commit:     "7d5c1431100adeadbeefdeadbeefdeadbeefdead",
	}

	if err := writeBehaviorArtifact(source, moduleDir, output); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Error("the wrapper does not end with a newline")
	}
	var document struct {
		FormatVersion int             `json:"format_version"`
		Source        bootstrapSource `json:"source"`
		Behavior      json.RawMessage `json:"behavior"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse the wrapper: %v", err)
	}
	if document.FormatVersion != 1 {
		t.Errorf("format_version = %d, want 1", document.FormatVersion)
	}
	if document.Source.Repository != source.Repository ||
		document.Source.Version != source.Version ||
		document.Source.Commit != source.Commit {
		t.Errorf("source = %+v, want the resolved module's identity carried through", document.Source)
	}
	sum := sha256.Sum256([]byte(artifact))
	if want := hex.EncodeToString(sum[:]); document.Source.SpecificationSHA256 != want {
		t.Errorf("specification_sha256 = %q, want the digest of the artifact bytes %q",
			document.Source.SpecificationSHA256, want)
	}
	var behavior struct {
		Writes map[string]struct {
			RequiredOnCreate []string `json:"required_on_create"`
		} `json:"writes"`
	}
	if err := json.Unmarshal(document.Behavior, &behavior); err != nil {
		t.Fatalf("parse the wrapped behaviour: %v", err)
	}
	if got := behavior.Writes["Nat"].RequiredOnCreate; len(got) != 1 || got[0] != "protocol" {
		t.Errorf("wrapped writes.Nat.required_on_create = %v, want [protocol] carried verbatim", got)
	}
}

func Test_writeBehaviorArtifactIsDeterministic(t *testing.T) {
	moduleDir := behaviorModuleDir(t, `{"writes":{}}`)
	source := bootstrapSource{Repository: "r", Version: "v", Commit: "c"}
	outputs := make([][]byte, 2)
	for i := range outputs {
		output := filepath.Join(t.TempDir(), "behavior.json")
		if err := writeBehaviorArtifact(source, moduleDir, output); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		outputs[i] = raw
	}
	if !bytes.Equal(outputs[0], outputs[1]) {
		t.Error("two runs over the same module produced different wrapper bytes")
	}
}

func Test_writeBehaviorArtifactRefusesAModuleWithoutTheArtifact(t *testing.T) {
	err := writeBehaviorArtifact(bootstrapSource{}, t.TempDir(), filepath.Join(t.TempDir(), "behavior.json"))
	if err == nil || !strings.Contains(err.Error(), "behavior.json") {
		t.Fatalf("writeBehaviorArtifact() error = %v, want it to name the missing artifact", err)
	}
}

func Test_writeBehaviorArtifactRefusesAnInvalidArtifact(t *testing.T) {
	moduleDir := behaviorModuleDir(t, `{"writes":`)
	err := writeBehaviorArtifact(bootstrapSource{}, moduleDir, filepath.Join(t.TempDir(), "behavior.json"))
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("writeBehaviorArtifact() error = %v, want a refusal to wrap invalid JSON", err)
	}
}

// The two modes take disjoint flags; a directive mixing them is a typo, not
// a request this tool should guess its way through.
func Test_runRefusesBehaviorOutputAlongsideAStructBootstrap(t *testing.T) {
	var stderr bytes.Buffer
	exitCode := run([]string{
		"-package", "example.com/pkg",
		"-behavior-output", "behavior.json",
		"-struct", "Nat",
	}, &stderr)
	if exitCode != 2 || !strings.Contains(stderr.String(), "behavior-output stands alone") {
		t.Fatalf("run() = %d, stderr %q; want refusal naming the flag conflict", exitCode, stderr.String())
	}
}
