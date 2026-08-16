package main

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

// TestShellReceiptFieldsAreKnownToTheirConsumers reads the field set each shell
// producer emits straight out of its jq program and compares it against the json
// tags of the Go type that decodes it.
//
// THE SEAM. These receipts are written by shell into /tmp during a pipeline run
// and decoded by Go with DisallowUnknownFields. A producer that starts emitting
// one new field breaks its consumer at runtime, and NOTHING in the Go suite can
// see it, because the file only exists inside a pipeline. That is exactly what
// 61732c45 did with tree_state: build, vet, `go test ./...` and `go generate`
// were all green across the break.
//
// WHY NOT A COMMITTED FIXTURE OF EACH RECEIPT, which was the first plan. A
// fixture goes stale silently unless regenerating it is part of changing the
// producer, and the machinery that would regenerate it -- catalog-build-schema_test.sh
// and catalog-unit-differential_test.sh -- is invoked by nothing. A guard whose
// safety depends on tests that do not run is the disease it is meant to catch.
// Reading the producer directly cannot go stale, because there is nothing to
// regenerate.
//
// THE FAILURE MODE THIS HAS, stated because every check has one and the useful
// question is which direction it fails in. Finding the receipt literal in a
// shell script is heuristic: a script may build several jq objects, and picking
// the wrong one would compare the wrong field set and report whatever that
// comparison happened to say. Measured while writing this --
// catalog-controller-differential.sh has a plan-building literal before its
// receipt, and a naive "first object mentioning format_version" picked it and
// returned a single key.
//
// So the anchor is a floor rather than a better guess: a candidate object counts
// as the receipt only if its TOP-LEVEL keys include both format_version and
// gate, and a script where no object qualifies FAILS rather than being skipped.
// Misidentification is then loud, which is the direction to be wrong in.
func TestShellReceiptFieldsAreKnownToTheirConsumers(t *testing.T) {
	producers := []struct {
		script   string
		consumer string
		target   any
	}{{
		script:   ".woodpecker/scripts/catalog-build-schema.sh",
		consumer: "catalogparity.BuildSchemaReceipt, decoded by cmd/catalog-admission and cmd/catalog-migration-recovery",
		target:   catalogparity.BuildSchemaReceipt{},
	}, {
		script:   ".woodpecker/scripts/catalog-unit-differential.sh",
		consumer: "catalogparity.UnitDifferentialReceipt, decoded by cmd/catalog-admission",
		target:   catalogparity.UnitDifferentialReceipt{},
	}, {
		script:   ".woodpecker/scripts/catalog-controller-differential.sh",
		consumer: "catalogparity.ControllerDifferentialReceipt, decoded by cmd/catalog-admission and cmd/catalog-migration-recovery",
		target:   catalogparity.ControllerDifferentialReceipt{},
	}}

	checked := 0
	for _, producer := range producers {
		body, err := os.ReadFile(producer.script)
		if err != nil {
			t.Errorf("%s: %v", producer.script, err)
			continue
		}
		emitted, found := receiptKeys(string(body))
		if !found {
			t.Errorf("%s: no jq object with both format_version and gate at top level.\n"+
				"    This test cannot tell which object is the receipt, so it is refusing rather\n"+
				"    than comparing something it guessed at. Either the receipt stopped carrying\n"+
				"    those two keys, or it is now built somewhere this cannot read -- and in both\n"+
				"    cases the shell-to-Go seam is unguarded until it is fixed.",
				producer.script)
			continue
		}
		known := jsonTags(producer.target)
		var unknown []string
		for _, key := range emitted {
			if !known[key] {
				unknown = append(unknown, key)
			}
		}
		sort.Strings(unknown)
		if len(unknown) > 0 {
			t.Errorf("%s emits %d field(s) that %s does not carry:\n    %s\n\n"+
				"    That consumer uses DisallowUnknownFields, so this is a runtime failure in the\n"+
				"    pipeline rather than a style question -- and no other test can see it, because\n"+
				"    the receipt only exists inside a run. Teach the type about the field BEFORE the\n"+
				"    producer emits it; the other order is what broke the pipeline at 61732c45.",
				producer.script, len(unknown), producer.consumer, strings.Join(unknown, "\n    "))
			continue
		}
		checked++
		t.Logf("%s: %d emitted field(s), all known to its consumer", producer.script, len(emitted))
	}

	// Every assertion above is inside the loop, so an empty producer list or a
	// run where each script failed to parse would report success having compared
	// nothing.
	if checked != len(producers) {
		t.Errorf("compared %d of %d producers; the rest are reported above", checked, len(producers))
	}
}

// receiptKeys returns the top-level keys of the first jq object in the script
// whose keys include both format_version and gate.
//
// Shell single quotes cannot nest or be escaped inside a single-quoted string,
// so scanning for quote pairs is exact rather than approximate. What is heuristic
// is WHICH object is the receipt, and that is what the two-key floor decides.
func receiptKeys(script string) ([]string, bool) {
	for index := 0; index < len(script); {
		open := strings.IndexByte(script[index:], '\'')
		if open < 0 {
			return nil, false
		}
		open += index
		shut := strings.IndexByte(script[open+1:], '\'')
		if shut < 0 {
			return nil, false
		}
		shut += open + 1
		candidate := script[open+1 : shut]
		index = shut + 1
		if !strings.Contains(candidate, "format_version") {
			continue
		}
		keys := topLevelKeys(candidate)
		hasVersion, hasGate := false, false
		for _, key := range keys {
			switch key {
			case "format_version":
				hasVersion = true
			case "gate":
				hasGate = true
			}
		}
		if hasVersion && hasGate {
			return keys, true
		}
	}
	return nil, false
}

// topLevelKeys parses `key:` at depth one of the first brace-delimited object.
func topLevelKeys(body string) []string {
	start := strings.IndexByte(body, '{')
	if start < 0 {
		return nil
	}
	depth := 0
	end := -1
	for index := start; index < len(body); index++ {
		switch body[index] {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
			if depth == 0 && body[index] == '}' {
				end = index
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}

	var keys []string
	depth = 0
	token := strings.Builder{}
	flush := func() {
		text := strings.TrimSpace(token.String())
		token.Reset()
		colon := strings.IndexByte(text, ':')
		if colon <= 0 {
			return
		}
		name := strings.Trim(strings.TrimSpace(text[:colon]), `"`)
		if name != "" && !strings.ContainsAny(name, " \t\n$|.[]{}()") {
			keys = append(keys, name)
		}
	}
	for _, char := range body[start+1 : end] {
		switch char {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
		case ',':
			if depth == 0 {
				flush()
				continue
			}
		}
		token.WriteRune(char)
	}
	flush()
	return keys
}

// jsonTags returns the json names a type carries at its top level, following
// embedded structs the way encoding/json does.
func jsonTags(value any) map[string]bool {
	names := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		if typ.Kind() != reflect.Struct {
			return
		}
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			tag := strings.Split(field.Tag.Get("json"), ",")[0]
			if field.Anonymous && tag == "" {
				walk(field.Type)
				continue
			}
			if tag == "" || tag == "-" {
				names[field.Name] = true
				continue
			}
			names[tag] = true
		}
	}
	walk(reflect.TypeOf(value))
	return names
}
