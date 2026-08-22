package unifi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The recorded blockers must agree with the tree, in both directions.
//
// WHY THIS FILE EXISTS AT ALL. A readiness measure rebuilt from the artifacts
// enumerates its sources, finds one for every column except hand-won findings,
// and proceeds without them -- reading as MORE rigorous for having done so.
// That happened: a rebuild on all four artifacts reported dynamic_dns,
// port_profile and radius_user ready, and all three were blocked by facts
// already measured by hand. The findings had no file, so the rebuild could not
// find them. Now they have one.
//
// The check is deliberately about BOOKKEEPING rather than about re-deriving the
// blockers. Re-deriving them is what was wrong four times; what was missing is
// a record that a rebuilt instrument can be diffed against.

type surfaceBlockers struct {
	BlockerKinds []string            `json:"blocker_kinds"`
	Surfaces     map[string][]string `json:"surfaces"`
}

func loadSurfaceBlockers(t *testing.T) surfaceBlockers {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "provider-codegen", "policy", "surface-blockers.json"))
	if err != nil {
		t.Fatalf("read the blocker record: %v", err)
	}
	var record surfaceBlockers
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("parse the blocker record: %v", err)
	}
	if len(record.Surfaces) == 0 {
		t.Fatal("the record names no surfaces, so every assertion below passes vacuously")
	}
	if len(record.BlockerKinds) == 0 {
		t.Fatal("the record declares no blocker kinds")
	}
	return record
}

// resourceSurfaces lists every *_resource.go in this package, and whether it
// has been cut over to the kit.
func resourceSurfaces(t *testing.T) map[string]bool {
	t.Helper()
	files, err := filepath.Glob("*_resource.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		name := strings.TrimSuffix(path, "_resource.go")
		// CUT OVER MEANS SERVED FROM THE KIT, NOT "A DESCRIPTOR FILE EXISTS".
		//
		// This read os.Stat(name+"_descriptor.go") and took the file's presence
		// as the answer. A descriptor written but not yet wired -- the state a
		// migration is in for as long as it takes to write one -- flipped its
		// surface to cut over, which made TestNoRecordedBlockerOutlivesItsSurface
		// demand its blockers be deleted while the resource was still served by
		// hand-written CRUD. Deleting them would then satisfy
		// TestEveryUncutSurfaceHasARecordedBlocker too, because that one asks
		// the same detector. Both checks would be green about a surface nobody
		// had migrated.
		//
		// The wiring is what the record is about, so the wiring is what is read:
		// a kit resource embeds resourcekit.Resource[Model, SDK] and a
		// hand-written one does not. Measured across the tree, the two detectors
		// agree on all twelve migrated surfaces and disagree only where a
		// descriptor exists unwired -- which is the case that matters.
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// THE SURFACE NAME COMES FROM WHAT THE FILE SERVES, NOT FROM ITS NAME.
		//
		// device is the first surface whose kit wiring does not live in
		// <surface>_resource.go: its descriptor took that filename's contents and
		// the wiring went to device_kit_resource.go. Trimming "_resource.go"
		// yielded "device_kit" -- a surface nobody has ever heard of, recorded as
		// cut over -- while the real "device" read as uncut from the leftover
		// helper file. Its blocker list is empty, and an empty list is this
		// file's way of saying READY TO CUT OVER, so both guards passed while
		// advertising a migration that had already happened.
		//
		// Metadata is in the same file by necessity -- cmd/metadata-contract-gen
		// parses for it and a promoted one is invisible -- so the served name is
		// always readable right here.
		if served := servedTypeName(source); served != "" {
			name = served
		}
		// OR, NOT ASSIGN, because a surface can span files. device's wiring is in
		// device_kit_resource.go and its leftover helpers in device_resource.go;
		// assigning would let whichever the glob visits last decide, and the glob
		// is sorted, so the helper file would always win.
		out[name] = out[name] || bytes.Contains(source, []byte("resourcekit.Resource["))
	}
	if len(out) == 0 {
		t.Fatal("no resource files found; the detector is broken, not the tree")
	}
	return out
}

func TestEveryUncutSurfaceHasARecordedBlocker(t *testing.T) {
	record := loadSurfaceBlockers(t)
	var missing []string
	for name, cutOver := range resourceSurfaces(t) {
		if cutOver {
			continue
		}
		blockers, ok := record.Surfaces[name]
		if !ok {
			missing = append(missing, name+" (no entry)")
			continue
		}
		// An EMPTY list is legitimate and says "ready to cut over". Treating it
		// as an error would make the record unable to express the state it
		// exists to communicate -- which is what it said before a capability
		// landed and freed five surfaces at once.
		_ = blockers
	}
	sort.Strings(missing)
	for _, name := range missing {
		t.Errorf("%s: a surface that is neither cut over nor recorded as blocked is a "+
			"surface someone will pick next and discover the blocker the expensive way", name)
	}
}

func TestNoRecordedBlockerOutlivesItsSurface(t *testing.T) {
	record := loadSurfaceBlockers(t)
	surfaces := resourceSurfaces(t)
	var stale []string
	for name := range record.Surfaces {
		cutOver, exists := surfaces[name]
		switch {
		case !exists:
			stale = append(stale, name+" (no such surface)")
		case cutOver:
			stale = append(stale, name+" (already served from the kit)")
		}
	}
	sort.Strings(stale)
	for _, name := range stale {
		t.Errorf("%s is still recorded as blocked; a record that outlives its subject "+
			"sends the next reader to solve a problem that is gone", name)
	}
}

// TestEveryRecordedBlockerIsADeclaredKind stops the taxonomy drifting into free
// text, which is how two measures come to disagree about whether they found the
// same thing.
func TestEveryRecordedBlockerIsADeclaredKind(t *testing.T) {
	record := loadSurfaceBlockers(t)
	declared := map[string]bool{}
	for _, kind := range record.BlockerKinds {
		declared[kind] = true
	}
	if len(declared) == 0 {
		t.Fatal("no blocker kinds are declared, so nothing below can fail")
	}
	for surface, blockers := range record.Surfaces {
		for _, blocker := range blockers {
			if !declared[blocker] {
				t.Errorf("%s is recorded with blocker %q, which is not a declared kind",
					surface, blocker)
			}
		}
	}
}

// servedTypeName reads the suffix a file's Metadata method serves, or "" when
// the file declares none.
func servedTypeName(source []byte) string {
	match := servedTypeNamePattern.FindSubmatch(source)
	if match == nil {
		return ""
	}
	return string(match[1])
}

var servedTypeNamePattern = regexp.MustCompile(
	`resp\.TypeName = req\.ProviderTypeName \+ "_([a-z0-9_]+)"`)
