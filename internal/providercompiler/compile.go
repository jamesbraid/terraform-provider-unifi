package providercompiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

var validDispositions = map[string]struct{}{
	"managed":       {},
	"computed":      {},
	"preserve_only": {},
	"omitted":       {},
}

// Compile resolves structural facts and provider policy into generator input
// and reviewable reports. It rejects drift instead of guessing policy.
func Compile(input CompileInput) (Result, error) {
	var rules policy
	if err := decodeJSON("policy", input.Policy, &rules, true); err != nil {
		return Result{}, err
	}
	source, err := structuralSource(input, rules)
	if err != nil {
		return Result{}, err
	}
	var baseline baselineManifest
	if err := decodeJSON("baseline digests", input.BaselineDigests, &baseline, false); err != nil {
		return Result{}, err
	}

	if source.FormatVersion != 1 || rules.FormatVersion != 1 {
		return Result{}, fmt.Errorf("unsupported compiler input format")
	}
	if source.Resource.Name != rules.Resource {
		return Result{}, fmt.Errorf("resource mismatch: bootstrap %q, policy %q", source.Resource.Name, rules.Resource)
	}
	// Both sides must name a source. Comparing two empty strings succeeds and
	// says nothing, so a bootstrap and policy that simply omit the field would
	// have been treated as bound to each other.
	if source.Source.SpecificationSHA256 == "" || rules.SourceSpecificationSHA256 == "" {
		return Result{}, fmt.Errorf(
			"bootstrap and policy must both record the source specification they were derived from; bootstrap %q, policy %q",
			source.Source.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	if source.Source.SpecificationSHA256 != rules.SourceSpecificationSHA256 {
		return Result{}, fmt.Errorf(
			"bootstrap digest mismatch: bootstrap %q, policy %q",
			source.Source.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	surfaceKey := catalogparity.SurfaceKey{Kind: rules.SurfaceKind, Name: rules.Resource}
	// Reject an unsupported surface kind before any digest check, so the
	// operator is told the kind cannot be emitted rather than being sent
	// hunting for a baseline digest that was never the problem.
	if !emittableSurfaceKind(rules.SurfaceKind) {
		return Result{}, fmt.Errorf(
			"no code specification member for surface kind %q: emitting it would generate no code",
			rules.SurfaceKind,
		)
	}
	if err := validateBaseline(rules.BaselineDigests, baseline.SchemaSHA256, surfaceKey); err != nil {
		return Result{}, err
	}
	if err := validateAdmission(input, rules, baseline); err != nil {
		return Result{}, err
	}

	sourceFields := make(map[string]bootstrapField, len(source.Resource.Fields))
	for _, field := range source.Resource.Fields {
		if field.Name == "" || field.Type == "" {
			return Result{}, fmt.Errorf("bootstrap field has empty name or type")
		}
		if _, exists := sourceFields[field.Name]; exists {
			return Result{}, fmt.Errorf("duplicate structural field %q", field.Name)
		}
		sourceFields[field.Name] = field
	}

	policyFields := make(map[string]fieldPolicy, len(rules.Fields))
	terraformNames := make(map[string]string, len(rules.Fields)+len(rules.ProviderOwned))
	for _, field := range rules.Fields {
		if _, exists := policyFields[field.StructuralName]; exists {
			return Result{}, fmt.Errorf("duplicate policy field %q", field.StructuralName)
		}
		if err := validateDisposition(field.Disposition, field.StructuralName); err != nil {
			return Result{}, err
		}
		// An omitted field occupies no Terraform name, because it is never
		// emitted. Claiming one made a name collide with an attribute that
		// does exist: wlan omits the SDK's legacy `schedule` and presents a
		// `schedule` block built from schedule_with_duration, and the omission
		// took the name the block needs. Naming the omitted field something
		// else would be inventing a fact to satisfy a check.
		if field.Disposition != "omitted" {
			if err := claimTerraformName(terraformNames, field.TerraformName, field.StructuralName); err != nil {
				return Result{}, err
			}
		}
		policyFields[field.StructuralName] = field
	}

	// A grouped field is classified by the grouping that consumes it, not at
	// the top level, so gather those before checking coverage.
	grouped, err := groupedStructuralFields(rules.Groupings, policyFields, terraformNames)
	if err != nil {
		return Result{}, err
	}

	// Checked before coverage: a grouping naming a field that does not exist
	// also leaves whatever it should have consumed unclassified, and reporting
	// that consequence sends the reader to the wrong field.
	for _, name := range sortedKeys(grouped) {
		if _, exists := sourceFields[name]; !exists {
			return Result{}, fmt.Errorf(
				"grouping %q consumes %q, which the catalog does not observe",
				grouped[name], name,
			)
		}
	}
	flattened, err := flattenedStructuralFields(rules.Flattenings, sourceFields, policyFields, grouped, terraformNames)
	if err != nil {
		return Result{}, err
	}

	for name := range sourceFields {
		_, classified := policyFields[name]
		_, consumed := grouped[name]
		_, spread := flattened[name]
		if !classified && !consumed && !spread {
			return Result{}, fmt.Errorf("unclassified structural field %q", name)
		}
	}
	for name := range policyFields {
		if _, exists := sourceFields[name]; !exists {
			return Result{}, fmt.Errorf("stale policy field %q", name)
		}
	}

	providerOwned := append([]providerOwnedPolicy(nil), rules.ProviderOwned...)
	for _, seam := range providerOwned {
		if err := validateDisposition(seam.Disposition, seam.TerraformName); err != nil {
			return Result{}, err
		}
		if err := claimTerraformName(terraformNames, seam.TerraformName, "provider-owned seam "+seam.TerraformName); err != nil {
			return Result{}, err
		}
	}

	fieldNames := make([]string, 0, len(sourceFields))
	for name := range sourceFields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	sort.Slice(providerOwned, func(i, j int) bool {
		return providerOwned[i].TerraformName < providerOwned[j].TerraformName
	})

	mapping := mappingReport{
		FormatVersion: 1,
		SurfaceKind:   rules.SurfaceKind,
		SurfaceName:   rules.Resource,
		Resource:      rules.Resource,
		Fields:        make([]mappingField, 0, len(fieldNames)),
		ProviderOwned: make([]providerOwnedMapping, 0, len(providerOwned)),
	}
	attributes := make([]codeAttribute, 0, len(fieldNames)+len(providerOwned))
	blocks := make([]codeAttribute, 0, len(fieldNames))
	for _, name := range fieldNames {
		// A field consumed by a grouping or spread by a flattening is emitted
		// and recorded by that mechanism, not here. Falling through resolved a
		// Terraform type for it anyway, which is meaningless for a field this
		// loop will not emit — and for a collection it demanded a list-or-set
		// decision, so a grouped scalar passed and a grouped collection failed
		// with no name to blame.
		if _, consumed := grouped[name]; consumed {
			continue
		}
		if _, spread := flattened[name]; spread {
			continue
		}
		structural := sourceFields[name]
		field := policyFields[name]
		// Resolved through the shared builder so a scalar, a collection and an
		// object all take one path. A structural type with no specification
		// member must fail here rather than fall through: "array<string>" and
		// "object" are not members, and emitting one produces a document the
		// generator reads while generating no attribute.
		terraformType := field.TerraformType
		// An omitted field is not emitted, so it has no Terraform type and
		// resolving one asks for a decision with no consequence. That is not
		// merely wasted: a wide surface omits dozens of fields, and requiring a
		// list-or-set choice for each buries the omissions a reader should be
		// checking under choices that mean nothing.
		switch {
		case field.Disposition == "omitted":
			terraformType = ""
		case structuralIsObject(structural.Type):
			resolved, err := objectTerraformType(field, structural.Type)
			if err != nil {
				return Result{}, err
			}
			terraformType = resolved
		default:
			if element, isCollection := structuralElementType(structural.Type); isCollection {
				resolved, err := collectionTerraformType(field, element)
				if err != nil {
					return Result{}, err
				}
				terraformType = resolved
			} else if terraformType == "" {
				terraformType = structural.Type
			}
		}
		mapping.Fields = append(mapping.Fields, mappingField{
			StructuralName: structural.Name,
			TerraformName:  field.TerraformName,
			StructuralType: structural.Type,
			TerraformType:  terraformType,
			Disposition:    field.Disposition,
		})
		if field.Disposition == "managed" || field.Disposition == "computed" {
			attribute, err := buildCodeAttribute(field, structural, terraformNames)
			if err != nil {
				return Result{}, fmt.Errorf("field %q: %w", name, err)
			}
			if blockNesting(field.TerraformType) != "" {
				blocks = append(blocks, attribute)
			} else {
				attributes = append(attributes, attribute)
			}
		}
	}
	flattenings := append([]flatteningPolicy(nil), rules.Flattenings...)
	sort.Slice(flattenings, func(i, j int) bool { return flattenings[i].StructuralName < flattenings[j].StructuralName })
	for _, flattening := range flattenings {
		structural := sourceFields[flattening.StructuralName]
		members := append([]flattenedMember(nil), flattening.Members...)
		sort.Slice(members, func(i, j int) bool { return members[i].TerraformName < members[j].TerraformName })
		for _, member := range members {
			inner := bootstrapField{}
			for _, candidate := range structural.Fields {
				if candidate.Name == member.StructuralName {
					inner = candidate
					break
				}
			}
			mapping.Fields = append(mapping.Fields, mappingField{
				StructuralName: flattening.StructuralName + "." + member.StructuralName,
				TerraformName:  member.TerraformName,
				StructuralType: inner.Type,
				TerraformType:  member.TerraformType,
				Disposition:    member.Disposition,
			})
			if member.Disposition != "managed" && member.Disposition != "computed" {
				continue
			}
			attribute, err := buildCodeAttribute(fieldPolicy{
				StructuralName: member.StructuralName,
				TerraformName:  member.TerraformName,
				TerraformType:  member.TerraformType,
				Disposition:    member.Disposition,
				Attribute:      member.Attribute,
			}, inner, terraformNames)
			if err != nil {
				return Result{}, fmt.Errorf("flattening of %q: %w", flattening.StructuralName, err)
			}
			attributes = append(attributes, attribute)
		}
	}

	groupings := append([]groupingPolicy(nil), rules.Groupings...)
	sort.Slice(groupings, func(i, j int) bool { return groupings[i].TerraformName < groupings[j].TerraformName })
	for _, grouping := range groupings {
		attribute, err := buildGroupingAttribute(grouping, sourceFields, terraformNames)
		if err != nil {
			return Result{}, err
		}
		attributes = append(attributes, attribute)
		for _, member := range grouping.Members {
			mapping.Fields = append(mapping.Fields, mappingField{
				StructuralName: member.StructuralName,
				TerraformName:  grouping.TerraformName + "." + member.TerraformName,
				StructuralType: groupedStructuralType(sourceFields, member),
				TerraformType:  member.TerraformType,
				Disposition:    member.Disposition,
			})
		}
	}

	for _, seam := range providerOwned {
		mapping.ProviderOwned = append(mapping.ProviderOwned, providerOwnedMapping{
			TerraformName: seam.TerraformName,
			TerraformType: seam.TerraformType,
			Disposition:   seam.Disposition,
			Generated:     seam.Generated,
		})
		if seam.Generated {
			attribute, err := makeCodeAttribute(seam.TerraformName, seam.TerraformType, seam.Attribute)
			if err != nil {
				return Result{}, fmt.Errorf("provider-owned seam %q: %w", seam.TerraformName, err)
			}
			attributes = append(attributes, attribute)
		}
	}
	sort.Slice(attributes, func(i, j int) bool { return attributes[i].Name < attributes[j].Name })
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].Name < blocks[j].Name })

	generatorName := rules.GeneratorName
	if generatorName == "" {
		generatorName = rules.Resource
	}
	schema := codeSchema{
		Attributes:          attributes,
		Blocks:              blocks,
		MarkdownDescription: rules.Description,
	}
	specification := codeSpecification{
		Version:  "0.1",
		Provider: codeProvider{Name: "unifi"},
	}
	// Emit under the member the generator reads for this surface kind. A kind
	// with no emission path must fail here: putting it under the wrong member
	// produces a specification the generator accepts and silently generates
	// nothing from, so the mistake would surface much later as an undefined
	// symbol rather than as a compile error.
	switch rules.SurfaceKind {
	case catalogparity.ManagedResource:
		specification.Resources = []codeResource{{Name: generatorName, Schema: schema}}
	case catalogparity.DataSource:
		specification.DataSources = []codeDataSource{{Name: generatorName, Schema: schema}}
	default:
		return Result{}, fmt.Errorf(
			"no code specification member for surface kind %q: emitting it would generate no code",
			rules.SurfaceKind,
		)
	}
	impact := impactReport{
		FormatVersion:     1,
		SurfaceKind:       rules.SurfaceKind,
		SurfaceName:       rules.Resource,
		Resource:          rules.Resource,
		Source:            source.Source,
		BaselineDigests:   rules.BaselineDigests,
		StructuralFields:  len(sourceFields),
		GeneratedAttrs:    len(attributes),
		ProviderSeams:     len(providerOwned),
		UnresolvedFields:  []string{},
		StalePolicyFields: []string{},
	}

	providerCodeSpec, err := marshalCanonical(specification)
	if err != nil {
		return Result{}, fmt.Errorf("encode provider code specification: %w", err)
	}
	impactBytes, err := marshalCanonical(impact)
	if err != nil {
		return Result{}, fmt.Errorf("encode impact report: %w", err)
	}
	mappingBytes, err := marshalCanonical(mapping)
	if err != nil {
		return Result{}, fmt.Errorf("encode mapping report: %w", err)
	}

	return Result{
		ProviderCodeSpec: providerCodeSpec,
		ImpactReport:     impactBytes,
		MappingReport:    mappingBytes,
	}, nil
}

func validateAdmission(input CompileInput, rules policy, baseline baselineManifest) error {
	if !validSurfaceKind(rules.SurfaceKind) {
		return fmt.Errorf("unsupported surface kind %q", rules.SurfaceKind)
	}
	if len(input.Ledger) == 0 {
		return fmt.Errorf("catalog admission ledger is required")
	}
	ledger, err := catalogparity.ParseLedger(input.Ledger)
	if err != nil {
		return fmt.Errorf("catalog admission ledger: %w", err)
	}
	if ledger.BaselineSHA256 != byteSHA256(input.BaselineDigests) {
		return fmt.Errorf("catalog admission ledger baseline digest mismatch")
	}
	key := catalogparity.SurfaceKey{Kind: rules.SurfaceKind, Name: rules.Resource}
	// GeneratedShadow may compile; Admitted may ship. Admission needs a
	// campaign receipt, the campaign diffs a candidate binary, and the
	// candidate binary is what compiling produces, so requiring admission to
	// compile is circular and only dns_record ever escaped it. GeneratedShadow
	// breaks the cycle because stateRequiresReceipt exempts it: a shadow
	// candidate carries no receipt, so it does not wait on the campaign it
	// feeds. States below it still cannot compile.
	if err := ledger.Require(
		key,
		catalogparity.GeneratedShadow,
		catalogparity.Admitted,
		catalogparity.ContractParity,
		catalogparity.ReleaseReady,
	); err != nil {
		return fmt.Errorf("catalog admission: %w", err)
	}
	expected := baseline.SchemaSHA256[surfaceBaselineKey(key)]
	for _, entry := range ledger.Entries {
		if entry.SurfaceKey != key {
			continue
		}
		if expected == "" || entry.BaselineSchemaSHA256 != expected {
			return fmt.Errorf("catalog admission baseline schema digest mismatch for %s/%s", key.Kind, key.Name)
		}
		return nil
	}
	return fmt.Errorf("catalog admission surface %s/%s is missing", key.Kind, key.Name)
}

func validSurfaceKind(kind catalogparity.SurfaceKind) bool {
	switch kind {
	case catalogparity.ManagedResource, catalogparity.DataSource, catalogparity.ListResource, catalogparity.Action:
		return true
	default:
		return false
	}
}

// groupedStructuralFields validates every declared grouping and returns which
// observed field each one consumes, mapped to the grouping that consumed it.
//
// The guard this enforces is what keeps a grouping a migration: every member
// either names a field the catalog observed, consumed exactly once across the
// whole policy, or declares itself invented and says why. Nothing else is
// admitted. Without the exactly-once rule a grouping could quietly duplicate a
// field that also appears at the top level, and the schema would claim two
// attributes back one wire field.
func groupedStructuralFields(
	groupings []groupingPolicy,
	policyFields map[string]fieldPolicy,
	terraformNames map[string]string,
) (map[string]string, error) {
	consumed := map[string]string{}
	for _, grouping := range groupings {
		if grouping.TerraformName == "" {
			return nil, fmt.Errorf("grouping has no terraform_name")
		}
		switch grouping.TerraformType {
		case "single_nested", "list_nested", "set_nested":
		case "":
			return nil, fmt.Errorf(
				"grouping %q must declare terraform_type as single_nested, list_nested or set_nested",
				grouping.TerraformName,
			)
		default:
			return nil, fmt.Errorf(
				"grouping %q declares terraform_type %q, which is not a nested member",
				grouping.TerraformName, grouping.TerraformType,
			)
		}
		if err := claimTerraformName(terraformNames, grouping.TerraformName, "grouping "+grouping.TerraformName); err != nil {
			return nil, err
		}
		if len(grouping.Members) == 0 {
			return nil, fmt.Errorf("grouping %q has no members", grouping.TerraformName)
		}
		seen := map[string]struct{}{}
		for _, member := range grouping.Members {
			if member.TerraformName == "" {
				return nil, fmt.Errorf("grouping %q has a member with no terraform_name", grouping.TerraformName)
			}
			if _, duplicate := seen[member.TerraformName]; duplicate {
				return nil, fmt.Errorf(
					"grouping %q repeats member %q", grouping.TerraformName, member.TerraformName,
				)
			}
			seen[member.TerraformName] = struct{}{}
			if err := validateDisposition(member.Disposition, member.TerraformName); err != nil {
				return nil, err
			}
			if member.Invented != "" {
				// An invented member corresponds to nothing observed, so it
				// must not also claim a field, and it must say why it exists.
				if member.StructuralName != "" {
					return nil, fmt.Errorf(
						"grouping %q member %q is declared invented and also names structural field %q",
						grouping.TerraformName, member.TerraformName, member.StructuralName,
					)
				}
				continue
			}
			if member.StructuralName == "" {
				return nil, fmt.Errorf(
					"grouping %q member %q names no structural field and is not declared invented",
					grouping.TerraformName, member.TerraformName,
				)
			}
			if owner, taken := consumed[member.StructuralName]; taken {
				return nil, fmt.Errorf(
					"structural field %q is consumed by groupings %q and %q",
					member.StructuralName, owner, grouping.TerraformName,
				)
			}
			if _, top := policyFields[member.StructuralName]; top {
				return nil, fmt.Errorf(
					"structural field %q is consumed by grouping %q and also classified at the top level",
					member.StructuralName, grouping.TerraformName,
				)
			}
			consumed[member.StructuralName] = grouping.TerraformName
		}
	}
	return consumed, nil
}

// flattenedStructuralFields validates every declared flattening and returns
// which observed object field each one spreads.
//
// The mirror of groupedStructuralFields. A flattening consumes an observed
// object, and every member of that object must be accounted for — promoted to
// a top-level attribute or explicitly omitted — so a member cannot be dropped
// without someone deciding to drop it, and cannot be promoted twice.
func flattenedStructuralFields(
	flattenings []flatteningPolicy,
	sourceFields map[string]bootstrapField,
	policyFields map[string]fieldPolicy,
	grouped map[string]string,
	terraformNames map[string]string,
) (map[string]string, error) {
	consumed := map[string]string{}
	for _, flattening := range flattenings {
		if flattening.StructuralName == "" {
			return nil, fmt.Errorf("flattening has no structural_name")
		}
		structural, observed := sourceFields[flattening.StructuralName]
		if !observed {
			return nil, fmt.Errorf(
				"flattening spreads %q, which the catalog does not observe",
				flattening.StructuralName,
			)
		}
		if !structuralIsObject(structural.Type) {
			return nil, fmt.Errorf(
				"flattening spreads %q, which is type %q rather than an object",
				flattening.StructuralName, structural.Type,
			)
		}
		if _, top := policyFields[flattening.StructuralName]; top {
			return nil, fmt.Errorf(
				"structural field %q is spread by a flattening and also classified at the top level",
				flattening.StructuralName,
			)
		}
		if owner, taken := grouped[flattening.StructuralName]; taken {
			return nil, fmt.Errorf(
				"structural field %q is spread by a flattening and also consumed by grouping %q",
				flattening.StructuralName, owner,
			)
		}
		if owner, taken := consumed[flattening.StructuralName]; taken {
			return nil, fmt.Errorf(
				"structural field %q is spread by two flattenings, %q and %q",
				flattening.StructuralName, owner, flattening.StructuralName,
			)
		}
		consumed[flattening.StructuralName] = flattening.StructuralName

		decided := map[string]struct{}{}
		for _, member := range flattening.Members {
			if member.StructuralName == "" {
				return nil, fmt.Errorf(
					"flattening of %q has a member with no structural_name",
					flattening.StructuralName,
				)
			}
			if !structuralHasMember(structural, member.StructuralName) {
				return nil, fmt.Errorf(
					"flattening of %q promotes %q, which that object does not carry",
					flattening.StructuralName, member.StructuralName,
				)
			}
			if _, twice := decided[member.StructuralName]; twice {
				return nil, fmt.Errorf(
					"flattening of %q promotes %q twice",
					flattening.StructuralName, member.StructuralName,
				)
			}
			decided[member.StructuralName] = struct{}{}
			if err := validateDisposition(member.Disposition, member.TerraformName); err != nil {
				return nil, err
			}
			if member.Disposition == "omitted" {
				continue
			}
			if err := claimTerraformName(terraformNames, member.TerraformName,
				flattening.StructuralName+"."+member.StructuralName); err != nil {
				return nil, err
			}
		}
		for _, member := range structural.Fields {
			if _, ok := decided[member.Name]; !ok {
				return nil, fmt.Errorf(
					"flattening of %q leaves member %q undecided: promote it or omit it",
					flattening.StructuralName, member.Name,
				)
			}
		}
	}
	return consumed, nil
}

// sortedKeys orders map keys so diagnostics do not depend on iteration order.
func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// groupedStructuralType reports the observed type a grouped member consumes.
// An invented member consumes nothing, and the mapping report says so rather
// than borrowing a type it does not have.
func groupedStructuralType(sourceFields map[string]bootstrapField, member groupedMember) string {
	if member.Invented != "" {
		return "invented"
	}
	return sourceFields[member.StructuralName].Type
}

// buildGroupingAttribute emits a declared grouping. Member types come from the
// catalog for observed members; an invented member has no observed type, so its
// policy must supply one.
func buildGroupingAttribute(
	grouping groupingPolicy,
	sourceFields map[string]bootstrapField,
	names map[string]string,
) (codeAttribute, error) {
	members := make([]codeAttribute, 0, len(grouping.Members))
	for _, member := range grouping.Members {
		if member.Disposition == "omitted" {
			continue
		}
		owner := grouping.TerraformName + "." + member.TerraformName
		if err := claimTerraformName(names, owner, owner); err != nil {
			return codeAttribute{}, err
		}
		if member.Invented != "" {
			if member.TerraformType == "" {
				return codeAttribute{}, fmt.Errorf(
					"invented member %q must declare terraform_type: no observed field supplies one", owner,
				)
			}
			attribute, err := makeCodeAttribute(member.TerraformName, member.TerraformType, member.Attribute)
			if err != nil {
				return codeAttribute{}, fmt.Errorf("invented member %q: %w", owner, err)
			}
			members = append(members, attribute)
			continue
		}
		structural := sourceFields[member.StructuralName]
		attribute, err := buildCodeAttribute(fieldPolicy{
			StructuralName: member.StructuralName,
			TerraformName:  member.TerraformName,
			TerraformType:  member.TerraformType,
			Disposition:    member.Disposition,
			Attribute:      member.Attribute,
		}, structural, names)
		if err != nil {
			return codeAttribute{}, fmt.Errorf("grouping %q: %w", grouping.TerraformName, err)
		}
		members = append(members, attribute)
	}
	if len(members) == 0 {
		return codeAttribute{}, fmt.Errorf("grouping %q generates no members", grouping.TerraformName)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })

	body := map[string]json.RawMessage{}
	if len(grouping.Attribute) > 0 {
		if err := json.Unmarshal(grouping.Attribute, &body); err != nil {
			return codeAttribute{}, fmt.Errorf("grouping %q attribute: %w", grouping.TerraformName, err)
		}
	}
	for _, reserved := range []string{"attributes", "nested_object"} {
		if _, present := body[reserved]; present {
			return codeAttribute{}, fmt.Errorf(
				"grouping %q hand-authors %q; members come from its declared member list",
				grouping.TerraformName, reserved,
			)
		}
	}
	encoded, err := json.Marshal(members)
	if err != nil {
		return codeAttribute{}, err
	}
	if grouping.TerraformType == "single_nested" {
		body["attributes"] = encoded
	} else {
		nested, err := json.Marshal(map[string]json.RawMessage{"attributes": encoded})
		if err != nil {
			return codeAttribute{}, err
		}
		body["nested_object"] = nested
	}
	definition, err := json.Marshal(body)
	if err != nil {
		return codeAttribute{}, err
	}
	return codeAttribute{Name: grouping.TerraformName, Type: grouping.TerraformType, Definition: definition}, nil
}

