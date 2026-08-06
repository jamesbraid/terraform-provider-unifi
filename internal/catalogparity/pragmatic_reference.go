package catalogparity

import "fmt"

type PragmaticReferenceKind string

const (
	AliasReference           PragmaticReferenceKind = "alias"
	FleetShapeReference      PragmaticReferenceKind = "fleet_shape"
	SiblingReadReference     PragmaticReferenceKind = "sibling_read"
	ZeroUseEndpointReference PragmaticReferenceKind = "zero_use_endpoint"
)

type PragmaticReference struct {
	SurfaceKey
	Signal           string                 `json:"signal"`
	ReferenceKind    PragmaticReferenceKind `json:"reference_kind"`
	Source           SurfaceKey             `json:"source"`
	FleetCount       int                    `json:"fleet_count"`
	ShapeSHA256      string                 `json:"shape_sha256,omitempty"`
	RequiredEvidence []string               `json:"required_evidence"`
}

type PragmaticReferenceSet struct {
	FormatVersion      int                  `json:"format_version"`
	ProviderAddress    string               `json:"provider_address"`
	InventorySHA256    string               `json:"inventory_sha256"`
	FleetSummarySHA256 string               `json:"fleet_summary_sha256"`
	References         []PragmaticReference `json:"references"`
}

type FleetSurfaceReference struct {
	SurfaceKey
	Count                int      `json:"count"`
	ConfiguredAttributes []string `json:"configured_attributes"`
	ShapeSHA256          string   `json:"shape_sha256,omitempty"`
}

type FleetReferenceSummary struct {
	FormatVersion   int                     `json:"format_version"`
	ProviderAddress string                  `json:"provider_address"`
	Surfaces        []FleetSurfaceReference `json:"surfaces"`
}

type EvidenceGap struct {
	SurfaceKey
	Signal string `json:"signal"`
}

type ResolvedPragmaticSignal struct {
	SurfaceKey
	Signal        string                 `json:"signal"`
	ReferenceKind PragmaticReferenceKind `json:"reference_kind"`
	Source        SurfaceKey             `json:"source"`
	FleetCount    int                    `json:"fleet_count"`
	ShapeSHA256   string                 `json:"shape_sha256,omitempty"`
}

type PragmaticResolution struct {
	FormatVersion           int                       `json:"format_version"`
	Result                  string                    `json:"result"`
	ProviderAddress         string                    `json:"provider_address"`
	InventorySHA256         string                    `json:"inventory_sha256"`
	FleetSummarySHA256      string                    `json:"fleet_summary_sha256"`
	ControllerReceiptSHA256 string                    `json:"controller_receipt_sha256,omitempty"`
	ResolvedSignalCount     int                       `json:"resolved_signal_count"`
	RemainingSignalCount    int                       `json:"remaining_signal_count"`
	Resolved                []ResolvedPragmaticSignal `json:"resolved"`
	Remaining               []EvidenceGap             `json:"remaining"`
}

