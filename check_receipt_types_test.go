package main

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/releasequalification"
)

// TestEveryReceiptFitsItsConsumersType answers a question nothing in this
// repository asked: DOES WHAT A PRODUCER WRITES FIT THE TYPE THAT READS IT?
//
// Two instances were found in one night, both by accident while porting
// something else, both invisible to every existing check:
//
//   - catalog-dependency-publishability.sh writes a `tree` key.
//     DependencyPublishabilityReceipt has no such field, so
//     catalog-release-ready cannot decode the receipt at all. LATENT, because
//     no workflow runs that consumer.
//   - catalog-controller-differential.sh adds `diagnostic_selection` and
//     `catalog_test_count` to the plan when CATALOG_ACCEPTANCE_TEST_NAMES is
//     set. ControllerPlanReceipt has neither, so catalog-admission fails one
//     step later in the same workflow. LIVE, but only in the mode an operator
//     reaches for when something is already broken.
//
// WHY THE EXISTING CHECKS COULD NOT SEE EITHER. Decoding the four frozen
// receipts covers the shapes one recorded run happened to emit -- and that run
// was not a diagnostic run, so the second instance is outside its reach
// entirely. A gate in a workflow only runs when its pipeline does. And the
// consumer of the first has no caller at all, so nothing ever tried it.
//
// THE PRODUCER SIDE IS PARSED, NOT EXECUTED, and that is the point rather than
// a compromise. Running a producer gives you one mode -- the one your
// environment selects -- which is exactly the blindness that hid the second
// instance. The keys are literals in the script, including the ones only a
// non-default mode writes, so reading them finds modes that no run you can
// arrange would reach.
func TestEveryReceiptFitsItsConsumersType(t *testing.T) {
	for _, pair := range receiptPairs {
		t.Run(pair.name, func(t *testing.T) {
			script := filepath.Join(".woodpecker", "scripts", pair.producer)
			body, err := os.ReadFile(script)
			if err != nil {
				t.Fatalf("producer is missing: %v", err)
			}
			written := keysWrittenBy(string(body), pair.writes)
			if len(written) == 0 {
				t.Fatalf("no written keys found in %s; the extraction is not reaching the receipt, "+
					"so this case would pass by looking at nothing", pair.producer)
			}

			accepted := jsonFieldNames(pair.consumer)
			if len(accepted) == 0 {
				t.Fatalf("%T exposes no json tags; the comparison would be vacuous", pair.consumer)
			}

			var unknown []string
			for _, key := range written {
				if !accepted[key] {
					unknown = append(unknown, key)
				}
			}
			sort.Strings(unknown)

			// Both directions, as with every other ledger here: an entry that
			// no longer describes anything understates what is broken, and a
			// reader cannot tell a live excuse from a resolved one.
			if known, recorded := knownReceiptMismatches[pair.name]; recorded {
				if strings.Join(unknown, ",") == known.keys {
					t.Logf("known and open: %s -- %s", known.keys, known.why)
					return
				}
				t.Errorf("the recorded mismatch for %s no longer matches.\n recorded: %s\n found:    %s\n\n"+
					"    If it is fixed, remove the entry. If it changed, the entry is now describing\n"+
					"    a different defect than the one it was written for.",
					pair.name, known.keys, strings.Join(unknown, ","))
				return
			}

			if len(unknown) > 0 {
				t.Errorf("%s writes %d key(s) that %s cannot decode:\n    %s\n\n"+
					"    The receipt is produced and then refused, which is a disagreement between\n"+
					"    producer and consumer rather than a bad run. Either add the field to the\n"+
					"    type or stop writing the key -- and note which of the two is the decision:\n"+
					"    a key nobody reads is not automatically safe to keep.",
					pair.producer, len(unknown), pair.consumerName, strings.Join(unknown, "\n    "))
			}
		})
	}
}