// emittableSurfaceKind reports whether codeSpecification has a member the
// generator reads for this kind. List resources and actions do not yet, and
// must fail rather than be emitted under a member that generates nothing.
func emittableSurfaceKind(kind catalogparity.SurfaceKind) bool {
	return kind == catalogparity.ManagedResource || kind == catalogparity.DataSource
}

func surfaceBaselineKey(key catalogparity.SurfaceKey) string {
	switch key.Kind {
	case catalogparity.ManagedResource:
		return "resource_schemas." + key.Name
	case catalogparity.DataSource:
		return "data_source_schemas." + key.Name
	case catalogparity.ListResource:
		return "list_resource_schemas." + key.Name
	case catalogparity.Action:
		return "action_schemas." + key.Name
	default:
		return ""
	}
}

func structuralSource(input CompileInput, rules policy) (bootstrap, error) {
	if len(input.Bootstrap) > 0 && len(input.Catalog) > 0 {
		return bootstrap{}, fmt.Errorf("bootstrap and catalog inputs are mutually exclusive")
	}
	if len(input.Catalog) == 0 {
		var source bootstrap
		if err := decodeJSON("bootstrap", input.Bootstrap, &source, true); err != nil {
			return bootstrap{}, err
		}
		return source, nil
	}

	var catalog observedCatalog
	if err := decodeJSON("catalog", input.Catalog, &catalog, true); err != nil {
		return bootstrap{}, err
	}
	if catalog.FormatVersion != 1 {
		return bootstrap{}, fmt.Errorf("unsupported catalog format")
	}
	if rules.CatalogID == "" || catalog.CatalogID != rules.CatalogID {
		return bootstrap{}, fmt.Errorf("catalog ID mismatch: catalog %q, policy %q", catalog.CatalogID, rules.CatalogID)
	}
	if rules.CatalogSHA256 == "" || byteSHA256(input.Catalog) != rules.CatalogSHA256 {
		return bootstrap{}, fmt.Errorf("admitted catalog digest mismatch")
	}
	if rules.CatalogSource.Repository == "" || len(rules.CatalogSource.Commit) != 40 || rules.CatalogSource.Path == "" {
		return bootstrap{}, fmt.Errorf("complete admitted catalog source is required")
	}
	if err := validateCatalogTarget(catalog.Target, rules.CatalogTarget, catalog.CatalogID); err != nil {
		return bootstrap{}, err
	}
	if err := validateCatalogSources(catalog.Sources, rules.CatalogSources); err != nil {
		return bootstrap{}, err
	}
	if rules.OperationDigest == "" || catalog.Admission.OperationDigest != rules.OperationDigest {
		return bootstrap{}, fmt.Errorf("admitted operation digest mismatch")
	}
	if catalog.Admission.State != "admitted" {
		return bootstrap{}, fmt.Errorf("catalog admission state %q is not admitted", catalog.Admission.State)
	}
	// A catalog that records no specification digest is not unbound. Its source
	// is pinned by the four capture digests compared below — the capture lock,
	// the structural projection, the semantic predecessor and the semantic IDs
	// — which are recomputed from the catalog itself. The policy's
	// source_specification_sha256 is a bootstrap-path field, and on this path it
	// is not what ties the policy to its source, so a silent catalog is not the
	// hole it appears to be. The comparison below still applies when a catalog
	// does record one.
	if catalog.Sources.SpecificationSHA256 != "" && catalog.Sources.SpecificationSHA256 != rules.SourceSpecificationSHA256 {
		return bootstrap{}, fmt.Errorf(
			"catalog specification digest mismatch: catalog %q, policy %q",
			catalog.Sources.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	if len(catalog.Conflicts) != 0 {
		return bootstrap{}, fmt.Errorf("catalog contains %d unresolved conflicts", len(catalog.Conflicts))
	}
	policyBySemanticID := make(map[string]fieldPolicy, len(rules.Fields))
	for _, field := range rules.Fields {
		if field.SemanticID == "" {
			return bootstrap{}, fmt.Errorf("policy field %q is missing semantic ID", field.StructuralName)
		}
		if _, duplicate := policyBySemanticID[field.SemanticID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate policy semantic ID %q", field.SemanticID)
		}
		policyBySemanticID[field.SemanticID] = field
	}

	coverage := make(map[string]string, len(catalog.Coverage))
	for _, record := range catalog.Coverage {
		if _, duplicate := coverage[record.ID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate catalog coverage for %q", record.ID)
		}
		coverage[record.ID] = record.State
	}
	fields := make([]bootstrapField, 0, len(catalog.StructuralRecords))
	seenFields := make(map[string]struct{}, len(catalog.StructuralRecords))
	structuralByID := make(map[string]catalogStructuralRecord, len(catalog.StructuralRecords))
	for _, record := range catalog.StructuralRecords {
		expectedID := "unifi.network.dns_record.field." + record.Field
		if record.ID != expectedID {
			return bootstrap{}, fmt.Errorf("unstable catalog ID %q for field %q", record.ID, record.Field)
		}
		if _, duplicate := structuralByID[record.ID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate structural semantic ID %q", record.ID)
		}
		if _, duplicate := seenFields[record.Field]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate catalog field %q", record.Field)
		}
		policyField, selected := policyBySemanticID[record.ID]
		if !selected || policyField.StructuralName != record.Field {
			return bootstrap{}, fmt.Errorf("policy semantic ID mismatch for %q", record.ID)
		}
		if catalogDefinitionDigest(record) != record.DefinitionSHA256 {
			return bootstrap{}, fmt.Errorf("definition digest mismatch for %q", record.ID)
		}
		seenFields[record.Field] = struct{}{}
		structuralByID[record.ID] = record
		if coverage[record.ID] != "observed" {
			return bootstrap{}, fmt.Errorf("incomplete catalog coverage for %q", record.ID)
		}
		fieldType, err := providerStructuralType(record.Type)
		if err != nil {
			return bootstrap{}, fmt.Errorf("field %q: %w", record.Field, err)
		}
		fields = append(fields, bootstrapField{Name: record.Field, Type: fieldType})
	}
	if len(fields) == 0 {
		return bootstrap{}, fmt.Errorf("catalog has no structural records")
	}
	observedByID := make(map[string]catalogObservedRecord, len(catalog.ObservedRecords))
	for _, record := range catalog.ObservedRecords {
		if _, duplicate := observedByID[record.ID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate observed semantic ID %q", record.ID)
		}
		structural, known := structuralByID[record.ID]
		if !known {
			return bootstrap{}, fmt.Errorf("unknown observed semantic ID %q", record.ID)
		}
		if record.Field != structural.Field {
			return bootstrap{}, fmt.Errorf("observed field mismatch for %q", record.ID)
		}
		if record.JSONType != structural.Type {
			return bootstrap{}, fmt.Errorf("observed type mismatch for %q: structural %q, observed %q", record.ID, structural.Type, record.JSONType)
		}
		if record.PresentCount < 1 || record.NonNullCount < 0 || record.NonNullCount > record.PresentCount {
			return bootstrap{}, fmt.Errorf("invalid observation counts for %q", record.ID)
		}
		observedByID[record.ID] = record
	}
	for id := range structuralByID {
		if _, observed := observedByID[id]; !observed {
			return bootstrap{}, fmt.Errorf("missing observed record for %q", id)
		}
	}
	for id := range coverage {
		if _, known := structuralByID[id]; !known {
			return bootstrap{}, fmt.Errorf("unknown coverage semantic ID %q", id)
		}
	}
	migrationSources := make(map[string]struct{}, len(catalog.Migrations))
	for _, migration := range catalog.Migrations {
		if migration.FromID == "" || migration.ToID == "" || strings.TrimSpace(migration.Reason) == "" || !migration.Reviewed {
			return bootstrap{}, fmt.Errorf("incomplete migration from %q", migration.FromID)
		}
		if _, duplicate := migrationSources[migration.FromID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate migration from %q", migration.FromID)
		}
		if _, active := structuralByID[migration.FromID]; active {
			return bootstrap{}, fmt.Errorf("migration source %q is still active", migration.FromID)
		}
		if _, known := structuralByID[migration.ToID]; !known {
			return bootstrap{}, fmt.Errorf("migration target %q is not selected", migration.ToID)
		}
		migrationSources[migration.FromID] = struct{}{}
	}
	for _, tombstone := range catalog.Tombstones {
		if _, resolved := migrationSources[tombstone]; !resolved {
			return bootstrap{}, fmt.Errorf("unresolved tombstone %q", tombstone)
		}
	}
	for _, record := range catalog.StructuralRecords {
		if record.SecretCandidate {
			field, ok := policyFieldByStructuralName(rules.Fields, record.Field)
			if !ok || !secretCandidateIsSafe(field) {
				return bootstrap{}, fmt.Errorf("secret candidate %q lacks a safe provider disposition", record.ID)
			}
		}
	}
	return bootstrap{
		FormatVersion: 1,
		Source: bootstrapSource{
			Repository:          rules.CatalogSource.Repository,
			Commit:              rules.CatalogSource.Commit,
			SpecificationSHA256: rules.SourceSpecificationSHA256,
		},
		Resource: bootstrapSchema{Name: rules.Resource, Fields: fields},
	}, nil
}

func catalogDefinitionDigest(record catalogStructuralRecord) string {
	definition, _ := json.Marshal(struct {
		WireName        string `json:"wire_name"`
		JSONType        string `json:"json_type"`
		SecretCandidate bool   `json:"secret_candidate"`
	}{record.Field, record.Type, record.SecretCandidate})
	digest := sha256.Sum256(definition)
	return fmt.Sprintf("%x", digest)
}

func byteSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return fmt.Sprintf("%x", digest)
}

