package main

import (
	"go/ast"
	"go/parser"
	"go/token"
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

	// THE OTHER DIRECTION, WHICH THIS LEDGER WAS MISSING WHILE ITS SIBLINGS
	// ENFORCED IT. unguardedGenerators reports "(no such script)";
	// knownUnreachableChecks reports "something now invokes them". This one
	// could only ever grow, so an excuse for a consumer that has since been
	// paired, or stopped decoding strictly, or ceased to exist, would sit here
	// reading as live.
	//
	// I predicted it would fire immediately and it did not, which was worth more
	// than being right. The population held eleven, the ledger ten, and two were
	// paired -- so eleven had to be short by one, and I inferred a stale entry.
	// Printing the three sets instead showed the real shape: one PAIRED package,
	// releasequalification, is not in the population at all, because the type it
	// owns is decoded from cmd/catalog-release-ready. Pairs are keyed on the
	// package owning the TYPE and the population on the package making the CALL.
	//
	// So catalog-release-ready is recorded as unpaired while the decode it
	// performs is in fact paired -- the ledger overstates what is unverified.
	// Over-listing rather than under-listing, which is the safe direction and
	// the one deliberately chosen when this walk was written, but the entry is
	// misleading and this is the concrete instance rather than the general
	// worry noted beside strictConsumerFiles.
	//
	// It is also what makes narrowing the population safe. Any correction that
	// removes packages -- teaching the scan that a comment is not a call was one
	// -- turns their entries into fiction, and without this the fix would
	// quietly create the defect it was fixing.
	inPopulation := map[string]bool{}
	for _, file := range strict {
		inPopulation[file] = true
	}
	var resolved []string
	for file := range unpairedStrictConsumers {
		switch {
		case !inPopulation[file]:
			resolved = append(resolved, file+" (no longer decodes strictly, or is gone)")
		case paired[file]:
			resolved = append(resolved, file+" (now paired in receiptPairs)")
		}
	}
	sort.Strings(resolved)
	if len(resolved) > 0 {
		t.Errorf("%d recorded unpaired consumer(s) no longer describe anything:\n    %s\n\n"+
			"    Remove them. An entry that has stopped being true overstates how much is\n"+
			"    unverified, and the next reader cannot tell a live gap from a resolved one.",
			len(resolved), strings.Join(resolved, "\n    "))
	}

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
	// EMPTY, AND THAT IS THE CURRENT STATE RATHER THAN AN UNUSED FEATURE.
	//
	// #158's entry went first: this check reported it stale the moment the fix
	// landed -- `recorded: tree, found: (nothing)`. #159's went the same way
	// when go/catalog-build-schema merged, exactly as its own reason predicted
	// it would. Both times the ledger asked to be deleted rather than sitting
	// as a live-looking excuse, which is the behaviour that makes it worth
	// keeping when the next mismatch arrives.
}

// unpairedStrictConsumers is a LEDGER of strict decoders with no producer pair.
//
// Every entry here is a receipt whose fit has never been checked. They are not
// paired yet because their producers are mid-rewrite in the other two lanes:
// pairing against a script that is being replaced this week would pin the shape
// of the thing being deleted. They pair as each producer lands.
var unpairedStrictConsumers = map[string]string{
	// This check named its own executor, which is correct and is kept rather
	// than special-cased away. cmd/receipt-gate decodes strictly because
	// decoding IS its first assertion, and the types it decodes into are exactly
	// the ones receiptPairs already covers -- so every pair above checks it, and
	// an entry of its own would count the same question twice.
	//
	// It does expose a real imprecision. The walk keys on the package containing
	// the DisallowUnknownFields CALL, while a pair is about the package owning
	// the TYPE, and those differ whenever a command decodes a type declared
	// elsewhere -- which is most of them. Recorded rather than fixed: keying on
	// the type means resolving it, and a wrong answer there would silently drop
	// consumers from the population instead of listing them here, which is the
	// worse failure of the two.
	"receipt-gate": "the command that executes the pairs in receiptPairs; its decoding is " +
		"checked by every entry there rather than by one of its own",

	// Arrived with sweep's merge and was named by this check on the first run
	// after it: a new strict decoder landing with nothing paired to it, which is
	// the state both original instances were found in.
	"catalog-migration-verify": "reads the migration and recovery receipt, whose producer is " +
		"catalog-migration-recovery -- a Go binary, so the two agree by sharing a type rather " +
		"than by a key set someone has to keep in step. Pairing Go to Go is a different check " +
		"from this one, which exists because a shell producer and a Go consumer have nothing " +
		"between them at all.",
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
			// PARSED, NOT GREPPED, and that is a correction rather than a
			// refinement. strings.Contains counted the identifier wherever it
			// appeared, INCLUDING IN COMMENTS -- cmd/schema-parity was reported
			// as a strict consumer on the strength of a sentence explaining why
			// it must not add a field to a receipt that eighty consumers decode
			// strictly. It decodes nothing.
			//
			// The cost of that is not the false name in a list. It is that the
			// prescribed remedy for a false positive is a ledger entry, and an
			// entry here is meant to be the difference between a defect somebody
			// decided to carry and a defect nobody has noticed. Recording one
			// for a file that decodes nothing puts a defect in the ledger that
			// does not exist, and the count in the floor above goes up with it.
			if callsDisallowUnknownFields(path) {
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

// callsDisallowUnknownFields reports whether a file CALLS the method, as opposed
// to mentioning its name.
//
// A parse failure returns false rather than erroring: this walk runs over the
// whole tree, and a file that does not parse is a compile failure the build
// reports far more clearly than a receipt-pairing test could.
func callsDisallowUnknownFields(path string) bool {
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel != nil && selector.Sel.Name == "DisallowUnknownFields" {
			found = true
			return false
		}
		return !found
	})
	return found
}
