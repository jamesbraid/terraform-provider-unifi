package unifi

import (
	"context"
	"strings"
	"testing"
)

// knownOmitZeroGaps pins every OmitZeroProblems hit whose fix is not adding
// OmitZero to the field, and why -- so the census stays green without going
// silent about what it still finds. Each entry must still be produced by the
// walk below, or the gap was closed and the pin is stale; remove it in the
// same commit that fixes it.
//
// The top-level entries are hits where OmitZero is wrong (a Required field) or
// redundant (a plan-time validator already rejects 0, so no zero reaches the
// write). The nested entries are a different shape: OmitZeroProblems now
// descends into ObjectField / ObjectListField element types, but a nested
// member is filled in by the descriptor's Encode closure, not an Int64PtrField,
// so there is no OmitZero flag for the structural walk to read -- it reflects
// the SDK struct and reports every zero-rejecting pointer-integer member. Where
// the member can be Unknown on write (Optional+Computed), the fix lives in that
// closure -- radiusServerParts drops zero/unknown via util.OmitZeroInt64Pointer,
// and the device radio_table's sanitizeRadioForUpdate drops an out-of-range or
// unset-sentinel value -- and the pin records that the structural walk still
// sees it. Where the member cannot be Unknown (Optional-only, plan-guarded), the
// pin is redundant, the dns_record.port case one level down.
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
		`value to the six legal index ranges, none of which include 0.`,
	"static_route.static-route_distance": `Required (the Terraform attribute is named "distance"); ` +
		`same reasoning as firewall_rule.rule_index above.`,

	// Nested hits from the descent into ObjectListField element types.
	"radius_profile.acct_servers.port": `Optional+Computed, so an omitted port is Unknown on ` +
		`create and the Encode closure would send it as 0. The pattern's trailing |^$ arm accepts ` +
		`an empty string, but a numeric field never sends "", so omitting is the only "unset" -- ` +
		`radiusServerParts now uses util.OmitZeroInt64Pointer to drop zero/unknown, closing the ` +
		`hazard. This structural census still reports it because it reflects ` +
		`RADIUSProfileAcctServers, not the Encode closure that carries the fix.`,
	"radius_profile.auth_servers.port": `same as radius_profile.acct_servers.port -- both server ` +
		`blocks share radiusServerParts, so the fix and the reason are identical; pinned because the ` +
		`structural walk cannot see the closure.`,
	"device.radio_table.ht": `Optional+Computed with a OneOf(20,40,...) validator that rejects 0, ` +
		`but Computed means an omitted value is Unknown on write and a validator does not run on ` +
		`Unknown, so ValueInt64Pointer sends 0. sanitizeRadioForUpdate (run inside the radio_table ` +
		`Encode) now drops an ht that is not one of the controller's channel widths -- the ` +
		`unknown-sentinel 0 among them -- so no 0 reaches the wire (see TestSanitizeRadioForUpdate). ` +
		`The structural walk still reports it because it reflects DeviceRadioTable, not the closure.`,
	"device.radio_table.maxsta": `already handled: sanitizeRadioForUpdate nils maxsta unless it is ` +
		`in [1,200], so the unknown-sentinel 0 is dropped before the wire (see ` +
		`TestSanitizeRadioForUpdate). Pinned because this structural walk reflects DeviceRadioTable, ` +
		`not the Encode closure that sanitizes it.`,
	"device.radio_table.min_rssi": `already handled: sanitizeRadioForUpdate nils min_rssi unless ` +
		`enabled and in [-90,-67], so the unknown-sentinel 0 is dropped before the wire (see ` +
		`TestSanitizeRadioForUpdate). Pinned as for device.radio_table.maxsta.`,
	"device.radio_table.sens_level": `already handled: sanitizeRadioForUpdate nils sens_level unless ` +
		`enabled and in [-90,-50], so the unknown-sentinel 0 is dropped before the wire (see ` +
		`TestSanitizeRadioForUpdate). Pinned as for device.radio_table.maxsta.`,
	"nat.destination_filter.port": `redundant, the dns_record.port case one level down: the nested ` +
		`port is Optional-only (not Computed), so an omitted one is null -- never Unknown -- and its ` +
		`ValueInt64Pointer encode drops it; an explicit 0 fails the schema's ` +
		`int64validator.Between(1,99999) at plan time, before Encode runs. No zero can reach the ` +
		`wire, so omitting zero/unknown would change nothing.`,
	"nat.source_filter.port": `same as nat.destination_filter.port: Optional-only and plan-guarded ` +
		`by Between(1,99999), so no zero reaches the wire.`,
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