func validateCatalogTarget(actual, expected catalogTarget, catalogID string) error {
	if expected.Name == "" || expected.Product == "" || expected.Version == "" || expected.Architecture == "" ||
		!validSHA256(expected.ImageIndexSHA256, true) || !validSHA256(expected.ImageManifestSHA256, true) ||
		!validSHA256(expected.ControllerFingerprint, true) {
		return fmt.Errorf("complete locked catalog target policy is required")
	}
	if actual.Name == "" || actual.Product == "" || actual.Version == "" || actual.Architecture == "" ||
		!validSHA256(actual.ImageIndexSHA256, true) || !validSHA256(actual.ImageManifestSHA256, true) ||
		!validSHA256(actual.ControllerFingerprint, true) {
		return fmt.Errorf("complete locked catalog target is required")
	}
	if actual != expected {
		return fmt.Errorf("locked catalog target does not match provider policy")
	}
	if catalogID != "unifi.network.dns_record@"+actual.Version {
		return fmt.Errorf("catalog ID does not match locked target version")
	}
	return nil
}

func validateCatalogSources(actual, expected catalogSources) error {
	actualDigests := []string{
		actual.CaptureLockSHA256,
		actual.StructuralProjectionSHA256,
		actual.SemanticPredecessorSHA256,
		actual.SemanticIDsSHA256,
	}
	expectedDigests := []string{
		expected.CaptureLockSHA256,
		expected.StructuralProjectionSHA256,
		expected.SemanticPredecessorSHA256,
		expected.SemanticIDsSHA256,
	}
	for _, digest := range expectedDigests {
		if !validSHA256(digest, false) {
			return fmt.Errorf("complete catalog source digest policy is required")
		}
	}
	for _, digest := range actualDigests {
		if !validSHA256(digest, false) {
			return fmt.Errorf("complete catalog source digests are required")
		}
	}
	if actual.CaptureLockSHA256 != expected.CaptureLockSHA256 ||
		actual.StructuralProjectionSHA256 != expected.StructuralProjectionSHA256 ||
		actual.SemanticPredecessorSHA256 != expected.SemanticPredecessorSHA256 ||
		actual.SemanticIDsSHA256 != expected.SemanticIDsSHA256 {
		return fmt.Errorf("catalog source digests do not match provider policy")
	}
	return nil
}