// receiptPairs is the enumeration, and it is written out rather than discovered
// because the link between a script and the type that reads its output exists
// nowhere in the tree -- no import, no path, no name in common. That is the
// root of this whole class: there is nothing for a compiler to check.
//
// An entry is a claim that these two are a producer/consumer pair. Adding a
// producer without adding an entry leaves it unchecked, which is why
// TestEveryStrictConsumerIsPaired below counts the other side.
type receiptPair struct {
	name         string
	producer     string
	writes       *regexp.Regexp
	consumer     any
	consumerName string
}

// consumerPackage is the package the consuming type lives in, which is the
// granularity the strict-decoder walk reports.
func (p receiptPair) consumerPackage() string {
	return strings.SplitN(p.consumerName, ".", 2)[0]
}

var receiptPairs = []receiptPair{
	{
		name:     "dependency-publishability",
		producer: "catalog-dependency-publishability.sh",
		// Keys of the jq object literal that becomes the receipt: `name: $var`
		// at the start of a line inside the construction.
		writes:       regexp.MustCompile(`(?m)^\s{4,}([a-z_0-9]+):\s`),
		consumer:     releasequalification.DependencyPublishabilityReceipt{},
		consumerName: "releasequalification.DependencyPublishabilityReceipt",
	},
	{
		name:     "controller-differential plan",
		producer: "catalog-controller-differential.sh",
		// Assignment form, `.key = value`, which is how the diagnostic mode
		// adds its two keys to an already-written plan.
		writes:       regexp.MustCompile(`(?m)^\s*\.([a-z_0-9]+)\s*=\s`),
		consumer:     catalogparity.ControllerPlanReceipt{},
		consumerName: "catalogparity.ControllerPlanReceipt",
	},
}

