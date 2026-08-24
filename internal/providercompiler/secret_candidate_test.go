package providercompiler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The secret-candidate check refuses a bootstrap field whose name says it
// carries a credential unless the policy has disposed of it safely --
// omitted, or exposed and marked sensitive. The lookup must search all four
// places a policy can dispose of a field: fields, groupings, flattenings
// and claims, not just fields.
func secretCandidateInput(t *testing.T) CompileInput {
	t.Helper()
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	return CompileInput{
		Bootstrap: read("../../provider-codegen/bootstrap/go-unifi-v1.103.0-vpn-server.json"),
		Policy:    read("../../provider-codegen/policy/vpn_server.json"),
	}
}

// TestCompileAcceptsASecretCandidateDispositionedByAGrouping compiles the
// real vpn_server, whose x_ fields are all declared as grouping members
// rather than top-level fields.
func TestCompileAcceptsASecretCandidateDispositionedByAGrouping(t *testing.T) {
	if _, err := Compile(secretCandidateInput(t)); err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
}

// TestCompileRefusesASecretCandidateWithNoDisposition removes one grouping
// member and nothing else, so the field remains a secret candidate on the
// bootstrap with no disposition anywhere. The policy is edited in memory,
// not on disk, since this suite runs beside generators that refuse to run
// against a dirty tree. It asserts the message names all four places
// searched, not just "fields" -- which is exactly where the field is not.
func TestCompileRefusesASecretCandidateWithNoDisposition(t *testing.T) {
	input := secretCandidateInput(t)

	var document map[string]any
	if err := json.Unmarshal(input.Policy, &document); err != nil {
		t.Fatal(err)
	}
	groupings, _ := document["groupings"].([]any)
	removed := 0
	for _, raw := range groupings {
		group, _ := raw.(map[string]any)
		members, _ := group["members"].([]any)
		kept := members[:0]
		for _, entry := range members {
			member, _ := entry.(map[string]any)
			if member["structural_name"] == "x_auth_key" {
				removed++
				continue
			}
			kept = append(kept, entry)
		}
		group["members"] = kept
	}
	if removed != 1 {
		t.Fatalf("removed %d members named x_auth_key, want exactly 1 -- the fixture no longer describes the policy", removed)
	}
	edited, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	input.Policy = edited

	_, err = Compile(input)
	if err == nil {
		t.Fatal("Compile() accepted a secret candidate with no disposition in any of the four places")
	}
	for _, want := range []string{"x_auth_key", "fields", "groupings", "flattenings", "claims"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Compile() error = %v, want it to mention %q", err, want)
		}
	}
}