func validSHA256(value string, prefixed bool) bool {
	if prefixed {
		if !strings.HasPrefix(value, "sha256:") {
			return false
		}
		value = strings.TrimPrefix(value, "sha256:")
	}
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Collection element types deliberately stop at the two the estate actually
// uses. Measured across every surface, element types appear 60 times as
// string, once as int64, and once as a MAC address. The MAC case is a custom
// type over string, so it travels in the policy's attribute definition rather
// than in this vocabulary, and no general element-type system is needed for a
// population that small.
var structuralElementTypes = map[string]string{
	"array<string>": "string",
	"array<int64>":  "int64",
}

// Object structural types carry their members in bootstrapField.Fields rather
// than in the type string. Whether the SDK exposes one struct or a slice of
// them is observable, so it lives here; whether a slice becomes a list or a set
// is not, so policy decides that.
const (
	structuralObject      = "object"
	structuralObjectArray = "array<object>"
)

func structuralIsObject(structuralType string) bool {
	return structuralType == structuralObject || structuralType == structuralObjectArray
}

func providerStructuralType(jsonType string) (string, error) {
	if _, ok := structuralElementTypes[jsonType]; ok {
		return jsonType, nil
	}
	if structuralIsObject(jsonType) {
		return jsonType, nil
	}
	switch jsonType {
	case "number":
		return "int64", nil
	case "bool", "string", "int64":
		return jsonType, nil
	default:
		return "", fmt.Errorf("unsupported structural type %q", jsonType)
	}
}

// objectTerraformType resolves an object field's specification member.
//
// A single struct can only be single_nested. A slice of structs could be
// either list_nested or set_nested, and the SDK cannot say which: it is []T
// regardless of whether order carries meaning. That is the same decision the
// scalar collections require, so it is made the same way, by policy.
// blockNesting maps a declared block type to the specification member that
// carries it, and returns empty for anything that is not a block.
//
// A block is declared explicitly rather than inferred from the SDK, for the
// same reason list and set are: the SDK says []T either way, and whether a
// repeated object is written as a block or as a nested attribute is a decision
// about configuration syntax that no struct can express.
func blockNesting(declared string) string {
	switch declared {
	case "list_nested_block":
		return "list_nested"
	case "set_nested_block":
		return "set_nested"
	case "single_nested_block":
		return "single_nested"
	default:
		return ""
	}
}

func objectTerraformType(field fieldPolicy, structuralType string) (string, error) {
	if structuralType == structuralObject {
		switch field.TerraformType {
		case "", "single_nested":
			return "single_nested", nil
		case "single_nested_block":
			return "single_nested", nil
		default:
			return "", fmt.Errorf(
				"object field %q declares terraform_type %q, want single_nested or single_nested_block",
				field.StructuralName, field.TerraformType,
			)
		}
	}
	switch field.TerraformType {
	case "list_nested", "set_nested":
		return field.TerraformType, nil
	case "list_nested_block", "set_nested_block":
		return blockNesting(field.TerraformType), nil
	case "":
		return "", fmt.Errorf(
			"object collection field %q must declare terraform_type as list_nested, set_nested, list_nested_block or set_nested_block: the SDK type distinguishes none of them, and both order sensitivity and block-versus-attribute syntax are semantic decisions",
			field.StructuralName,
		)
	default:
		return "", fmt.Errorf(
			"object collection field %q declares terraform_type %q, want list_nested, set_nested, list_nested_block or set_nested_block",
			field.StructuralName, field.TerraformType,
		)
	}
}

// nestedDefinition builds an object attribute's specification body: the policy
// supplies the leaf decisions, the catalog supplies the members, and this joins
// them. The member list is generated rather than authored, which is what makes
// this a migration instead of hand-writing moved to another file.
func nestedDefinition(
	field fieldPolicy,
	structural bootstrapField,
	terraformType string,
	names map[string]string,
) (json.RawMessage, error) {
	if len(structural.Fields) == 0 {
		return nil, fmt.Errorf("object field %q has no members in the catalog", field.StructuralName)
	}
	members, err := nestedAttributes(field, structural, names)
	if err != nil {
		return nil, err
	}
	body := map[string]json.RawMessage{}
	if len(field.Attribute) > 0 {
		if err := json.Unmarshal(field.Attribute, &body); err != nil {
			return nil, fmt.Errorf("object field %q attribute: %w", field.StructuralName, err)
		}
	}
	for _, reserved := range []string{"attributes", "nested_object"} {
		if _, present := body[reserved]; present {
			return nil, fmt.Errorf(
				"object field %q hand-authors %q; the member list is derived from the catalog",
				field.StructuralName, reserved,
			)
		}
	}
	encoded, err := json.Marshal(members)
	if err != nil {
		return nil, err
	}
	// A block has no computed_optional_required. Terraform expresses a block's
	// presence by how many times it is written in configuration, not by a
	// disposition, and the specification schema rejects the member outright.
	// The Go type in the specification library does carry the field, so this
	// only shows up when real code is generated — which is why it is refused
	// here by name rather than left to surface as a parse error from the
	// generator with no field to blame.
	if blockNesting(field.TerraformType) != "" {
		if _, present := body["computed_optional_required"]; present {
			return nil, fmt.Errorf(
				"block %q declares computed_optional_required; a block has no disposition, its presence is how many times it is written",
				field.StructuralName,
			)
		}
	}
	if terraformType == "single_nested" {
		body["attributes"] = encoded
	} else {
		nested, err := json.Marshal(map[string]json.RawMessage{"attributes": encoded})
		if err != nil {
			return nil, err
		}
		body["nested_object"] = nested
	}
	return json.Marshal(body)
}

// nestedAttributes walks one object's members, pairing each catalog member with
// its policy decision. It recurses, so nesting depth is bounded by the catalog
// rather than by this code.
func nestedAttributes(
	field fieldPolicy,
	structural bootstrapField,
	names map[string]string,
) ([]codeAttribute, error) {
	decisions := make(map[string]fieldPolicy, len(field.Fields))
	for _, member := range field.Fields {
		decisions[member.StructuralName] = member
	}
	for name, decision := range decisions {
		if decision.Invented != "" {
			continue
		}
		if !structuralHasMember(structural, name) {
			return nil, fmt.Errorf(
				"object field %q has a policy for member %q that the catalog does not observe; "+
					"if the provider invents it, say so with invented and a reason",
				field.StructuralName, name,
			)
		}
	}
	members := make([]codeAttribute, 0, len(structural.Fields))
	for _, member := range structural.Fields {
		decision, classified := decisions[member.Name]
		if !classified {
			return nil, fmt.Errorf(
				"object field %q member %q is unclassified: every member needs a policy decision",
				field.StructuralName, member.Name,
			)
		}
		if err := validateDisposition(decision.Disposition, decision.TerraformName); err != nil {
			return nil, err
		}
		if decision.Disposition == "omitted" {
			continue
		}
		owner := field.StructuralName + "." + member.Name
		if err := claimTerraformName(names, owner+"/"+decision.TerraformName, owner); err != nil {
			return nil, err
		}
		attribute, err := buildCodeAttribute(decision, member, names)
		if err != nil {
			return nil, err
		}
		members = append(members, attribute)
	}
	for _, decision := range field.Fields {
		if decision.Invented == "" || decision.Disposition == "omitted" {
			continue
		}
		owner := field.StructuralName + "." + decision.TerraformName
		if decision.TerraformType == "" {
			return nil, fmt.Errorf(
				"invented member %q must declare terraform_type: no observed field supplies one", owner,
			)
		}
		if err := claimTerraformName(names, owner+"/"+decision.TerraformName, owner); err != nil {
			return nil, err
		}
		attribute, err := makeCodeAttribute(decision.TerraformName, decision.TerraformType, decision.Attribute)
		if err != nil {
			return nil, fmt.Errorf("invented member %q: %w", owner, err)
		}
		members = append(members, attribute)
	}
	if len(members) == 0 {
		return nil, fmt.Errorf("object field %q generates no members", field.StructuralName)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Name < members[j].Name })
	return members, nil
}

