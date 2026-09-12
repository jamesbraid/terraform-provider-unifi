// descriptor-emitter writes the mechanical half of each kit descriptor: the
// Terraform model struct and every Fields entry the pipeline's own artifacts
// fully determine. The judgment half -- hooks, Backends, conditional writes,
// custom types, nested-object encodings -- stays in the hand descriptor,
// which lays its entries over the generated list with resourcekit.Override.
//
// Everything emitted is derived, never invented:
//
//   - the wire name, field kind and attribute pairing come from the surface's
//     mapping artifact (provider-codegen/generated/<name>.mapping.json);
//   - the SDK identifier and pointer-ness come from the SDK structs
//     themselves, resolved through internal/sdkshape;
//   - Elide comes from resourcekit.DerivedElide over the generated schema,
//     the same function ElideProblems judges descriptors with;
//   - OmitZero comes from the SDK's own constraint table, where the
//     controller's pattern rejects a literal zero on a non-Required field.
//
// A wire the artifacts cannot fully determine is skipped, not guessed at:
// the hand descriptor supplies it. A wire the hand Spec routes around the
// field list -- AlwaysWire or MappedElsewhere -- is skipped too, read off
// the hand file so a hook-carried secret can never surface as a plain
// generated field.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/sdkshape"
)

const (
	goUnifiPackage         = "github.com/ubiquiti-community/go-unifi/unifi"
	goUnifiSettingsPackage = goUnifiPackage + "/settings"
)

// settingMappingPath locates setting.mapping.json, whose qualified names
// resolve a section to its settings struct; set from -mappings in main.
var settingMappingPath string

func main() {
	log.SetFlags(0)
	log.SetPrefix("descriptor-emitter: ")
	mappings := flag.String("mappings", "generated", "directory holding the *.mapping.json artifacts")
	descriptors := flag.String("descriptors", "../unifi", "directory holding the hand descriptors; output lands beside them")
	flag.Parse()

	settingMappingPath = filepath.Join(*mappings, "setting.mapping.json")

	sdk, err := sdkshape.Load(goUnifiPackage, goUnifiSettingsPackage)
	if err != nil {
		log.Fatalf("resolving %s: %v", goUnifiPackage, err)
	}

	ctx := context.Background()
	for _, s := range surfaces {
		if err := emit(ctx, s, sdk, *mappings, *descriptors); err != nil {
			log.Fatalf("%s: %v", s.name, err)
		}
	}
}

func emit(ctx context.Context, s surface, sdk *sdkshape.Package, mappings, descriptors string) error {
	mapping, err := loadMapping(filepath.Join(mappings, s.name+".mapping.json"))
	if err != nil {
		return err
	}
	hand, err := scanHandDescriptor(filepath.Join(descriptors, s.name+"_descriptor.go"))
	if err != nil {
		return err
	}
	source, err := render(ctx, s, mapping, sdk, hand)
	if err != nil {
		return err
	}
	path := filepath.Join(descriptors, s.name+"_descriptor_gen.go")
	if err := os.WriteFile(path, source, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
