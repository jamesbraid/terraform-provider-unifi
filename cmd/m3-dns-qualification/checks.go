package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The assertions in this file were jq one-liners inside a script that only ran
// with a controller, twelve Docker volumes and an x86_64 daemon. Nothing could
// exercise them, so nothing did. They are ordinary functions here for one
// reason: they can be tested without any of that.

// recordAddress is the resource every fixture under .woodpecker/fixtures/m3
// declares. It is a constant rather than a parameter because the fixtures and
// these assertions have to agree, and a parameter invites them to drift.
const recordAddress = "unifi_dns_record.test"

// normalizeRecordState reproduces
//
//	jq --sort-keys '.values.root_module.resources[]
//	    | select(.address == "unifi_dns_record.test") | .values | del(.id)'
//
// with one deliberate difference: it refuses to normalize a state that does not
// contain the record.
//
// The shell could not refuse. jq prints nothing when the select matches
// nothing, so the redirect left an EMPTY file. Four empty files compare equal,
// so the three cmp calls that stand behind
// bidirectional_adapter_state_round_trip all passed, and sha256 of an empty
// file is still 64 hex characters, so validHex accepted the digest downstream.
// A run that silently lost the resource in every adapter produced the same
// receipt as a run where all four agreed -- the claim held vacuously, which is
// the one way it must never hold.
func normalizeRecordState(showJSON []byte) ([]byte, error) {
	var document struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address string          `json:"address"`
					Values  json.RawMessage `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	decoder := json.NewDecoder(bytes.NewReader(showJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("state is not valid terraform show -json output: %w", err)
	}

	var matched []json.RawMessage
	for _, resource := range document.Values.RootModule.Resources {
		if resource.Address == recordAddress {
			matched = append(matched, resource.Values)
		}
	}
	switch len(matched) {
	case 1:
	case 0:
		return nil, fmt.Errorf("state contains no resource at %s, so there is nothing to compare "+
			"between CLIs; an empty normalization would make the round-trip claim hold vacuously",
			recordAddress)
	default:
		return nil, fmt.Errorf("state contains %d resources at %s; the address must select exactly one",
			len(matched), recordAddress)
	}

	values := map[string]any{}
	valueDecoder := json.NewDecoder(bytes.NewReader(matched[0]))
	valueDecoder.UseNumber()
	if err := valueDecoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("resource values are not a JSON object: %w", err)
	}
	// del(.id): the identifier is controller-assigned and differs per run, so it
	// is the one field that must not take part in the comparison.
	delete(values, "id")
	if len(values) == 0 {
		return nil, fmt.Errorf("resource at %s has no attributes besides id; comparing empty objects "+
			"across CLIs proves nothing", recordAddress)
	}

	return encodeSorted(values)
}

// encodeSorted matches jq --sort-keys: two-space indent, keys sorted at every
// level, one trailing newline. encoding/json sorts map keys on its own, and
// UseNumber above keeps numeric literals byte-exact instead of routing them
// through float64.
func encodeSorted(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// assertReplacementPlan reproduces the jq that required the planned change for
// the record to be exactly one entry containing both create and delete -- a
// replacement, not an in-place update and not a bare create.
func assertReplacementPlan(planJSON []byte) error {
	var plan struct {
		ResourceChanges []struct {
			Address string `json:"address"`
			Change  struct {
				Actions []string `json:"actions"`
			} `json:"change"`
		} `json:"resource_changes"`
	}
	if err := json.Unmarshal(planJSON, &plan); err != nil {
		return fmt.Errorf("replacement plan is not valid terraform show -json output: %w", err)
	}

	var actions [][]string
	for _, change := range plan.ResourceChanges {
		if change.Address == recordAddress {
			actions = append(actions, change.Change.Actions)
		}
	}
	if len(actions) != 1 {
		return fmt.Errorf("replacement plan has %d changes for %s, want exactly 1",
			len(actions), recordAddress)
	}
	var hasCreate, hasDelete bool
	for _, action := range actions[0] {
		switch action {
		case "create":
			hasCreate = true
		case "delete":
			hasDelete = true
		}
	}
	if !hasCreate || !hasDelete {
		return fmt.Errorf("replacement plan for %s has actions %v, want both create and delete "+
			"(an in-place update here means the field stopped forcing replacement)",
			recordAddress, actions[0])
	}
	return nil
}

// assertUpgradeState reproduces the two state-upgrade assertions: schema
// version 0 carries ttl as the integer 300, and after the upgrade schema
// version 1 carries it as the string "5m0s". Passing the wanted ttl as a
// json.RawMessage keeps the integer/string distinction, which is the entire
// point of the check -- comparing them as strings would let 300 and "300"
// through.
func assertUpgradeState(showJSON []byte, wantSchemaVersion int, wantTTL string) error {
	var document struct {
		Values struct {
			RootModule struct {
				Resources []struct {
					Address       string `json:"address"`
					SchemaVersion *int   `json:"schema_version"`
					Values        struct {
						TTL json.RawMessage `json:"ttl"`
					} `json:"values"`
				} `json:"resources"`
			} `json:"root_module"`
		} `json:"values"`
	}
	if err := json.Unmarshal(showJSON, &document); err != nil {
		return fmt.Errorf("upgrade state is not valid terraform show -json output: %w", err)
	}

	for _, resource := range document.Values.RootModule.Resources {
		if resource.Address != recordAddress {
			continue
		}
		if resource.SchemaVersion == nil {
			return fmt.Errorf("upgrade state for %s reports no schema_version", recordAddress)
		}
		if *resource.SchemaVersion != wantSchemaVersion {
			return fmt.Errorf("upgrade state for %s is schema_version %d, want %d",
				recordAddress, *resource.SchemaVersion, wantSchemaVersion)
		}
		if string(resource.Values.TTL) != wantTTL {
			return fmt.Errorf("upgrade state for %s carries ttl %s, want %s",
				recordAddress, string(resource.Values.TTL), wantTTL)
		}
		return nil
	}
	return fmt.Errorf("upgrade state contains no resource at %s", recordAddress)
}

// installPrebuiltCandidate replaces m3-evidence-lib.sh. The shell tested that
// the source existed and was executable, installed it 0755, and compared the
// two files. This does the same and reports which of those failed.
func installPrebuiltCandidate(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("candidate provider binary %s: %w", source, err)
	}
	if info.IsDir() {
		return fmt.Errorf("candidate provider binary %s is a directory", source)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("candidate provider binary %s is not executable (mode %v)", source, info.Mode().Perm())
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}

	contents, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.WriteFile(destination, contents, 0o755); err != nil {
		return err
	}
	// The shell finished with cmp -s. Keep it: an install that silently
	// truncated would otherwise be discovered as a provider that fails to load,
	// several minutes and one controller later.
	installed, err := os.ReadFile(destination)
	if err != nil {
		return err
	}
	if !bytes.Equal(contents, installed) {
		return fmt.Errorf("installed candidate at %s differs from %s", destination, source)
	}
	return nil
}

// sha256Reader is separated so the digest of a downloaded archive and the
// digest of a file on disk go through one implementation.
func copyAndDigest(destination io.Writer, source io.Reader) (string, error) {
	digest := newDigest()
	if _, err := io.Copy(io.MultiWriter(destination, digest), source); err != nil {
		return "", err
	}
	return digest.hex(), nil
}