func structuralHasMember(structural bootstrapField, name string) bool {
	for _, member := range structural.Fields {
		if member.Name == name {
			return true
		}
	}
	return false
}

// buildCodeAttribute resolves one field, scalar or object, to its
// specification attribute.
func buildCodeAttribute(
	field fieldPolicy,
	structural bootstrapField,
	names map[string]string,
) (codeAttribute, error) {
	if structuralIsObject(structural.Type) {
		terraformType, err := objectTerraformType(field, structural.Type)
		if err != nil {
			return codeAttribute{}, err
		}
		definition, err := nestedDefinition(field, structural, terraformType, names)
		if err != nil {
			return codeAttribute{}, err
		}
		return codeAttribute{Name: field.TerraformName, Type: terraformType, Definition: definition}, nil
	}
	if len(structural.Fields) > 0 {
		return codeAttribute{}, fmt.Errorf(
			"field %q is type %q but the catalog gives it members; only %s and %s carry members",
			field.StructuralName, structural.Type, structuralObject, structuralObjectArray,
		)
	}
	terraformType := field.TerraformType
	if element, isCollection := structuralElementType(structural.Type); isCollection {
		resolved, err := collectionTerraformType(field, element)
		if err != nil {
			return codeAttribute{}, err
		}
		terraformType = resolved
	} else if terraformType == "" {
		terraformType = structural.Type
	} else if err := requireScalarOverride(field.StructuralName, structural.Type, terraformType); err != nil {
		return codeAttribute{}, err
	}
	return makeCodeAttribute(field.TerraformName, terraformType, field.Attribute)
}

