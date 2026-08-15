package catalogparity

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

// TestCampaignPolicyMatchesCommittedInventory recomputes the campaign shape
// from the committed evidence inventory using the same rules as the jq plan
// builder in .woodpecker/scripts/catalog-controller-differential.sh.
//
// Without this, the policy is only checked against a plan that the tests
// themselves synthesised from that same policy, which is true by construction.
// A count that has drifted away from the real catalog would then survive every
// local check and only surface an hour into a controller campaign.
func TestCampaignPolicyMatchesCommittedInventory(t *testing.T) {
	policy := testCampaignPolicy(t)

	data, err := os.ReadFile("../../build/release-ready/catalog-evidence-inventory.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory EvidenceInventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		t.Fatal(err)
	}

	excepted := make(map[SurfaceKey]bool, len(policy.SharedScenarioExceptions))
	for _, exception := range policy.SharedScenarioExceptions {
		excepted[exception.SurfaceKey] = true
	}

	waves := []int{1, 2, 3, 4, 5}
	surfaceCount := 0
	evidenceGapCount := 0
	testNames := make([]string, 0)
	sharedScenarioOwners := make([]string, 0)
	runtimeChanged := make([]SurfaceKey, 0)
	for _, surface := range inventory.Surfaces {
		if !slices.Contains(waves, surface.Wave) {
			continue
		}
		if surface.Runtime.Status == FileChanged {
			runtimeChanged = append(runtimeChanged, surface.SurfaceKey)
		}
		surfaceCount++
		evidenceGapCount += len(surface.MissingSignals)
		for _, name := range surface.TestFunctions {
			if !strings.HasPrefix(name, "TestAcc") {
				continue
			}
			isList := strings.Contains(name, "List")
			switch surface.Kind {
			case ManagedResource:
				if isList {
					continue
				}
			case ListResource:
				if !isList {
					continue
				}
			}
			if !slices.Contains(testNames, name) {
				testNames = append(testNames, name)
			}
		}
		// A scenario is shared when the runtime is byte-identical, or when the
		// policy declares an exception for the surface. The exception used to
		// be hardcoded here as "the port action"; it now comes from the policy,
		// so this test reads the same declaration admission does.
		shared := surface.Runtime.Status == "identical" || excepted[surface.SurfaceKey]
		if shared && !slices.Contains(sharedScenarioOwners, surface.ScenarioOwner) {
			sharedScenarioOwners = append(sharedScenarioOwners, surface.ScenarioOwner)
		}
	}

	// A declared exception for a surface whose runtime is identical is doing
	// nothing, because the runtime rule already shares it. Catching a redundant
	// exception here keeps the declaration a decision rather than decoration
	// nobody rereads.
	for _, exception := range policy.SharedScenarioExceptions {
		surface := inventory.Surface(exception.SurfaceKey)
		if surface == nil {
			t.Errorf("campaign policy excepts %s/%s, which the catalog does not contain",
				exception.Kind, exception.Name)
			continue
		}
		if surface.Runtime.Status == FileIdentical {
			t.Errorf("campaign policy excepts %s/%s, whose runtime is identical, so the exception is redundant",
				exception.Kind, exception.Name)
		}
	}

	for _, check := range []struct {
		field string
		got   int
		want  int
	}{
		{"surface_count", surfaceCount, policy.SurfaceCount},
		{"evidence_gap_count", evidenceGapCount, policy.EvidenceGapCount},
		{"test_name_count", len(testNames), policy.TestNameCount},
	} {
		if check.got != check.want {
			t.Errorf("inventory yields %s = %d, policy says %d", check.field, check.got, check.want)
		}
	}

	// Migration recovery compares this exact set, sorted by kind then name, an
	// hour into a campaign. Catching a stale entry here costs a second.
	sort.Slice(runtimeChanged, func(a, b int) bool {
		if runtimeChanged[a].Kind != runtimeChanged[b].Kind {
			return runtimeChanged[a].Kind < runtimeChanged[b].Kind
		}
		return runtimeChanged[a].Name < runtimeChanged[b].Name
	})
	if !reflect.DeepEqual(runtimeChanged, policy.RuntimeChangeKeys()) {
		t.Errorf("inventory runtime change set is %v, policy declares %v",
			runtimeChanged, policy.RuntimeChangeKeys())
	}

	// Every disposition has to name a test the campaign actually plans to run,
	// or the released suite can never satisfy it.
	for _, names := range [][]string{
		policy.AllowedSkips,
		policy.ReleasedAllowedFailures,
		policy.ReleasedAllowedMissing,
	} {
		for _, name := range names {
			if !slices.Contains(testNames, name) {
				t.Errorf("campaign policy names %q, which the catalog does not plan", name)
			}
		}
	}
}