// keysWrittenBy pulls the JSON keys a producer writes, from write positions
// only -- object-literal keys and assignments.
//
// NOT EVERY KEY MENTIONED. A jq program also READS keys, from its own inputs,
// and those belong to other types entirely; counting them would report the
// producer as writing fields it only ever looked at. The narrow patterns are
// per-pair for the same reason: one script builds its receipt as a literal and
// the other patches an existing file, and a pattern loose enough for both would
// be loose enough to catch reads.
func keysWrittenBy(body string, writes *regexp.Regexp) []string {
	seen := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range writes.FindAllStringSubmatch(line, -1) {
			seen[match[1]] = true
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// jsonFieldNames reads the consumer's accepted keys off the type itself, so the
// comparison cannot go stale against a struct that changed. Embedded structs
// are walked, because a promoted field is accepted by the decoder exactly as a
// declared one is.
func jsonFieldNames(value any) map[string]bool {
	names := map[string]bool{}
	var walk func(reflect.Type)
	walk = func(t reflect.Type) {
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			tag := field.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, _, _ := strings.Cut(tag, ",")
			if name == "" {
				if field.Anonymous {
					walk(field.Type)
					continue
				}
				name = field.Name
			}
			names[name] = true
		}
	}
	walk(reflect.TypeOf(value))
	return names
}

// TestEveryStrictConsumerIsPaired is the other half, and without it the table
// above is a list of the pairs somebody remembered.
//
// It finds every DisallowUnknownFields call in the tree -- each one is a place
// a receipt gets refused for having a field -- and requires the file to be
// either represented in receiptPairs or written down as unpaired with a reason.
// A strict consumer nobody has paired is a receipt nobody has checked, which is
// the state both instances were found in.
func TestEveryStrictConsumerIsPaired(t *testing.T) {
	strict := strictConsumerFiles(t)
	if len(strict) < 3 {
		t.Fatalf("found %d strict decoder(s); the walk is not reaching the tree", len(strict))
	}

	paired := map[string]bool{}
	for _, pair := range receiptPairs {
		paired[pair.consumerPackage()] = true
	}

	var unpaired []string
	for _, file := range strict {
		if paired[file] {
			continue
		}
		if _, declared := unpairedStrictConsumers[file]; declared {
			continue
		}
		unpaired = append(unpaired, file)
	}
	sort.Strings(unpaired)

	if len(unpaired) > 0 {
		t.Errorf("%d file(s) decode strictly and are paired with no producer:\n    %s\n\n"+
			"    Each is a place a receipt is refused for carrying a field. Add the pair to\n"+
			"    receiptPairs so the producer's keys are checked against it, or record it in\n"+
			"    unpairedStrictConsumers with the reason -- so the next reader sees which\n"+
			"    consumers are unverified rather than assuming the list is complete.",
			len(unpaired), strings.Join(unpaired, "\n    "))
	}
	t.Logf("%d strict consumer(s); %d paired, %d recorded unpaired",
		len(strict), len(receiptPairs), len(unpairedStrictConsumers))
}

// unpairedStrictConsumers is a LEDGER of strict decoders with no producer pair
// yet, and every entry is a receipt whose producer has not been established.
// knownReceiptMismatches records producer/consumer disagreements that are real,
// open, and not this branch's to fix.
//
// An entry is not permission. It is the difference between a defect somebody
// decided to carry and a defect nobody has noticed, and the exact key set is
// recorded so the entry cannot quietly come to cover a second one.
var knownReceiptMismatches = map[string]struct{ keys, why string }{
	"dependency-publishability": {
		keys: "tree",
		why: "#158. The script writes `tree`; every other receipt in the tree calls the same " +
			"thing `tree_state`, and the odd name is the tell that the field was added without " +
			"anyone opening the type. Measured live on BOTH main and go/catalog-build-schema " +
			"even though the task reads completed -- proven and recorded is not the same as " +
			"fixed. The consumer, catalog-release-ready, is wired to no workflow, which is the " +
			"only reason this has cost nothing yet. Whether to add the field or wire the " +
			"consumer is the same open question as whether that binary should exist.",
	},
	"controller-differential plan": {
		keys: "catalog_test_count,diagnostic_selection",
		why: "#159. Added to the plan only when CATALOG_ACCEPTANCE_TEST_NAMES is set -- the mode " +
			"an operator reaches for when something is already broken -- and catalog-admission " +
			"refuses the plan one step later in the same workflow. ALREADY FIXED on " +
			"go/catalog-build-schema, where this pair passes; the entry exists because main " +
			"still carries it. It will report stale the moment that branch merges, which is the " +
			"signal to delete it rather than a failure.",
	},
}

// unpairedStrictConsumers is a LEDGER of strict decoders with no producer pair.
//
// Every entry here is a receipt whose fit has never been checked. They are not
// paired yet because their producers are mid-rewrite in the other two lanes:
// pairing against a script that is being replaced this week would pin the shape
// of the thing being deleted. They pair as each producer lands.
var unpairedStrictConsumers = map[string]string{
	"catalog-admission":            "producer catalog-controller-differential.sh is being ported by sweep",
	"catalog-evidence":             "producer folded into the binary by sweep; artifact is committed and byte-compared",
	"catalog-hardware-disposition": "reads the controller receipt, which sweep is porting",
	"catalog-management-contract":  "reads the admission receipt, which sweep is porting",
	"catalog-migration-recovery":   "reads four receipts, three of them mid-port",
	"catalog-pragmatic-evidence":   "reads the controller receipt, which sweep is porting",
	"catalog-release-ready":        "wired to no workflow; its disposal is an open question, see #158",
	"providercompiler":             "reads provider-codegen policy, not a pipeline receipt -- different family",
}

// strictConsumerFiles reports the PACKAGE of every strict decoder, not the file.
// A package is the granularity a producer pairs with: several files in one
// package decode parts of the same receipt, and pairing each separately would
// ask the same question repeatedly and call it coverage.
func strictConsumerFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	seen := map[string]bool{}
	for _, root := range []string{"internal", "cmd"} {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(string(body), "DisallowUnknownFields") {
				key := filepath.Base(filepath.Dir(path))
				if !seen[key] {
					seen[key] = true
					files = append(files, key)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(files)
	return files
}