// terraformScalarTypes are the specification members that hold a single value.
// Everything else changes how many values an attribute holds, not how one is
// spelled.
var terraformScalarTypes = map[string]bool{
	"bool": true, "string": true, "int64": true, "number": true, "float64": true,
}

// requireScalarOverride rejects a declared terraform_type that changes an
// attribute's cardinality.
//
// Overriding the representation of a single value is established practice and
// nothing else can express it: dns_record presents a numeric ttl as a duration
// string, power_supervisor does the same for three integer second counts. Those
// are scalar for scalar, and the conversion lives in the resource.
//
// Declaring a list over a scalar field is a different act. It says the
// controller sends many values where the SDK says it sends one, and no
// conversion can make that true — the generated schema simply would not
// marshal. Until this check existed the compiler took the declaration at its
// word, so an SDK field that changed from a slice to a plain string compiled
// clean and left the policy describing a list that no longer exists.
func requireScalarOverride(name, structuralType, declared string) error {
	if terraformScalarTypes[structuralType] && terraformScalarTypes[declared] {
		return nil
	}
	return fmt.Errorf(
		"field %q is observed as %q and declares terraform_type %q: an override may change how a "+
			"single value is represented, not how many values there are",
		name, structuralType, declared,
	)
}

// structuralElementType reports the element type of a collection structural
// type, and whether the type is a collection at all.
func structuralElementType(structuralType string) (string, bool) {
	element, ok := structuralElementTypes[structuralType]
	return element, ok
}

