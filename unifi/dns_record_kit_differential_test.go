package unifi

// The differential that decides whether the generated shape may replace the
// hand-written one.
//
// SAME METHOD AS THE SHELL PORT, FOR THE SAME REASON. The two implementations
// exist side by side and are handed identical inputs; the new one is not
// trusted because it reads correct, it is trusted because it agrees. The
// schema-parity gate covers the SCHEMA and cannot see any of this -- these four
// functions are where a resource decides what to send, what to keep, and what
// to null, and none of that appears in a schema.
//
// DELETE THIS FILE IN THE COMMIT THAT DELETES THE HAND-WRITTEN RESOURCE. It
// compares two implementations and means nothing with one.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// cases covers the states the two paths can disagree about, not a sample of
// plausible ones: every attribute null, every attribute set, and the four the
// mapping calls optional at their SDK zero, which is the case where one
// implementation writes a value and the other writes a null.
func dnsRecordKitCases() map[string]dnsRecordFrameworkResourceModel {
	full := dnsRecordFrameworkResourceModel{
		ID:         types.StringValue("abc123"),
		Site:       types.StringValue("default"),
		Name:       types.StringValue("host.example"),
		Enabled:    types.BoolValue(true),
		Port:       types.Int64Value(8080),
		Priority:   types.Int64Value(10),
		RecordType: types.StringValue("A"),
		TTL:        timetypes.NewGoDurationValue(300 * time.Second),
		Value:      types.StringValue("10.0.0.1"),
		Weight:     types.Int64Value(5),
	}
	sparse := dnsRecordFrameworkResourceModel{
		ID:         types.StringValue("abc123"),
		Site:       types.StringValue("default"),
		Name:       types.StringValue("host.example"),
		Value:      types.StringValue("10.0.0.1"),
		Enabled:    types.BoolNull(),
		Port:       types.Int64Null(),
		Priority:   types.Int64Null(),
		RecordType: types.StringNull(),
		TTL:        timetypes.NewGoDurationNull(),
		Weight:     types.Int64Null(),
	}
	unknown := full
	unknown.Port = types.Int64Unknown()
	unknown.Weight = types.Int64Unknown()
	unknown.RecordType = types.StringUnknown()

	zeroed := full
	zeroed.Port = types.Int64Value(0)
	zeroed.Priority = types.Int64Value(0)
	zeroed.Weight = types.Int64Value(0)
	zeroed.TTL = timetypes.NewGoDurationValue(0)

	return map[string]dnsRecordFrameworkResourceModel{
		"every attribute set":     full,
		"only the required ones":  sparse,
		"three unknown":           unknown,
		"optionals at their zero": zeroed,
	}
}

// comparable drops Timeouts, which holds an object value that Go cannot compare
// and which none of the four functions under test touches. Comparing the whole
// struct would need reflect.DeepEqual over a type whose equality is not defined;
// dropping the one field nobody writes keeps == and keeps the comparison exact
// over everything that IS written.
type comparableDNSRecord struct {
	ID, Site, Name, RecordType, Value types.String
	Enabled                           types.Bool
	Port, Priority, Weight            types.Int64
	TTL                               timetypes.GoDuration
}

func comparableOf(m dnsRecordKitModel) comparableDNSRecord {
	return comparableDNSRecord{
		ID: m.ID, Site: m.Site, Name: m.Name, RecordType: m.RecordType, Value: m.Value,
		Enabled: m.Enabled, Port: m.Port, Priority: m.Priority, Weight: m.Weight, TTL: m.TTL,
	}
}

func toKitModel(m dnsRecordFrameworkResourceModel) dnsRecordKitModel {
	return dnsRecordKitModel{
		ID: m.ID, Site: m.Site, Name: m.Name, Enabled: m.Enabled, Port: m.Port,
		Priority: m.Priority, RecordType: m.RecordType, TTL: m.TTL,
		Value: m.Value, Weight: m.Weight, Timeouts: m.Timeouts,
	}
}

