package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The query block is the shape all twenty-five list surfaces filter with, and
// isQueryBlock is what decides whether a name collision with an SDK field is
// expected or alarming. Getting that wrong in either direction is costly: too
// loose and a real binding is silently declared invented, too tight and the
// refusal fires on twenty-odd surfaces and stops meaning anything.
func TestIsQueryBlock(t *testing.T) {
	for name, testCase := range map[string]struct {
		members map[string]any
		want    bool
	}{
		"the filter block":    {map[string]any{"name": nil, "value": nil}, true},
		"a third member":      {map[string]any{"name": nil, "value": nil, "op": nil}, false},
		"a missing value":     {map[string]any{"name": nil}, false},
		"two other members":   {map[string]any{"field": nil, "match": nil}, false},
		"no members":          {map[string]any{}, false},
		"value without a key": {map[string]any{"value": nil, "other": nil}, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isQueryBlock(testCase.members); got != testCase.want {
				t.Errorf("isQueryBlock(%v) = %v, want %v", testCase.members, got, testCase.want)
			}
		})
	}
}

// The reason a member is invented is the only place a reader is told why
// filter.name is not bound to the SDK's name field. If it read the same for
// both members it would be decoration.
func TestInventedReasonDistinguishesKeyFromValue(t *testing.T) {
	key := inventedReason("filter", "name", true, true)
	value := inventedReason("filter", "value", true, false)

	if !strings.Contains(key, "KEY") || !strings.Contains(value, "VALUE") {
		t.Fatalf("the two members are not distinguished:\n  name:  %s\n  value: %s", key, value)
	}
	// The collision is the thing a reader stops at, so the answer belongs in
	// the reason rather than in someone's memory of this conversation.
	if !strings.Contains(key, "still not bound") {
		t.Errorf("a colliding key does not say why it is still invented: %s", key)
	}
	if plain := inventedReason("filter", "name", true, false); strings.Contains(plain, "still not bound") {
		t.Errorf("a key with no collision explains a collision that did not happen: %s", plain)
	}
}

// A block that is not the query shape and whose member shares an SDK field name
// must stop the tool rather than produce a policy asserting something it cannot
// know.
func TestRefusesAnUnrecognisedBlockThatCollides(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}

	code := run(scaffoldArgs(t, dir,
		`{"source":{"specification_sha256":"abc"},"resource":{"fields":[{"name":"zone_id"}]}}`,
		`{"list_resource_schemas":{"unifi_thing":{"block":{
			"description":"List things.",
			"attributes":{"site":{"type":"string","optional":true,"description":"The site."}},
			"block_types":{"scope":{"nesting_mode":"list","block":{"attributes":{
				"zone_id":{"type":"string","required":true,"description":"Zone."},
				"depth":{"type":"string","optional":true,"description":"Depth."},
				"mode":{"type":"string","optional":true,"description":"Mode."}
			}}}}
		}}}}`), &bytes.Buffer{}, stderr)

	if code == 0 {
		t.Fatal("the tool wrote a policy for a block it cannot reason about")
	}
	for _, want := range []string{"scope.zone_id", "not the key/value query shape", "write the member by hand"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the refusal does not mention %q:\n%s", want, stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "policy.json")); err == nil {
		t.Error("the tool refused and wrote the policy anyway")
	}
}

// The ordinary case must stay ordinary. filter.name collides with an SDK field
// on most surfaces; if that alone refused, the tool would refuse twenty-odd of
// the twenty-five.
func TestAcceptsTheQueryBlockDespiteACollision(t *testing.T) {
	dir := t.TempDir()
	stderr := &bytes.Buffer{}

	code := run(scaffoldArgs(t, dir,
		`{"source":{"specification_sha256":"abc"},"resource":{"fields":[{"name":"name"},{"name":"_id"}]}}`,
		`{"list_resource_schemas":{"unifi_thing":{"block":{
			"description":"List things.",
			"attributes":{"site":{"type":"string","optional":true,"description":"The site."}},
			"block_types":{"filter":{"nesting_mode":"list","block":{"attributes":{
				"name":{"type":"string","required":true,"description":"Which key."},
				"value":{"type":"string","required":true,"description":"What to match."}
			}}}}
		}}}}`), &bytes.Buffer{}, stderr)

	if code != 0 {
		t.Fatalf("the tool refused the ordinary case: %s", stderr)
	}

	var policy struct {
		SurfaceKind   string                         `json:"surface_kind"`
		Fields        []struct{ Disposition string } `json:"fields"`
		ProviderOwned []struct {
			TerraformName string `json:"terraform_name"`
		} `json:"provider_owned"`
		Groupings []struct {
			TerraformType string `json:"terraform_type"`
			Members       []struct {
				TerraformName string `json:"terraform_name"`
				Invented      string `json:"invented"`
			} `json:"members"`
		} `json:"groupings"`
	}
	body, err := os.ReadFile(filepath.Join(dir, "policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &policy); err != nil {
		t.Fatal(err)
	}

	if policy.SurfaceKind != "list_resource" {
		t.Errorf("surface_kind is %q", policy.SurfaceKind)
	}
	// Every SDK field omitted is the claim that makes the policy readable: this
	// config schema exposes none of the listed resource's fields.
	if len(policy.Fields) != 2 {
		t.Errorf("the policy classifies %d SDK fields, want both", len(policy.Fields))
	}
	for _, field := range policy.Fields {
		if field.Disposition != "omitted" {
			t.Errorf("an SDK field is %q, want omitted", field.Disposition)
		}
	}
	if len(policy.Groupings) != 1 || policy.Groupings[0].TerraformType != "list_nested_block" {
		t.Fatalf("the filter block is %+v, want one list_nested_block", policy.Groupings)
	}
	for _, member := range policy.Groupings[0].Members {
		if member.Invented == "" {
			t.Errorf("member %q is not declared invented", member.TerraformName)
		}
	}
}

func scaffoldArgs(t *testing.T, dir, bootstrap, schema string) []string {
	t.Helper()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	return []string{
		"-bootstrap", write("bootstrap.json", bootstrap),
		"-schema", write("schema.json", schema),
		"-digests", write("digests.json", `{"schema_sha256":{"list_resource_schemas.unifi_thing":"deadbeef"}}`),
		"-resource", "unifi_thing",
		"-generator-name", "thing",
		"-output", filepath.Join(dir, "policy.json"),
	}
}