func ResolvePragmaticReferences(
	inventory EvidenceInventory,
	inventorySHA256 string,
	fleetSummary FleetReferenceSummary,
	fleetSummarySHA256 string,
	referenceSet PragmaticReferenceSet,
) (PragmaticResolution, error) {
	if inventory.FormatVersion != 1 || inventory.ProviderAddress != CanonicalProviderAddress {
		return PragmaticResolution{}, fmt.Errorf("inventory identity is invalid")
	}
	if referenceSet.FormatVersion != 1 || referenceSet.ProviderAddress != CanonicalProviderAddress {
		return PragmaticResolution{}, fmt.Errorf("pragmatic reference identity is invalid")
	}
	if !validSHA256(inventorySHA256) || referenceSet.InventorySHA256 != inventorySHA256 {
		return PragmaticResolution{}, fmt.Errorf("inventory SHA-256 does not match measured inventory")
	}
	if fleetSummary.FormatVersion != 1 || fleetSummary.ProviderAddress != CanonicalProviderAddress {
		return PragmaticResolution{}, fmt.Errorf("fleet reference summary identity is invalid")
	}
	if !validSHA256(fleetSummarySHA256) || referenceSet.FleetSummarySHA256 != fleetSummarySHA256 {
		return PragmaticResolution{}, fmt.Errorf("fleet summary SHA-256 does not match measured summary")
	}

	surfaces := make(map[SurfaceKey]SurfaceEvidenceInventory, len(inventory.Surfaces))
	missing := make(map[EvidenceGap]struct{})
	for _, surface := range inventory.Surfaces {
		if _, duplicate := surfaces[surface.SurfaceKey]; duplicate {
			return PragmaticResolution{}, fmt.Errorf("duplicate inventory surface %s/%s", surface.Kind, surface.Name)
		}
		surfaces[surface.SurfaceKey] = surface
		for _, signal := range surface.MissingSignals {
			missing[EvidenceGap{SurfaceKey: surface.SurfaceKey, Signal: signal}] = struct{}{}
		}
	}
	fleetSurfaces := make(map[SurfaceKey]FleetSurfaceReference, len(fleetSummary.Surfaces))
	for _, surface := range fleetSummary.Surfaces {
		if _, exists := surfaces[surface.SurfaceKey]; !exists {
			return PragmaticResolution{}, fmt.Errorf("fleet reference surface %s/%s is not in the inventory", surface.Kind, surface.Name)
		}
		if _, duplicate := fleetSurfaces[surface.SurfaceKey]; duplicate {
			return PragmaticResolution{}, fmt.Errorf("duplicate fleet reference surface %s/%s", surface.Kind, surface.Name)
		}
		if surface.Count < 0 {
			return PragmaticResolution{}, fmt.Errorf("fleet reference surface %s/%s has a negative count", surface.Kind, surface.Name)
		}
		if surface.Count == 0 {
			if len(surface.ConfiguredAttributes) != 0 || surface.ShapeSHA256 != "" {
				return PragmaticResolution{}, fmt.Errorf("zero-use fleet reference surface %s/%s has a shape", surface.Kind, surface.Name)
			}
		} else {
			if len(surface.ConfiguredAttributes) == 0 || !validSHA256(surface.ShapeSHA256) {
				return PragmaticResolution{}, fmt.Errorf("used fleet reference surface %s/%s has an invalid shape", surface.Kind, surface.Name)
			}
			if !strictlySortedUnique(surface.ConfiguredAttributes) {
				return PragmaticResolution{}, fmt.Errorf("fleet reference attributes for %s/%s are not sorted and unique", surface.Kind, surface.Name)
			}
		}
		fleetSurfaces[surface.SurfaceKey] = surface
	}

	references := make(map[EvidenceGap]PragmaticReference, len(referenceSet.References))
	for _, reference := range referenceSet.References {
		gap := EvidenceGap{SurfaceKey: reference.SurfaceKey, Signal: reference.Signal}
		if _, duplicate := references[gap]; duplicate {
			return PragmaticResolution{}, fmt.Errorf(
				"duplicate pragmatic reference for %s/%s signal %q",
				reference.SurfaceKey.Kind, reference.Name, reference.Signal,
			)
		}
		target, exists := surfaces[reference.SurfaceKey]
		if !exists {
			return PragmaticResolution{}, fmt.Errorf("pragmatic reference target %s/%s is missing", reference.SurfaceKey.Kind, reference.Name)
		}
		if _, exists := missing[gap]; !exists {
			return PragmaticResolution{}, fmt.Errorf(
				"signal %q is not missing for %s/%s", reference.Signal, reference.SurfaceKey.Kind, reference.Name,
			)
		}
		if target.Runtime.Status != FileIdentical {
			return PragmaticResolution{}, fmt.Errorf("surface %s/%s runtime is changed", reference.SurfaceKey.Kind, reference.Name)
		}
		fleetSurface, exists := fleetSurfaces[reference.SurfaceKey]
		if !exists {
			return PragmaticResolution{}, fmt.Errorf("surface %s/%s is missing from the fleet summary", reference.SurfaceKey.Kind, reference.Name)
		}
		if fleetSurface.Count != reference.FleetCount {
			return PragmaticResolution{}, fmt.Errorf("surface %s/%s fleet count is %d, reference has %d", reference.SurfaceKey.Kind, reference.Name, fleetSurface.Count, reference.FleetCount)
		}
		if fleetSurface.ShapeSHA256 != reference.ShapeSHA256 {
			return PragmaticResolution{}, fmt.Errorf("surface %s/%s shape SHA-256 does not match the fleet summary", reference.SurfaceKey.Kind, reference.Name)
		}
		source, exists := surfaces[reference.Source]
		if !exists {
			return PragmaticResolution{}, fmt.Errorf("source surface %s/%s is missing", reference.Source.Kind, reference.Source.Name)
		}
		if source.Runtime.Status != FileIdentical || len(source.MissingSignals) != 0 {
			return PragmaticResolution{}, fmt.Errorf("source surface %s/%s does not have complete identical-runtime evidence", reference.Source.Kind, reference.Source.Name)
		}
		if err := validatePragmaticReference(reference); err != nil {
			return PragmaticResolution{}, fmt.Errorf("surface %s/%s signal %q: %w", reference.SurfaceKey.Kind, reference.Name, reference.Signal, err)
		}
		references[gap] = reference
	}

	resolution := PragmaticResolution{
		FormatVersion:      1,
		Result:             "blocked_evidence",
		ProviderAddress:    CanonicalProviderAddress,
		InventorySHA256:    inventorySHA256,
		FleetSummarySHA256: referenceSet.FleetSummarySHA256,
		Resolved:           make([]ResolvedPragmaticSignal, 0, len(references)),
		Remaining:          make([]EvidenceGap, 0),
	}
	for _, surface := range inventory.Surfaces {
		for _, signal := range surface.MissingSignals {
			gap := EvidenceGap{SurfaceKey: surface.SurfaceKey, Signal: signal}
			reference, resolved := references[gap]
			if !resolved {
				resolution.Remaining = append(resolution.Remaining, gap)
				continue
			}
			resolution.Resolved = append(resolution.Resolved, ResolvedPragmaticSignal{
				SurfaceKey:    reference.SurfaceKey,
				Signal:        reference.Signal,
				ReferenceKind: reference.ReferenceKind,
				Source:        reference.Source,
				FleetCount:    reference.FleetCount,
				ShapeSHA256:   reference.ShapeSHA256,
			})
		}
	}
	resolution.ResolvedSignalCount = len(resolution.Resolved)
	resolution.RemainingSignalCount = len(resolution.Remaining)
	if resolution.RemainingSignalCount == 0 {
		resolution.Result = "references_complete"
	}
	return resolution, nil
}