// TestKitSendsWhatTheHandWrittenPathSends compares the object that reaches the
// controller. It is the half a wrong mapping shows up in first: a field read
// from the wrong place is a value written to the wrong attribute.
func TestKitSendsWhatTheHandWrittenPathSends(t *testing.T) {
	hand := &dnsRecordFrameworkResource{}
	spec := dnsRecordKitSpec()
	for name, model := range dnsRecordKitCases() {
		t.Run(name, func(t *testing.T) {
			byHand := dnsRecordFromIntent(hand.modelToDNSRecordIntent(context.Background(), &model))
			kitModel := toKitModel(model)
			byKit := spec.ToSDK(&kitModel)
			// Port is a *int64, so the structs compare by pointer identity and
			// would differ on every case. Compare what they point at.
			if !samePort(byHand.Port, byKit.Port) {
				t.Fatalf("port differs: hand %s, kit %s", showPort(byHand.Port), showPort(byKit.Port))
			}
			handFlat, kitFlat := *byHand, *byKit
			handFlat.Port, kitFlat.Port = nil, nil
			if handFlat != kitFlat {
				t.Fatalf("the two paths send different objects:\n  hand %+v\n  kit  %+v", handFlat, kitFlat)
			}
		})
	}
}

// TestKitReadsWhatTheHandWrittenPathReads compares the state written back. This
// is where the elision rules live, and getting one wrong produces a permanent
// diff rather than an error.
func TestKitReadsWhatTheHandWrittenPathReads(t *testing.T) {
	hand := &dnsRecordFrameworkResource{}
	spec := dnsRecordKitSpec()
	port := int64(8080)
	zero := int64(0)
	for name, record := range map[string]*ui.DNSRecord{
		"every field populated": {
			ID: "abc123", Enabled: true, Key: "host.example", Port: &port,
			Priority: 10, RecordType: "A", Ttl: 300, Value: "10.0.0.1", Weight: 5,
		},
		"optionals absent": {
			ID: "abc123", Enabled: false, Key: "host.example", Value: "10.0.0.1",
		},
		"a port pointer at zero": {
			ID: "abc123", Key: "host.example", Value: "10.0.0.1", Port: &zero,
		},
	} {
		t.Run(name, func(t *testing.T) {
			var handModel dnsRecordFrameworkResourceModel
			hand.dnsRecordToModel(context.Background(), dnsRecordFromAPI(record), &handModel, "default")

			var kitModel dnsRecordKitModel
			spec.ToModel(record, &kitModel, "default")

			if got, want := comparableOf(toKitModel(handModel)), comparableOf(kitModel); got != want {
				t.Fatalf("the two paths write different state:\n  hand %+v\n  kit  %+v", got, want)
			}
		})
	}
}

// TestKitMasksWhatTheHandWrittenPathMasks compares the update's field list. A
// mask that is too wide overwrites controller-owned attributes; one that is too
// narrow silently drops a change the practitioner made.
func TestKitMasksWhatTheHandWrittenPathMasks(t *testing.T) {
	hand := &dnsRecordFrameworkResource{}
	spec := dnsRecordKitSpec()
	for name, model := range dnsRecordKitCases() {
		t.Run(name, func(t *testing.T) {
			state := model
			patch := hand.modelToDNSRecordPatch(context.Background(), &model, &state)
			byHand, err := patch.wireFields()
			if err != nil {
				t.Fatalf("the hand-written path refused: %v", err)
			}
			kitModel := toKitModel(model)
			byKit, err := spec.WireFields(&kitModel)
			if err != nil {
				t.Fatalf("the generated path refused: %v", err)
			}
			if len(byHand) != len(byKit) {
				t.Fatalf("masks differ:\n  hand %v\n  kit  %v", byHand, byKit)
			}
			for i := range byHand {
				if byHand[i] != byKit[i] {
					t.Fatalf("masks differ at %d:\n  hand %v\n  kit  %v", i, byHand, byKit)
				}
			}
		})
	}
}

// TestKitMergesWhatTheHandWrittenPathMerges compares the plan-over-state merge,
// which is what preserves a computed value the controller assigned.
func TestKitMergesWhatTheHandWrittenPathMerges(t *testing.T) {
	hand := &dnsRecordFrameworkResource{}
	spec := dnsRecordKitSpec()
	cases := dnsRecordKitCases()
	for planName, plan := range cases {
		for stateName, state := range cases {
			t.Run(planName+" over "+stateName, func(t *testing.T) {
				handState := state
				hand.applyPlanToState(context.Background(), &plan, &handState)

				kitPlan, kitState := toKitModel(plan), toKitModel(state)
				spec.ApplyPlanToState(&kitPlan, &kitState)

				if got := comparableOf(toKitModel(handState)); got != comparableOf(kitState) {
					t.Fatalf("the two merges differ:\n  hand %+v\n  kit  %+v", got, comparableOf(kitState))
				}
			})
		}
	}
}

func samePort(a, b *int64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func showPort(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprintf("&%d", *p)
}
