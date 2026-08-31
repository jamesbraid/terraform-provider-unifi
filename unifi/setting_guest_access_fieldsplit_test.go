package unifi

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// guestAccessFieldSplitErrors partitions typ's named fields against secret
// and plain, and reports every violation: a field in neither set, or a
// field in both. BaseSetting -- the envelope every settings section embeds
// (_id, site_id, key, ...) -- is not a guest_access field and is skipped by
// its Anonymous bit, not by name, so it can't be silently miscounted as an
// unclassified field.
//
// Factored out of the test function so
// TestGuestAccessFieldSplitCatchesAnIncompleteSet can prove the check
// actually fails on a bad set, rather than trusting the assertion by
// inspection.
func guestAccessFieldSplitErrors(typ reflect.Type, secret, plain map[string]bool) []string {
	var errs []string
	seen := map[string]int{}
	for _, set := range []map[string]bool{secret, plain} {
		for name := range set {
			seen[name]++
		}
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous {
			continue
		}
		switch seen[f.Name] {
		case 1:
		case 0:
			errs = append(errs, fmt.Sprintf(
				"%s is in neither set: every field is modeled, so classify it secret or plain", f.Name))
		default:
			errs = append(errs, fmt.Sprintf("%s is in %d sets; it must be in exactly one", f.Name, seen[f.Name]))
		}
	}
	return errs
}

// TestGuestAccessFieldSplitCoversTheStruct is the partition contract every
// later task in this plan indexes into: guestAccessSecret and
// guestAccessPlain, together, must classify every field of
// settings.GuestAccess exactly once. A field in neither set would simply
// never reach the schema -- the exact failure this test exists to make
// loud instead of invisible.
func TestGuestAccessFieldSplitCoversTheStruct(t *testing.T) {
	typ := reflect.TypeOf(settings.GuestAccess{})

	for _, msg := range guestAccessFieldSplitErrors(typ, guestAccessSecret, guestAccessPlain) {
		t.Error(msg)
	}

	named := 0
	for i := 0; i < typ.NumField(); i++ {
		if !typ.Field(i).Anonymous {
			named++
		}
	}
	if named != 92 {
		t.Errorf("settings.GuestAccess has %d fields (excluding the embedded BaseSetting envelope), expected 92. "+
			"The SDK moved. STOP and reconcile guestAccessSecret/guestAccessPlain in "+
			"setting_guest_access_fieldsplit.go against the new struct -- do not adjust this number to make "+
			"the test pass.", named)
	}
}

// TestGuestAccessFieldSplitCatchesAnIncompleteSet proves
// guestAccessFieldSplitErrors would fail loudly if a field ever went
// unclassified, by feeding it a set with one field removed and checking it
// names exactly that field. This cycle has produced several tests that
// looked right and could not fail; this is the one that can.
func TestGuestAccessFieldSplitCatchesAnIncompleteSet(t *testing.T) {
	typ := reflect.TypeOf(settings.GuestAccess{})

	const dropped = "WechatShopID"
	if !guestAccessPlain[dropped] {
		t.Fatalf("test fixture assumes %s is in guestAccessPlain; it is not -- update the fixture", dropped)
	}
	incomplete := make(map[string]bool, len(guestAccessPlain)-1)
	for name, v := range guestAccessPlain {
		if name == dropped {
			continue
		}
		incomplete[name] = v
	}

	errs := guestAccessFieldSplitErrors(typ, guestAccessSecret, incomplete)
	if len(errs) != 1 {
		t.Fatalf("dropping one field should produce exactly one violation, got %d: %v", len(errs), errs)
	}
	want := dropped + " is in neither set: every field is modeled, so classify it secret or plain"
	if errs[0] != want {
		t.Errorf("wrong message for the dropped field:\ngot:  %s\nwant: %s", errs[0], want)
	}
}

// TestGuestAccessSecretSetIsExactlyTheXPrefixedFields pins the x_ prefix as
// this codebase's marker for a credential field (see
// setting_guest_access_fieldsplit.go's comment and Task 0's report): the
// secret set must have exactly 18 members, and membership in it must
// coincide exactly with carrying the SDK's x_ wire prefix -- a secret
// without the prefix, or an x_ field left out of the set, is a
// classification error.
func TestGuestAccessSecretSetIsExactlyTheXPrefixedFields(t *testing.T) {
	if len(guestAccessSecret) != 18 {
		t.Errorf("guestAccessSecret has %d members, expected 18", len(guestAccessSecret))
	}

	typ := reflect.TypeOf(settings.GuestAccess{})
	wireByName := map[string]string{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.Anonymous {
			continue
		}
		wire, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		wireByName[f.Name] = wire
	}

	for name := range guestAccessSecret {
		wire, ok := wireByName[name]
		if !ok {
			t.Errorf("guestAccessSecret names %s, which is not a field of settings.GuestAccess", name)
			continue
		}
		if !strings.HasPrefix(wire, "x_") {
			t.Errorf("%s (wire %q) is in guestAccessSecret but does not carry the x_ prefix", name, wire)
		}
	}

	for name, wire := range wireByName {
		if strings.HasPrefix(wire, "x_") && !guestAccessSecret[name] {
			t.Errorf("%s (wire %q) carries the x_ prefix but is not in guestAccessSecret", name, wire)
		}
	}
}