// collectionTerraformType resolves a collection field's Terraform type.
//
// The SDK cannot answer set versus list: it is []string either way. Only a
// human knows whether order carries meaning, and getting it wrong produces a
// spurious diff on every plan, so the policy must say and the compiler must
// refuse to guess. The declared element type is then checked against the
// catalog, which is the ground truth for what the SDK actually returns.
func collectionTerraformType(field fieldPolicy, element string) (string, error) {
	// A grouped or flattened member carries no structural name of its own, so
	// naming only that leaves the reader with `collection field ""` and no way
	// to find the field. Fall back to whatever identifies it.
	named := field.StructuralName
	if named == "" {
		named = field.TerraformName
	}
	if named == "" {
		named = "(unnamed)"
	}
	switch field.TerraformType {
	case "list", "set":
	case "":
		return "", fmt.Errorf(
			"collection field %q must declare terraform_type as list or set: the SDK type cannot distinguish them and order sensitivity is a semantic decision",
			named,
		)
	default:
		return "", fmt.Errorf(
			"collection field %q declares terraform_type %q, want list or set",
			named, field.TerraformType,
		)
	}
	declared, err := declaredElementType(field)
	if err != nil {
		return "", err
	}
	if declared != element {
		return "", fmt.Errorf(
			"collection field %q declares element type %q but the catalog observed %q",
			named, declared, element,
		)
	}
	return field.TerraformType, nil
}