func validatePragmaticReference(reference PragmaticReference) error {
	wantEvidence := map[PragmaticReferenceKind][]string{
		AliasReference:           {"alias_lifecycle"},
		FleetShapeReference:      {"controller_list", "mapping", "plan_shape"},
		SiblingReadReference:     {"mapping", "sibling_read"},
		ZeroUseEndpointReference: {"controller_list", "mapping", "zero_use_inventory"},
	}
	want, valid := wantEvidence[reference.ReferenceKind]
	if !valid {
		return fmt.Errorf("reference kind %q is invalid", reference.ReferenceKind)
	}
	if !sameStrings(reference.RequiredEvidence, want) {
		return fmt.Errorf("required evidence is %v, want %v", reference.RequiredEvidence, want)
	}
	if reference.ReferenceKind == FleetShapeReference {
		if reference.FleetCount <= 0 {
			return fmt.Errorf("fleet count must be positive")
		}
		if !validSHA256(reference.ShapeSHA256) {
			return fmt.Errorf("shape SHA-256 is invalid")
		}
		return nil
	}
	if reference.FleetCount != 0 {
		return fmt.Errorf("fleet count must be zero for %q", reference.ReferenceKind)
	}
	if reference.ShapeSHA256 != "" {
		return fmt.Errorf("shape SHA-256 is only valid for %q", FleetShapeReference)
	}
	return nil
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range want {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func strictlySortedUnique(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] >= values[index] {
			return false
		}
	}
	return true
}
