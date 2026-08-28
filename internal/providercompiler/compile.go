package providercompiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/cmdio"
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
	if err := expandOmittedFields(&rules); err != nil {
		return Result{}, err
	}
	source, err := structuralSource(input)
	if err != nil {
		return Result{}, err
	}
	if err := verifyBootstrapSecretCandidates(source, rules); err != nil {
		return Result{}, err
	}

	if source.FormatVersion != 1 || rules.FormatVersion != 1 {
		return Result{}, fmt.Errorf("unsupported compiler input format")
	}
	if source.Resource.Name != rules.Resource {
		return Result{}, fmt.Errorf("resource mismatch: bootstrap %q, policy %q", source.Resource.Name, rules.Resource)
	}
	// Both sides must name a source: comparing two empty strings would
	// succeed and treat an omitted field on both as bound to each other.
	if source.Source.SpecificationSHA256 == "" || rules.SourceSpecificationSHA256 == "" {
		return Result{}, fmt.Errorf(
			"bootstrap and policy must both record the source specification they were derived from; bootstrap %q, policy %q",
			source.Source.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	if source.Source.SpecificationSHA256 != rules.SourceSpecificationSHA256 {
		return Result{}, &DigestMismatchError{
			Bootstrap: source.Source.SpecificationSHA256,
			Policy:    rules.SourceSpecificationSHA256,
		}
	}
	// Reject an unsupported surface kind up front, rather than downstream
	// where the failure would look unrelated to the real cause.
	if !emittableSurfaceKind(rules.SurfaceKind) {
		return Result{}, fmt.Errorf(
			"no code specification member for surface kind %q: emitting it would generate no code",
			rules.SurfaceKind,
		)
	}

	// Observed fields are keyed by source and name, since a surface may
	// project more than one SDK struct with colliding field names. A field
	// of the lead struct keys as its bare name, so a pre-companion policy is
	// unaffected.
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
	companionStructs := map[string]struct{}{}
	for _, companion := range source.Companions {
		if companion.Struct == "" {
			return Result{}, fmt.Errorf("bootstrap companion has no struct name")
		}
		if _, exists := companionStructs[companion.Struct]; exists {
			return Result{}, fmt.Errorf("bootstrap names companion struct %q twice", companion.Struct)
		}
		// A companion named like a field of the lead struct would make
		// "X.member" ambiguous between a companion field and a flattening of
		// the lead's X.
		if _, collides := sourceFields[companion.Struct]; collides {
			return Result{}, fmt.Errorf(
				"companion struct %q has the same name as an observed field of the lead struct; "+
					"a qualified name would then be ambiguous with a flattening of that field",
				companion.Struct)
		}
		companionStructs[companion.Struct] = struct{}{}
		for _, field := range companion.Fields {
			if field.Name == "" || field.Type == "" {
				return Result{}, fmt.Errorf(
					"bootstrap field of companion %q has empty name or type", companion.Struct)
			}
			key := qualifyField(companion.Struct, field.Name)
			if _, exists := sourceFields[key]; exists {
				return Result{}, fmt.Errorf("duplicate structural field %q", key)
			}
			sourceFields[key] = field
		}
	}

	// A source the bootstrap does not carry is a typo; refuse it here rather
	// than let it surface as an unrelated unclassified-field error.
	if err := declaredSourcesExist(rules, companionStructs); err != nil {
		return Result{}, err
	}

	claimedFields, claimedMembers, err := claimedStructuralFields(rules.SurfaceKind, rules.Claims)
	if err != nil {
		return Result{}, err
	}
	for _, name := range cmdio.SortedKeys(claimedFields) {
		if _, exists := sourceFields[name]; !exists {
			return Result{}, fmt.Errorf(
				"%s consumes %q, which the catalog does not observe",
				claimedFields[name], name,
			)
		}
	}

	policyFields := make(map[string]fieldPolicy, len(rules.Fields))
	claimedTopLevel := make(map[string]fieldPolicy)
	terraformNames := make(map[string]string, len(rules.Fields)+len(rules.ProviderOwned))
	for _, field := range rules.Fields {
		// A top-level field named by a claim relates to the wire through that
		// claim, so it names no structural field of its own and is keyed by
		// the only name it has.
		if field.StructuralName == "" {
			owner, claimed := claimedMembers[field.TerraformName]
			if !claimed {
				return Result{}, fmt.Errorf(
					"top-level field %q names no structural field and is not named by any claim",
					field.TerraformName)
			}
			if err := validateDisposition(field.Disposition, field.TerraformName); err != nil {
				return Result{}, err
			}
			if _, exists := claimedTopLevel[field.TerraformName]; exists {
				return Result{}, fmt.Errorf("duplicate policy field %q", field.TerraformName)
			}
			if field.Disposition != "omitted" {
				if err := claimTerraformName(terraformNames, field.TerraformName, owner); err != nil {
					return Result{}, err
				}
			}
			claimedTopLevel[field.TerraformName] = field
			continue
		}
		key := qualifyField(field.StructuralSource, field.StructuralName)
		if _, exists := policyFields[key]; exists {
			return Result{}, fmt.Errorf("duplicate policy field %q", key)
		}
		if owner, claimed := claimedFields[key]; claimed {
			return Result{}, fmt.Errorf(
				"structural field %q is consumed by %s and also classified at the top level",
				key, owner)
		}
		if err := validateDisposition(field.Disposition, field.StructuralName); err != nil {
			return Result{}, err
		}
		// An omitted field occupies no Terraform name, since it's never
		// emitted -- claiming one anyway could collide with an attribute
		// built to reuse that same name.
		if field.Disposition != "omitted" {
			if err := claimTerraformName(terraformNames, field.TerraformName, field.StructuralName); err != nil {
				return Result{}, err
			}
		}
		policyFields[key] = field
	}

	// A grouped field is classified by the grouping that consumes it, not at
	// the top level, so gather those before checking coverage.
	grouped, err := groupedStructuralFields(rules.Groupings, policyFields, terraformNames, claimedMembers)
	if err != nil {
		return Result{}, err
	}
	for _, name := range cmdio.SortedKeys(grouped) {
		if owner, claimed := claimedFields[name]; claimed {
			return Result{}, fmt.Errorf(
				"structural field %q is consumed by grouping %q and also by %s",
				name, grouped[name], owner)
		}
	}

	// Checked before coverage: a grouping naming a field that does not exist
	// also leaves whatever it should have consumed unclassified, and reporting
	// that consequence sends the reader to the wrong field.
	for _, name := range cmdio.SortedKeys(grouped) {
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

	var unclassified []string
	for _, name := range cmdio.SortedKeys(sourceFields) {
		_, classified := policyFields[name]
		_, consumed := grouped[name]
		_, spread := flattened[name]
		_, related := claimedFields[name]
		if !classified && !consumed && !spread && !related {
			unclassified = append(unclassified, fmt.Sprintf("%q", name))
		}
	}
	if len(unclassified) > 0 {
		// Every one, not the first: a controller bump can add several fields to
		// one surface, and reporting them one compile at a time turns a single
		// policy decision pass into as many round trips as there are fields.
		return Result{}, fmt.Errorf("unclassified structural field %s", strings.Join(unclassified, ", "))
	}
	// A claim naming a member no grouping and no field list declares would
	// otherwise consume its fields and emit nothing, which reads downstream as a
	// field silently dropped rather than as the typo it is.
	declaredMembers := map[string]struct{}{}
	for _, grouping := range rules.Groupings {
		for _, member := range grouping.Members {
			declaredMembers[grouping.TerraformName+"."+member.TerraformName] = struct{}{}
		}
	}
	for _, path := range cmdio.SortedKeys(claimedMembers) {
		if _, top := claimedTopLevel[path]; top {
			continue
		}
		if _, nested := declaredMembers[path]; nested {
			continue
		}
		return Result{}, fmt.Errorf(
			"%s names terraform member %q, which is neither a top-level field nor a member "+
				"of any grouping",
			claimedMembers[path], path)
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
		// and recorded by that mechanism, not here.
		if _, consumed := grouped[name]; consumed {
			continue
		}
		if _, spread := flattened[name]; spread {
			continue
		}
		if _, related := claimedFields[name]; related {
			continue
		}
		structural := sourceFields[name]
		field := policyFields[name]
		terraformType := field.TerraformType
		// An omitted field is not emitted, so it has no Terraform type and
		// resolving one would demand a decision with no consequence.
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
			StructuralName: name,
			TerraformName:  field.TerraformName,
			StructuralType: structural.Type,
			TerraformType:  terraformType,
			Disposition:    field.Disposition,
		})
		if field.Disposition == "managed" || field.Disposition == "computed" {
			attribute, err := buildCodeAttribute(rules.Resource, field, structural, terraformNames)
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
	// A claimed top-level field has no one observed field to take a type
	// from, exactly as a claimed grouping member has none, so the policy
	// supplies it.
	for _, name := range sortedFieldKeys(claimedTopLevel) {
		field := claimedTopLevel[name]
		if field.Disposition != "managed" && field.Disposition != "computed" {
			continue
		}
		if field.TerraformType == "" {
			return Result{}, fmt.Errorf(
				"top-level field %q is named by %s and must declare terraform_type: no one "+
					"observed field supplies it",
				name, claimedMembers[name])
		}
		attribute, err := makeCodeAttribute(field.TerraformName, field.TerraformType, field.Attribute)
		if err != nil {
			return Result{}, fmt.Errorf("top-level field %q named by %s: %w",
				name, claimedMembers[name], err)
		}
		attributes = append(attributes, attribute)
	}

	flattenings := append([]flatteningPolicy(nil), rules.Flattenings...)
	sort.Slice(flattenings, func(i, j int) bool { return flattenings[i].StructuralName < flattenings[j].StructuralName })
	for _, flattening := range flattenings {
		structural := sourceFields[qualifyField(flattening.StructuralSource, flattening.StructuralName)]
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
				StructuralName: qualifyField(flattening.StructuralSource, flattening.StructuralName) + "." + member.StructuralName,
				TerraformName:  member.TerraformName,
				StructuralType: inner.Type,
				TerraformType:  member.TerraformType,
				Disposition:    member.Disposition,
			})
			if member.Disposition != "managed" && member.Disposition != "computed" {
				continue
			}
			attribute, err := buildCodeAttribute(rules.Resource, fieldPolicy{
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
		attribute, err := buildGroupingAttribute(rules.Resource, grouping, sourceFields, terraformNames, claimedMembers)
		if err != nil {
			return Result{}, err
		}
		// Same routing an observed field gets above: a block emitted under
		// "attributes" instead would render as a nested attribute, the same
		// data under different configuration syntax.
		if blockNesting(grouping.TerraformType) != "" {
			blocks = append(blocks, attribute)
		} else {
			attributes = append(attributes, attribute)
		}
		for _, member := range grouping.Members {
			// A claimed member's fields are reported by its claim, in one
			// place; reporting them here too would double-count them.
			if _, claimed := claimedMembers[grouping.TerraformName+"."+member.TerraformName]; claimed {
				continue
			}
			mapping.Fields = append(mapping.Fields, mappingField{
				StructuralName: qualifyField(member.StructuralSource, member.StructuralName),
				TerraformName:  grouping.TerraformName + "." + member.TerraformName,
				StructuralType: groupedStructuralType(sourceFields, member),
				TerraformType:  member.TerraformType,
				Disposition:    member.Disposition,
			})
		}
	}

	claims := append([]claimPolicy(nil), rules.Claims...)
	sort.Slice(claims, func(i, j int) bool {
		return strings.Join(claims[i].StructuralNames, ",") < strings.Join(claims[j].StructuralNames, ",")
	})
	for _, claim := range claims {
		mapping.Fields = append(mapping.Fields, claimMappingRows(claim, sourceFields)...)
	}

	if err := mappingCoversEveryObservedField(mapping, sourceFields, flattened, rules.Flattenings); err != nil {
		return Result{}, err
	}
	// The completeness check above runs against every row, omitted included;
	// only the artifact a reviewer reads is slimmed, once that's settled.
	mapping.Fields = withoutOmittedRows(mapping.Fields)

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
	// Emit under the member the generator reads for this surface kind. A
	// kind with no emission path must fail here: the wrong member produces a
	// specification the generator accepts and silently generates nothing
	// from.
	switch rules.SurfaceKind {
	case ManagedResource:
		specification.Resources = []codeResource{{Name: generatorName, Schema: schema}}
	case DataSource:
		specification.DataSources = []codeDataSource{{Name: generatorName, Schema: schema}}
	case ListResource:
		specification.ListResources = []codeListResource{{Name: generatorName, Schema: schema}}
	case Action:
		specification.Actions = []codeAction{{Name: generatorName, Schema: schema}}
	default:
		return Result{}, fmt.Errorf(
			"no code specification member for surface kind %q: emitting it would generate no code",
			rules.SurfaceKind,
		)
	}
	providerCodeSpec, err := marshalCanonical(specification)
	if err != nil {
		return Result{}, fmt.Errorf("encode provider code specification: %w", err)
	}
	mappingBytes, err := marshalCanonical(mapping)
	if err != nil {
		return Result{}, fmt.Errorf("encode mapping report: %w", err)
	}

	result := Result{ProviderCodeSpec: providerCodeSpec}
	// A list resource's fields are the lead struct's, entirely omitted, plus
	// invented filter attributes with no structural name at all -- there is
	// nothing in a mapping report for it a reviewer would look at. main.go
	// writes no file for one.
	if rules.SurfaceKind != ListResource {
		result.MappingReport = mappingBytes
	}
	return result, nil
}

// groupedStructuralFields validates every declared grouping and returns
// which observed field each one consumes, mapped to the grouping that
// consumed it. Every member either names a field the catalog observed
// exactly once across the whole policy, or declares itself invented and
// says why; nothing else is admitted.
func groupedStructuralFields(
	groupings []groupingPolicy,
	policyFields map[string]fieldPolicy,
	terraformNames map[string]string,
	claimedMembers map[string]string,
) (map[string]string, error) {
	// The grouping alone is what the caller needs; the member is carried
	// alongside it only so a conflict can name both sides of itself.
	type claimant struct{ grouping, member string }
	consumed := map[string]string{}
	claimants := map[string]claimant{}
	for _, grouping := range groupings {
		if grouping.TerraformName == "" {
			return nil, fmt.Errorf("grouping has no terraform_name")
		}
		switch grouping.TerraformType {
		// The plain spellings, plus the _block spellings for the same
		// block-vs-attribute configuration-syntax decision an observed field
		// already declares.
		case "single_nested", "list_nested", "set_nested",
			"single_nested_block", "list_nested_block", "set_nested_block":
		case "":
			return nil, fmt.Errorf(
				"grouping %q must declare terraform_type as single_nested, list_nested or "+
					"set_nested, or the _block spelling of one of those",
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
			path := grouping.TerraformName + "." + member.TerraformName
			claimOwner, claimed := claimedMembers[path]
			if member.Invented != "" {
				// An invented member corresponds to nothing observed, so it
				// must not also claim a field.
				if named := member.StructuralName; named != "" {
					return nil, fmt.Errorf(
						"grouping %q member %q is declared invented and also names structural field %q",
						grouping.TerraformName, member.TerraformName, named,
					)
				}
				if claimed {
					return nil, fmt.Errorf(
						"grouping %q member %q is declared invented and is also named by %s; "+
							"a member either corresponds to nothing observed or takes part in a "+
							"relation to something observed, and both cannot be true",
						grouping.TerraformName, member.TerraformName, claimOwner,
					)
				}
				continue
			}
			// A claimed member relates to the wire through its claim, so it
			// names no field of its own.
			if claimed {
				if named := member.StructuralName; named != "" {
					return nil, fmt.Errorf(
						"grouping %q member %q is named by %s and also names structural field %q; "+
							"the claim already says which fields it relates to",
						grouping.TerraformName, member.TerraformName, claimOwner, named,
					)
				}
				continue
			}
			if member.StructuralName == "" {
				return nil, fmt.Errorf(
					"grouping %q member %q names no structural field, is not declared invented, "+
						"and is not named by any claim",
					grouping.TerraformName, member.TerraformName)
			}
			// One field per member here; anything that is not one-to-one is a
			// claim.
			for _, name := range []string{qualifyField(member.StructuralSource, member.StructuralName)} {
				if owner, taken := claimants[name]; taken {
					// Distinguish a grouping consuming the same field twice
					// from two different groupings disagreeing over it --
					// otherwise both read as "consumed by groupings X and X".
					if owner.grouping == grouping.TerraformName {
						return nil, fmt.Errorf(
							"grouping %q consumes structural field %q twice, in members %q and %q",
							grouping.TerraformName, name, owner.member, member.TerraformName,
						)
					}
					return nil, fmt.Errorf(
						"structural field %q is consumed by groupings %q and %q",
						name, owner.grouping, grouping.TerraformName,
					)
				}
				if _, top := policyFields[name]; top {
					return nil, fmt.Errorf(
						"structural field %q is consumed by grouping %q and also classified at the top level",
						name, grouping.TerraformName,
					)
				}
				consumed[name] = grouping.TerraformName
				claimants[name] = claimant{grouping.TerraformName, member.TerraformName}
			}
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
		structural, observed := sourceFields[qualifyField(flattening.StructuralSource, flattening.StructuralName)]
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

// groupedStructuralType reports the observed type behind one row of the
// mapping report. An invented member consumes nothing, and the report says
// so rather than borrowing a type it does not have.
func groupedStructuralType(sourceFields map[string]bootstrapField, member groupedMember) string {
	if member.Invented != "" {
		return "invented"
	}
	return sourceFields[qualifyField(member.StructuralSource, member.StructuralName)].Type
}

// claimMappingRows reports one row per observed field a claim consumes --
// never one per claim, never one per member-field pair -- since that's the
// artifact the exactly-once accounting is reviewed from. The terraform side
// names every member together: the field relates to the set, not to any one
// member of it.
func claimMappingRows(claim claimPolicy, sourceFields map[string]bootstrapField) []mappingField {
	members := append([]string(nil), claim.TerraformMembers...)
	sort.Strings(members)
	joined := strings.Join(members, ", ")
	rows := make([]mappingField, 0, len(claim.StructuralNames))
	for _, bare := range claim.StructuralNames {
		name := qualifyField(claim.StructuralSource, bare)
		rows = append(rows, mappingField{
			StructuralName: name,
			TerraformName:  joined,
			StructuralType: sourceFields[name].Type,
		})
	}
	return rows
}

// withoutOmittedRows drops the rows a reviewer never needed: an omitted
// field carries no code and no claim on the schema, so a row for it is a
// disposition with no consequence. mappingCoversEveryObservedField has
// already checked the full set for completeness by the time this runs.
func withoutOmittedRows(fields []mappingField) []mappingField {
	kept := make([]mappingField, 0, len(fields))
	for _, field := range fields {
		if field.Disposition == "omitted" {
			continue
		}
		kept = append(kept, field)
	}
	return kept
}

// mappingCoversEveryObservedField checks the mapping report against the
// accounting it's supposed to show: the exactly-once rule is enforced while
// the policy is read, and the report is written afterwards from the same
// policy, so this is an independent second pass over the same fact.
//
// A flattened field is spread rather than emitted, so its members carry the
// rows under parent.member names and the parent carries none -- which is why
// this compares against a constructed expectation rather than counting rows.
func mappingCoversEveryObservedField(
	mapping mappingReport,
	sourceFields map[string]bootstrapField,
	flattened map[string]string,
	flattenings []flatteningPolicy,
) error {
	expected := map[string]int{}
	for name := range sourceFields {
		if _, spread := flattened[name]; spread {
			continue
		}
		expected[name] = 1
	}
	for _, flattening := range flattenings {
		parent := qualifyField(flattening.StructuralSource, flattening.StructuralName)
		for _, member := range flattening.Members {
			expected[parent+"."+member.StructuralName]++
		}
	}

	actual := map[string]int{}
	for _, row := range mapping.Fields {
		// An invented member has no observed field behind it and is reported
		// with an empty structural name; it is counted by neither side.
		if row.StructuralName == "" {
			continue
		}
		actual[row.StructuralName]++
	}

	for _, name := range sortedCountKeys(expected) {
		switch got := actual[name]; got {
		case expected[name]:
		case 0:
			return fmt.Errorf(
				"mapping report has no row for structural field %q: the policy accounts for it "+
					"and the artifact a reviewer reads does not show it",
				name)
		default:
			return fmt.Errorf(
				"mapping report has %d rows for structural field %q, want %d",
				got, name, expected[name])
		}
	}
	for _, name := range sortedCountKeys(actual) {
		if _, wanted := expected[name]; !wanted {
			return fmt.Errorf(
				"mapping report has a row for %q, which is neither an observed field nor a "+
					"member of a flattened one",
				name)
		}
	}
	return nil
}

// declaredSourcesExist refuses a structural_source naming a struct the
// bootstrap does not carry, wherever a policy may name one.
func declaredSourcesExist(rules policy, companions map[string]struct{}) error {
	known := func(source, where string) error {
		if source == "" {
			return nil
		}
		if _, ok := companions[source]; ok {
			return nil
		}
		return fmt.Errorf(
			"%s declares structural_source %q, which the bootstrap does not carry; "+
				"it names %s",
			where, source, describeCompanions(companions))
	}
	for _, field := range rules.Fields {
		if err := known(field.StructuralSource, "top-level field "+field.TerraformName); err != nil {
			return err
		}
	}
	for _, grouping := range rules.Groupings {
		for _, member := range grouping.Members {
			where := "grouping member " + grouping.TerraformName + "." + member.TerraformName
			if err := known(member.StructuralSource, where); err != nil {
				return err
			}
		}
	}
	for _, flattening := range rules.Flattenings {
		if err := known(flattening.StructuralSource, "flattening of "+flattening.StructuralName); err != nil {
			return err
		}
	}
	for _, claim := range rules.Claims {
		where := "claim on " + strings.Join(claim.StructuralNames, ", ")
		if err := known(claim.StructuralSource, where); err != nil {
			return err
		}
	}
	return nil
}

// describeCompanions names what the bootstrap does carry, because "not carried"
// without the alternatives sends the reader to the wrong file.
func describeCompanions(companions map[string]struct{}) string {
	if len(companions) == 0 {
		return "no companion structs at all, only the lead"
	}
	names := make([]string, 0, len(companions))
	for name := range companions {
		names = append(names, name)
	}
	sort.Strings(names)
	return "only " + strings.Join(names, ", ")
}

// qualifyField keys an observed field by the struct it belongs to: a field
// of the lead struct keys as its bare name (so a pre-companion policy is
// unaffected), a companion's field as Struct.field. unqualifyField is the
// only place that parses the result back apart.
func qualifyField(source, name string) string {
	if source == "" {
		return name
	}
	return source + "." + name
}

// unqualifyField reverses qualifyField for one entry of policy.Omitted, the
// only place a qualified name is read back rather than only ever compared.
// A structural name never contains ".", so the first one is always the
// companion separator qualifyField wrote.
func unqualifyField(entry string) (source, name string) {
	before, after, found := strings.Cut(entry, ".")
	if !found {
		return "", entry
	}
	return before, after
}

// expandOmittedFields turns the policy's compact omitted list into the same
// fieldPolicy shape "fields" declares inline, before anything reads
// rules.Fields: verifyBootstrapSecretCandidates and the classification pass
// below both key off it, and neither should need a second, compact-aware
// path through the same decision.
func expandOmittedFields(rules *policy) error {
	for _, entry := range rules.Omitted {
		source, name := unqualifyField(entry)
		if name == "" {
			return fmt.Errorf("omitted field %q names no structural field", entry)
		}
		rules.Fields = append(rules.Fields, fieldPolicy{
			StructuralSource: source,
			StructuralName:   name,
			TerraformName:    name,
			Disposition:      "omitted",
		})
	}
	return nil
}

func sortedFieldKeys(values map[string]fieldPolicy) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCountKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// collapsedElementAttribute emits a list of scalars over an observed
// array<object>, where the released attribute carries one member of the
// element and the rest are omitted. Emitting list_nested instead would
// compile but describe a different schema.
func collapsedElementAttribute(
	owner string,
	member groupedMember,
	structural bootstrapField,
) (codeAttribute, error) {
	if structural.Type != structuralObjectArray {
		return codeAttribute{}, fmt.Errorf(
			"member %q declares element_member %q but consumes %q, which is observed as %q "+
				"rather than %s",
			owner, member.ElementMember, member.StructuralName, structural.Type, structuralObjectArray)
	}
	var element bootstrapField
	for _, candidate := range structural.Fields {
		if candidate.Name == member.ElementMember {
			element = candidate
		}
	}
	if element.Name == "" {
		return codeAttribute{}, fmt.Errorf(
			"member %q declares element_member %q, which %q does not carry",
			owner, member.ElementMember, member.StructuralName)
	}
	// The declared member and the Fields list must agree, stated separately
	// on purpose: deriving "the collapsed member" from whichever one is not
	// omitted would let omitting a second member silently change the
	// attribute's element type.
	var live []string
	decided := map[string]struct{}{}
	for _, decision := range member.Fields {
		decided[decision.StructuralName] = struct{}{}
		if decision.Disposition != "omitted" {
			live = append(live, decision.StructuralName)
		}
	}
	for _, candidate := range structural.Fields {
		if _, ok := decided[candidate.Name]; !ok {
			return codeAttribute{}, fmt.Errorf(
				"member %q collapses %q to element_member %q and leaves member %q undecided: "+
					"promote it or omit it",
				owner, member.StructuralName, member.ElementMember, candidate.Name)
		}
	}
	sort.Strings(live)
	if len(live) != 1 || live[0] != member.ElementMember {
		return codeAttribute{}, fmt.Errorf(
			"member %q declares element_member %q but its field list leaves %d member(s) "+
				"not omitted (%s); a list of scalars carries exactly one",
			owner, member.ElementMember, len(live), strings.Join(live, ", "))
	}
	if member.TerraformType != "list" && member.TerraformType != "set" {
		return codeAttribute{}, fmt.Errorf(
			"member %q declares element_member %q and terraform_type %q, want list or set",
			owner, member.ElementMember, member.TerraformType)
	}
	declared, err := declaredElementType(fieldPolicy{
		StructuralName: member.StructuralName,
		TerraformName:  member.TerraformName,
		Attribute:      member.Attribute,
	})
	if err != nil {
		return codeAttribute{}, err
	}
	observed, err := providerStructuralType(element.Type)
	if err != nil {
		return codeAttribute{}, fmt.Errorf("member %q element_member %q: %w",
			owner, member.ElementMember, err)
	}
	if declared != observed {
		return codeAttribute{}, fmt.Errorf(
			"member %q declares element type %q but the catalog observes %q.%q as %q",
			owner, declared, member.StructuralName, member.ElementMember, observed)
	}
	return makeCodeAttribute(member.TerraformName, member.TerraformType, member.Attribute)
}

// buildGroupingAttribute emits a declared grouping. Member types come from the
// catalog for observed members; an invented member has no observed type, so its
// policy must supply one.
func buildGroupingAttribute(
	surface string,
	grouping groupingPolicy,
	sourceFields map[string]bootstrapField,
	names map[string]string,
	claimedMembers map[string]string,
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
		// A claimed member has no one observed field to take a type from, so
		// the policy supplies it -- the same treatment an invented member
		// gets, since there's no single observed type to compare against.
		if claim, claimed := claimedMembers[owner]; claimed {
			if member.TerraformType == "" {
				return codeAttribute{}, fmt.Errorf(
					"member %q is named by %s and must declare terraform_type: no one "+
						"observed field supplies it",
					owner, claim,
				)
			}
			attribute, err := makeCodeAttribute(member.TerraformName, member.TerraformType, member.Attribute)
			if err != nil {
				return codeAttribute{}, fmt.Errorf("member %q named by %s: %w", owner, claim, err)
			}
			members = append(members, attribute)
			continue
		}
		structural := sourceFields[qualifyField(member.StructuralSource, member.StructuralName)]
		if member.ElementMember != "" {
			attribute, err := collapsedElementAttribute(owner, member, structural)
			if err != nil {
				return codeAttribute{}, err
			}
			members = append(members, attribute)
			continue
		}
		attribute, err := buildCodeAttribute(surface, fieldPolicy{
			StructuralName: member.StructuralName,
			TerraformName:  member.TerraformName,
			TerraformType:  member.TerraformType,
			Disposition:    member.Disposition,
			Attribute:      member.Attribute,
			Fields:         member.Fields,
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
	// A block declares itself with the _block spelling, but the specification
	// member is named the same either way: a list_nested_block grouping is
	// emitted as "list_nested" under "blocks". This is the same mapping an
	// observed block field goes through, and doing it here rather than at the
	// call site keeps the emitted key and the routing decision reading from one
	// function.
	nesting := grouping.TerraformType
	if block := blockNesting(grouping.TerraformType); block != "" {
		nesting = block
	}

	if nesting == "single_nested" {
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
	return codeAttribute{Name: grouping.TerraformName, Type: nesting, Definition: definition}, nil
}

// emittableSurfaceKind reports whether codeSpecification has a member for
// this kind. A kind with no member must fail rather than be emitted under
// whichever member happens to exist.
func emittableSurfaceKind(kind SurfaceKind) bool {
	switch kind {
	case ManagedResource, DataSource,
		ListResource, Action:
		return true
	default:
		return false
	}
}

func structuralSource(input CompileInput) (bootstrap, error) {
	var source bootstrap
	if err := decodeJSON("bootstrap", input.Bootstrap, &source, true); err != nil {
		return bootstrap{}, err
	}
	return source, nil
}

// Collection element types deliberately stop at the two the estate actually
// uses. A custom type over string, such as a MAC address, travels in the
// policy's attribute definition rather than in this vocabulary.
var structuralElementTypes = map[string]string{
	"array<string>": "string",
	"array<int64>":  "int64",
}

// Object structural types carry their members in bootstrapField.Fields
// rather than in the type string: struct-vs-slice is observable, so it
// lives here, but list-vs-set is not, so policy decides that.
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

// blockNesting maps a declared block type to the specification member that
// carries it, and returns empty for anything that is not a block. A block
// is declared explicitly rather than inferred from the SDK: the SDK says
// []T either way, and block-versus-nested-attribute is a configuration
// syntax decision no struct can express.
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

// objectTerraformType resolves an object field's specification member. A
// single struct can only be single_nested; a slice of structs could be
// list_nested or set_nested, and the SDK can't say which, so policy decides
// -- the same decision scalar collections require.
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
	surface string,
	field fieldPolicy,
	structural bootstrapField,
	terraformType string,
	names map[string]string,
) (json.RawMessage, error) {
	if len(structural.Fields) == 0 {
		return nil, fmt.Errorf("object field %q has no members in the catalog", field.StructuralName)
	}
	members, err := nestedAttributes(surface, field, structural, names)
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
	// A block has no computed_optional_required: its presence is how many
	// times it's written in configuration, not a disposition, and the
	// specification schema rejects the member outright.
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
	surface string,
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
	// Every one, not the first: an object field can gain several members in
	// one controller bump, and reporting them one compile at a time turns a
	// single policy decision pass into as many round trips as there are
	// members.
	var unclassifiedMembers []string
	for _, member := range structural.Fields {
		if _, classified := decisions[member.Name]; !classified {
			unclassifiedMembers = append(unclassifiedMembers, fmt.Sprintf("%q", member.Name))
		}
	}
	if len(unclassifiedMembers) > 0 {
		return nil, fmt.Errorf(
			"object field %q has unclassified members %s: every member needs a policy decision",
			field.StructuralName, strings.Join(unclassifiedMembers, ", "),
		)
	}

	members := make([]codeAttribute, 0, len(structural.Fields))
	for _, member := range structural.Fields {
		decision := decisions[member.Name]
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
		attribute, err := buildCodeAttribute(surface, decision, member, names)
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
	surface string,
	field fieldPolicy,
	structural bootstrapField,
	names map[string]string,
) (codeAttribute, error) {
	if structuralIsObject(structural.Type) {
		terraformType, err := objectTerraformType(field, structural.Type)
		if err != nil {
			return codeAttribute{}, err
		}
		definition, err := nestedDefinition(surface, field, structural, terraformType, names)
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
	isCollection := false
	if element, ok := structuralElementType(structural.Type); ok {
		isCollection = true
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
	attribute := field.Attribute
	// Derivation is scoped to scalar attributes: a collection's element type
	// carries no OneOf/RegexMatches shape this task derives (deferred; see
	// the task report), and structuralElementType already routed those here
	// with isCollection set.
	if !isCollection {
		owner := field.StructuralName
		if owner == "" {
			owner = field.TerraformName
		}
		derived, err := deriveConstraintValidators(surface+"."+owner, terraformType, structural.Constraint, attribute)
		if err != nil {
			return codeAttribute{}, err
		}
		attribute = derived
	}
	return makeCodeAttribute(field.TerraformName, terraformType, attribute)
}

// terraformScalarTypes are the specification members that hold a single value.
// Everything else changes how many values an attribute holds, not how one is
// spelled.
var terraformScalarTypes = map[string]bool{
	"bool": true, "string": true, "int64": true, "number": true, "float64": true,
}

// requireScalarOverride rejects a declared terraform_type that changes an
// attribute's cardinality. Overriding how a single value is represented
// (a numeric ttl as a duration string, say) is fine, scalar for scalar; a
// list over a scalar field claims the controller sends many values where
// the SDK says one, which no conversion can make true.
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

// collectionTerraformType resolves a collection field's Terraform type. The
// SDK cannot answer set versus list -- it's []string either way -- so the
// policy must say, and the compiler checks the declared element type
// against the catalog.
func collectionTerraformType(field fieldPolicy, element string) (string, error) {
	// A grouped or flattened member carries no structural name of its own,
	// so fall back to whatever identifies it.
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

// claimedStructuralFields validates every declared claim and returns which
// observed field each one consumes, mapped to the claim that consumed it.
// Exactly-once holds in both directions: a field named by two claims is a
// conflict, and so is a member named by two claims -- two members that both
// write one field must be listed together, under one named function.
func claimedStructuralFields(
	kind SurfaceKind,
	claims []claimPolicy,
) (map[string]string, map[string]string, error) {
	// Only a managed resource writes. A data source, a list resource and an
	// action all read or invoke, so none of them has a to_api direction to
	// describe.
	writes := kind == ManagedResource
	fields := map[string]string{}
	members := map[string]string{}
	for index, claim := range claims {
		// A claim has no name of its own, so diagnostics identify it by what it
		// consumes. Naming it by index would send the reader counting entries in
		// a JSON array.
		owner := "claim on " + strings.Join(claim.StructuralNames, ", ")
		if len(claim.StructuralNames) == 0 {
			owner = fmt.Sprintf("claim %d", index+1)
			return nil, nil, fmt.Errorf("%s names no structural field", owner)
		}
		if len(claim.TerraformMembers) == 0 {
			return nil, nil, fmt.Errorf("%s names no terraform member", owner)
		}
		if claim.Reason == "" {
			return nil, nil, fmt.Errorf(
				"%s declares no reason; the compiler cannot check how the relation works, "+
					"so a reader has to be able to",
				owner)
		}
		// A one-to-one claim is an ordinary member wearing a costume. Refusing
		// it keeps one way to say each thing: the member's own structural_name.
		if len(claim.TerraformMembers) == 1 && len(claim.StructuralNames) == 1 {
			return nil, nil, fmt.Errorf(
				"%s relates one member (%s) to one field (%s); declare structural_name on "+
					"the member instead, so a claim always means a relation that is not "+
					"one-to-one",
				owner, claim.TerraformMembers[0], claim.StructuralNames[0])
		}
		if claim.Mapping == nil {
			return nil, nil, fmt.Errorf(
				"%s declares no mapping; how %d member(s) relate to %d field(s) is a "+
					"decision the compiler cannot see, and inferring it from names has "+
					"bound the wrong field before",
				owner, len(claim.TerraformMembers), len(claim.StructuralNames))
		}
		// from_api always; to_api only where there is a write to describe --
		// a data source never writes, so requiring to_api there would demand
		// a name for a transform that cannot exist.
		if claim.Mapping.FromAPI == "" {
			return nil, nil, fmt.Errorf(
				"%s declares a mapping with no from_api function, which builds the members "+
					"from the observed fields", owner)
		}
		if writes && claim.Mapping.ToAPI == "" {
			return nil, nil, fmt.Errorf(
				"%s declares a mapping with no to_api function, which builds the observed "+
					"fields from the members; both directions are named because they are "+
					"different functions here, not inverses of one another",
				owner)
		}
		if !writes && claim.Mapping.ToAPI != "" {
			return nil, nil, fmt.Errorf(
				"%s declares to_api %q on a %s, which never writes; a name for a transform "+
					"that cannot run is a false claim, not a missing function -- remove it",
				owner, claim.Mapping.ToAPI, kind)
		}
		// A name alone cannot say whether it IS the relation or merely contains
		// it, and those are different strengths of claim.
		switch claim.Mapping.Kind {
		case mappingDedicated, mappingContaining:
		case "":
			return nil, nil, fmt.Errorf(
				"%s declares a mapping with no kind; say %q when the named function does "+
					"this relation and nothing else, or %q when the relation is inline "+
					"inside a larger conversion function, because a reader cannot tell "+
					"which they have from a name",
				owner, mappingDedicated, mappingContaining)
		default:
			return nil, nil, fmt.Errorf(
				"%s declares mapping kind %q; the only kinds are %q and %q",
				owner, claim.Mapping.Kind, mappingDedicated, mappingContaining)
		}

		for _, bare := range claim.StructuralNames {
			if bare == "" {
				return nil, nil, fmt.Errorf("%s lists an empty structural name", owner)
			}
			name := qualifyField(claim.StructuralSource, bare)
			if existing, taken := fields[name]; taken {
				if existing == owner {
					return nil, nil, fmt.Errorf(
						"%s lists structural field %q twice; exactly-once accounting would "+
							"then report it consumed once and count it two",
						owner, name)
				}
				return nil, nil, fmt.Errorf(
					"structural field %q is consumed by two claims, %q and %q",
					name, existing, owner)
			}
			fields[name] = owner
		}
		for _, path := range claim.TerraformMembers {
			if path == "" {
				return nil, nil, fmt.Errorf("%s lists an empty terraform member", owner)
			}
			if existing, taken := members[path]; taken {
				if existing == owner {
					return nil, nil, fmt.Errorf("%s lists terraform member %q twice", owner, path)
				}
				return nil, nil, fmt.Errorf(
					"terraform member %q is named by two claims, %q and %q; a member relates "+
						"to the wire in one way or the schema does not say which",
					path, existing, owner)
			}
			members[path] = owner
		}
	}
	return fields, members, nil
}

// verifyBootstrapSecretCandidates runs the secret-candidate check against
// the bootstrap: every SDK field cmd/sdk-bootstrap marked x_-prefixed must
// have a safe disposition somewhere in the policy, or the compiler refuses
// rather than silently emitting it unmasked.
//
// It searches all four places a policy dispositions a field: fields,
// groupings, flattenings and claims. A claim fails closed -- it names the
// fields it consumes but carries no disposition of its own, so a claimed
// secret candidate is refused rather than silently passed.
func verifyBootstrapSecretCandidates(source bootstrap, rules policy) error {
	var unsafe []string
	for _, observed := range source.Resource.Fields {
		if !observed.SecretCandidate {
			continue
		}
		if err := secretCandidateDispositioned(observed.Name, rules); err != nil {
			unsafe = append(unsafe, err.Error())
		}
	}
	if len(unsafe) > 0 {
		// Every one, not the first: the original returned on the first failure,
		// which turns an authoring task into one compile per field.
		return fmt.Errorf("secret candidates lack a safe provider disposition:\n  %s", strings.Join(unsafe, "\n  "))
	}
	return nil
}

func secretCandidateDispositioned(name string, rules policy) error {
	if field, ok := policyFieldByStructuralName(rules.Fields, name); ok {
		if secretCandidateIsSafe(field) {
			return nil
		}
		return fmt.Errorf("%q is a top-level field that is neither omitted nor sensitive", name)
	}
	for _, group := range rules.Groupings {
		for _, member := range group.Members {
			if member.StructuralName != name {
				continue
			}
			if dispositionIsSafe(member.Disposition, member.Attribute) {
				return nil
			}
			return fmt.Errorf("%q is a member of grouping %q and is neither omitted nor sensitive", name, group.TerraformName)
		}
	}
	for _, flattening := range rules.Flattenings {
		for _, member := range flattening.Members {
			if member.StructuralName != name {
				continue
			}
			if dispositionIsSafe(member.Disposition, member.Attribute) {
				return nil
			}
			return fmt.Errorf("%q is a flattened member and is neither omitted nor sensitive", name)
		}
	}
	for _, claim := range rules.Claims {
		for _, claimed := range claim.StructuralNames {
			if claimed == name {
				return fmt.Errorf("%q is consumed by a claim, whose sensitivity lives on the terraform members it maps into; this check cannot yet follow that and refuses rather than passing", name)
			}
		}
	}
	return fmt.Errorf("%q has no disposition in fields, groupings, flattenings or claims", name)
}

func dispositionIsSafe(disposition string, attribute json.RawMessage) bool {
	if disposition == "omitted" {
		return true
	}
	var decoded struct {
		Sensitive bool `json:"sensitive"`
	}
	return len(attribute) > 0 && json.Unmarshal(attribute, &decoded) == nil && decoded.Sensitive
}