// declaredElementType reads element_type from the policy's attribute
// definition. A custom type over an element, such as a MAC address, still
// declares its underlying element here.
func declaredElementType(field fieldPolicy) (string, error) {
	var attribute struct {
		ElementType map[string]json.RawMessage `json:"element_type"`
	}
	if len(field.Attribute) > 0 {
		if err := json.Unmarshal(field.Attribute, &attribute); err != nil {
			return "", fmt.Errorf("collection field %q attribute: %w", field.StructuralName, err)
		}
	}
	if len(attribute.ElementType) != 1 {
		return "", fmt.Errorf(
			"collection field %q must declare exactly one element_type, found %d",
			field.StructuralName, len(attribute.ElementType),
		)
	}
	for name := range attribute.ElementType {
		return name, nil
	}
	return "", nil
}

func policyFieldByStructuralName(fields []fieldPolicy, name string) (fieldPolicy, bool) {
	for _, field := range fields {
		if field.StructuralName == name {
			return field, true
		}
	}
	return fieldPolicy{}, false
}

func secretCandidateIsSafe(field fieldPolicy) bool {
	if field.Disposition == "omitted" {
		return true
	}
	var attribute struct {
		Sensitive bool `json:"sensitive"`
	}
	return len(field.Attribute) > 0 && json.Unmarshal(field.Attribute, &attribute) == nil && attribute.Sensitive
}

func (a codeAttribute) MarshalJSON() ([]byte, error) {
	definition := a.Definition
	if len(definition) == 0 {
		definition = json.RawMessage(`{}`)
	}
	return json.Marshal(map[string]any{
		"name": a.Name,
		a.Type: definition,
	})
}

func makeCodeAttribute(name, attributeType string, definition json.RawMessage) (codeAttribute, error) {
	if name == "" || attributeType == "" {
		return codeAttribute{}, fmt.Errorf("terraform name and type are required")
	}
	if len(definition) == 0 || !json.Valid(definition) {
		return codeAttribute{}, fmt.Errorf("attribute definition is missing or invalid")
	}
	return codeAttribute{Name: name, Type: attributeType, Definition: definition}, nil
}

func decodeJSON(name string, data []byte, target any, strict bool) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode %s: trailing JSON value", name)
		}
		return fmt.Errorf("decode %s: %w", name, err)
	}
	return nil
}

func validateDisposition(disposition, name string) error {
	if _, valid := validDispositions[disposition]; !valid {
		return fmt.Errorf("invalid disposition %q for %q", disposition, name)
	}
	return nil
}

func claimTerraformName(names map[string]string, terraformName, owner string) error {
	if terraformName == "" {
		return fmt.Errorf("empty terraform attribute for %q", owner)
	}
	if existing, claimed := names[terraformName]; claimed {
		return fmt.Errorf("duplicate terraform attribute %q for %q and %q", terraformName, existing, owner)
	}
	names[terraformName] = owner
	return nil
}

// validateBaseline checks the surface's own schema digest, then the companion
// schemas that only a managed resource has.
//
// Companions are per-surface optional. Requiring all three unconditionally
// failed unifi_bgp and unifi_setting (no identity schema) and unifi_account,
// unifi_bgp and unifi_setting (no list resource) with "baseline identity
// digest mismatch", which reads as a stale digest and sends the reader hunting
// for one to regenerate that never existed.
//
// An omitted digest declares the companion absent, and that declaration is
// checked against the baseline manifest in both directions, so neither a
// forgotten digest nor a stale declaration can pass unnoticed.
func validateBaseline(expected baselineDigestSet, actual map[string]string, key catalogparity.SurfaceKey) error {
	primaryKey := surfaceBaselineKey(key)
	if primaryKey == "" {
		return fmt.Errorf("no baseline schema key for surface kind %q", key.Kind)
	}
	primary := expected.Resource
	if key.Kind == catalogparity.DataSource {
		primary = expected.DataSource
	}
	if primary == "" || actual[primaryKey] != primary {
		return fmt.Errorf("baseline %s digest mismatch for %s", key.Kind, key.Name)
	}
	if key.Kind != catalogparity.ManagedResource {
		return nil
	}
	for _, companion := range []struct {
		label string
		key   string
		want  string
	}{
		{"identity", "resource_identity_schemas." + key.Name, expected.Identity},
		{"list resource", "list_resource_schemas." + key.Name, expected.ListResource},
	} {
		have, present := actual[companion.key]
		switch {
		case companion.want == "" && present:
			return fmt.Errorf(
				"policy declares no %s schema for %s but the baseline has one",
				companion.label, key.Name,
			)
		case companion.want != "" && !present:
			return fmt.Errorf(
				"policy declares a %s schema for %s but the baseline has none",
				companion.label, key.Name,
			)
		case companion.want != "" && have != companion.want:
			return fmt.Errorf("baseline %s digest mismatch for %s", companion.label, key.Name)
		}
	}
	return nil
}

func marshalCanonical(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
