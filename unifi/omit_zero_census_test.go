package unifi

import (
	"context"
	"strings"
	"testing"
)

// knownOmitZeroGaps pins every OmitZeroProblems hit R2-C Task 10b's fix
// round chose NOT to close with OmitZero, and why -- so the census stays
// green without going silent about what it still finds. Each entry must
// still be produced by the walk below, or the gap was closed and the pin is
// stale; remove it in the same commit that fixes it.
var knownOmitZeroGaps = map[string]string{
	"dns_record.port": `not this class's fix, even after R2-C Task 10c tightened the SCHEMA ` +
		`validator to int64validator.Between(1, 65535) to match the controller's own pattern ` +
		`([1-9][0-9]{0,4}) -- a config with port = 0 now fails at plan time, before ToSDK ever ` +
		`runs, so the practitioner-facing hazard this table exists to catch is closed. This census ` +
		`is structural, not schema-aware: it flags any Int64PtrField whose wire name resolves to a ` +
		`zero-rejecting controller pattern and carries no OmitZero, regardless of what the ` +
		`generated schema validator allows, so it still (correctly) reports this field. Adding ` +
		`OmitZero would be redundant, not wrong -- port is Optional-only (no Computed), so it is ` +
		`never Unknown on create, and an explicit 0 can no longer reach ToSDK at all now that the ` +
		`validator rejects it at plan time -- but doing so is out of Task 10c's scope, which asked ` +
		`only for the validator change.`,
	"firewall_rule.rule_index": `Required, not Optional -- a Required attribute is never ` +
		`legitimately unset, so OmitZero (which OMITS the field from the wire) is the wrong tool. ` +
		`The hazard cannot currently trigger anyway: the schema validator already restricts the ` +
		`value to the four legal index ranges, none of which include 0.`,
	"static_route.static-route_distance": `Required (the Terraform attribute is named "distance"); ` +
		`same reasoning as firewall_rule.rule_index above.`,
}

// TestEveryKitSurfaceOmitsAZeroTheControllerRejects is the R2-C Task 10b
// census: it walks every kit surface's Int64PtrFields, resolves each one's
// wire name against the SDK's own unifi.FieldConstraints table, and fails by
// name wherever the controller's pattern rejects a literal "0" but the field
// sets no OmitZero -- the shape behind wlan's dtim_6e/dtim_na/dtim_ng and
// roaming_assistant_6e_rssi/roaming_assistant_na_rssi (all fixed). The first
// run of this walk found twelve more hits across five other surfaces
// (radius_user, device, port_profile, wlan.vlan, client_qos_rate); eleven
// were fixed the same way and are listed in the commit body, and three are
// pinned above as gaps this class of fix does not apply to.
// resourcekit.OmitZeroProblems proves the mechanism against a synthetic
// probe; this applies it to what the provider actually serves, the same
// shape as TestEveryKitSurfaceSurvivesAZeroRead.
func TestEveryKitSurfaceOmitsAZeroTheControllerRejects(t *testing.T) {
	ctx := context.Background()
	type omitZeroable interface {
		OmitZeroProblems() []string
	}

	checked := 0
	seenGap := map[string]bool{}
	for _, constructor := range New().Resources(ctx) {
		surface, ok := constructor().(omitZeroable)
		if !ok {
			continue
		}
		checked++
		for _, problem := range surface.OmitZeroProblems() {
			matched := false
			for name, reason := range knownOmitZeroGaps {
				if strings.HasPrefix(problem, name+":") {
					seenGap[name] = true
					matched = true
					t.Logf("%s -- pinned known gap: %s", problem, reason)
					break
				}
			}
			if !matched {
				t.Error(problem)
			}
		}
	}
	for name := range knownOmitZeroGaps {
		if !seenGap[name] {
			t.Errorf("%s no longer reports as an omit-zero gap; if it was fixed, remove it "+
				"from knownOmitZeroGaps in the same commit", name)
		}
	}

	// +1: unifi_account is a deprecated alias embedding the radius_user kit
	// resource (see TestEveryKitSurfaceSurvivesAZeroRead), so it joins the
	// walk as a twenty-first resource over the same twenty surfaces. What
	// the pin holds is that nothing drops from the walk unannounced.
	if want := len(kitServedSurfaces(t)) + 1; checked != want {
		t.Errorf("omit-zero census walked %d resource(s) against %d kit-served surface(s) plus "+
			"the account alias; a surface outside the walk is one whose Int64PtrFields nobody "+
			"has checked against the controller's own constraints", checked, want)
	}
}