// TestRuntimeChangeReasonsMatchTheGeneratedPackages checks every declared
// reason against the packages actually present under internal/generated.
//
// The list of surfaces is the easy half of the declaration and the one a tired
// human satisfies by pasting whatever the inventory measured. The reasons are
// the half that cannot be pasted, and this is what makes them cost something:
// claiming a surface was converted when it has no generated package fails here,
// in a second, rather than being believed forever.
//
// It deliberately does not decide between companion_conversion and hand_edit.
// Telling "the bytes moved because a companion was generated" apart from
// "someone changed this on purpose" is a judgement about intent, and intent is
// the reason this list is declared instead of derived.
func TestRuntimeChangeReasonsMatchTheGeneratedPackages(t *testing.T) {
	policy := testCampaignPolicy(t)
	if len(policy.RuntimeChangeSet) == 0 {
		t.Fatal("the policy declares no runtime changes, so every reason below would be vacuous")
	}
	for _, problem := range policy.RuntimeChangeReasonsMatchTree("../generated") {
		t.Error(problem)
	}
}

// TestRuntimeChangeReasonsRejectAContradictedClaim proves the check above can
// fail. A reason nothing refutes is decoration.
func TestRuntimeChangeReasonsRejectAContradictedClaim(t *testing.T) {
	for name, test := range map[string]struct {
		change RuntimeChange
		want   string
	}{
		"converted without a generated package": {
			change: RuntimeChange{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_device"},
				Reason:     ReasonConverted,
			},
			want: "no generated package resource_device",
		},
		"hand edit on a converted surface": {
			change: RuntimeChange{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_wlan"},
				Reason:     ReasonHandEdit,
			},
			want: "its own package resource_wlan exists",
		},
		"companion conversion on a converted surface": {
			change: RuntimeChange{
				SurfaceKey: SurfaceKey{Kind: ListResource, Name: "unifi_wlan"},
				Reason:     ReasonCompanionConversion,
			},
			want: "its own package listresource_wlan exists",
		},
		"companion conversion with no converted companion": {
			change: RuntimeChange{
				SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_setting"},
				Reason:     ReasonCompanionConversion,
			},
			want: "no companion package listresource_setting exists",
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := CampaignPolicy{RuntimeChangeSet: []RuntimeChange{test.change}}
			problems := policy.RuntimeChangeReasonsMatchTree("../generated")
			if len(problems) != 1 || !strings.Contains(problems[0].Error(), test.want) {
				t.Fatalf("problems = %v, want one containing %q", problems, test.want)
			}
		})
	}

	// The control: the reason the committed policy actually gives for that same
	// surface is accepted, so the cases above fail for their stated cause and
	// not because the checker rejects everything.
	policy := CampaignPolicy{RuntimeChangeSet: []RuntimeChange{{
		SurfaceKey: SurfaceKey{Kind: ManagedResource, Name: "unifi_device"},
		Reason:     ReasonHandEdit,
	}}}
	if problems := policy.RuntimeChangeReasonsMatchTree("../generated"); len(problems) != 0 {
		t.Fatalf("the committed reason for managed_resource/unifi_device was rejected: %v", problems)
	}
}
