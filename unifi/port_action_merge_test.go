package unifi

import (
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// mergePortOverride is candidate-only. It lives apart from the shared port
// action scenario file because that file is also copied into a separate
// build compiled against the released provider for comparison, and must stay
// compilable there too.
func Test_mergePortOverride_replacesMatchingPortWithoutDuplicates(t *testing.T) {
	port := int64(1)
	otherPort := int64(2)
	overrides := []ui.DevicePortOverrides{
		{PortIDX: &port, PoeMode: "auto"},
		{PortIDX: &otherPort, PoeMode: "off"},
	}

	got := mergePortOverride(overrides, 1, "pasv24")

	if len(got) != 2 {
		t.Fatalf("override count = %d, want 2", len(got))
	}
	if got[0].PortIDX == nil || *got[0].PortIDX != 1 || got[0].PoeMode != "pasv24" {
		t.Fatalf("target override = %#v, want port 1 with pasv24", got[0])
	}
	if got[1].PortIDX == nil || *got[1].PortIDX != 2 || got[1].PoeMode != "off" {
		t.Fatalf("unrelated override = %#v, want port 2 unchanged", got[1])
	}
}
