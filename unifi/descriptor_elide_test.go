package unifi

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	resource_client_qos_rate "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_client_qos_rate"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	resource_firewall_group "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_group"
	resource_firewall_zone "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_firewall_zone"
	resource_static_route "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_static_route"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// Every descriptor's Elide values must agree with its generated schema.
//
// This exists because the value was unverified: flipping all seven of
// dns_record's left the whole package green, so a descriptor generated across
// every surface could carry the wrong value everywhere and nothing would say
// so. resourcekit's own tests prove the check goes red on that mutation
// against a probe; the cases here apply it to the descriptors we ship.
func TestEveryDescriptorElideAgreesWithItsSchema(t *testing.T) {
	ctx := context.Background()

	checks := map[string]func(*testing.T){
		"firewall_zone": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				firewallZoneKitSpec(), resource_firewall_zone.FirewallZoneResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"firewall_group": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				firewallGroupKitSpec(), resource_firewall_group.FirewallGroupResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"client_qos_rate": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				clientQosRateKitSpec(), resource_client_qos_rate.ClientQosRateResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"static_route": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				staticRouteKitSpec(), resource_static_route.StaticRouteResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
		"dns_record": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				dnsRecordKitSpec(), resource_dns_record.DnsRecordResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
	}

	// THE TABLE MUST COVER EVERY KIT SURFACE, and refusing an empty one was not
	// enough. A table missing a single entry passes: the surfaces it does name
	// still check out, the count is still non-zero, and the one nobody added is
	// simply absent -- which reads exactly like a surface with no problems.
	//
	// That is the same shape this check was built to remove, one level up. The
	// Elide values went unverified because nothing asserted them; the table
	// asserting them went incomplete because nothing asserted IT. So the set is
	// compared against the surfaces the provider actually serves from the kit,
	// derived by the same AST walk the policy check uses rather than from a
	// second hand-kept list, which would just move the problem.
	// kitServedSurfaces is keyed by Terraform type name, which is what the
	// table's own keys are once the provider prefix comes off.
	want := map[string]bool{}
	for typeName := range kitServedSurfaces(t) {
		want[strings.TrimPrefix(typeName, "unifi_")] = true
	}

	var missing, extra []string
	for name := range want {
		if _, ok := checks[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range checks {
		if !want[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("%v are served from the kit and no case checks their Elide values; "+
			"the descriptor ships unverified in exactly the dimension this test exists for", missing)
	}
	if len(extra) > 0 {
		t.Errorf("%v are checked here but no longer served from the kit; "+
			"a case for a surface that is gone reads as coverage and is none", extra)
	}
	if len(checks) == 0 {
		t.Fatal("no descriptors are being checked; this test cannot fail and is worthless")
	}
	for name, check := range checks {
		t.Run(name, check)
	}
}

// TestTheShippedDescriptorMutatesToExactlyItsFieldCount is the end-to-end
// control, and it asserts an EXACT number for a reason.
//
// The first version of this control ran against the descriptor BEFORE
// record_type was corrected. Flipping all seven yielded six problems, because
// flipping the one already-wrong value made it right -- so the defect under
// test was cancelling one of the control's own detections. Six had a
// plausible explanation ready (enabled is a BoolField and carries no Elide),
// and that explanation was wrong: enabled was never among the seven. A count
// that arrives with a reason attached does not get counted again.
//
// Asserting the exact field count is what makes that impossible to repeat: if
// a future edit leaves one field correct under mutation, this fails rather
// than quietly reporting one fewer.
func TestTheShippedDescriptorMutatesToExactlyItsFieldCount(t *testing.T) {
	ctx := context.Background()
	spec := dnsRecordKitSpec()

	flipped := spec
	flipped.Fields = nil
	elidable := 0
	for _, field := range spec.Fields {
		value := reflect.ValueOf(field)
		if !value.FieldByName("Elide").IsValid() {
			flipped.Fields = append(flipped.Fields, field) // BoolField: nothing to flip
			continue
		}
		elidable++
		mutated := reflect.New(value.Type()).Elem()
		mutated.Set(value)
		elide := mutated.FieldByName("Elide")
		elide.SetBool(!elide.Bool())
		flipped.Fields = append(flipped.Fields,
			mutated.Interface().(resourcekit.Field[dnsRecordKitModel, ui.DNSRecord]))
	}

	if elidable == 0 {
		t.Fatal("no field carries an Elide, so this control cannot fail")
	}
	problems := resourcekit.ElideProblems(flipped, resource_dns_record.DnsRecordResourceSchema(ctx))
	if len(problems) != elidable {
		t.Fatalf("flipping every Elide produced %d problem(s) for %d elidable field(s); "+
			"a shortfall means a value was already wrong and the mutation corrected it: %v",
			len(problems), elidable, problems)
	}
}
