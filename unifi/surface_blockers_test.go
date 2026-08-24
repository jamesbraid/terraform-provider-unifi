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

// The recorded blockers must agree with the tree, in both directions. This
// checks bookkeeping rather than re-deriving the blockers: a hand-won
// finding has no automatable source, so an instrument that only enumerates
// its own sources and proceeds without one reads as more rigorous while
// missing exactly the surfaces it should have blocked.

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
		// "Cut over" means served from the kit, not merely that a descriptor
		// file exists: a descriptor can be written but not yet wired, so this
		// reads whether the resource actually embeds
		// resourcekit.Resource[Model, SDK] rather than checking a
		// _descriptor.go file's presence.
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// The surface name comes from what the file serves (via Metadata),
		// not from the filename: a surface's kit wiring can live in a
		// differently-named file (device's is in device_kit_resource.go), so
		// trimming "_resource.go" from the path can both name a surface that
		// doesn't exist and miss the real one.
		if served := servedTypeName(source); served != "" {
			name = served
		}
		// OR, not assign: a surface can span files (device's wiring and its
		// leftover helpers), so assigning would let file-glob order decide.
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
		// An empty list is legitimate: it says "ready to cut over". Treating
		// it as an error would make the record unable to express that state.
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
