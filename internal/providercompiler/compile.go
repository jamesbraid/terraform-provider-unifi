package providercompiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
	if source.Source.SpecificationSHA256 != rules.SourceSpecificationSHA256 {
		return Result{}, fmt.Errorf(
			"bootstrap digest mismatch: bootstrap %q, policy %q",
			source.Source.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	if err := validateBaseline(rules.BaselineDigests, baseline.SchemaSHA256, rules.Resource); err != nil {
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
		if err := claimTerraformName(terraformNames, field.TerraformName, field.StructuralName); err != nil {
			return Result{}, err
		}
		policyFields[field.StructuralName] = field
	}

	for name := range sourceFields {
		if _, exists := policyFields[name]; !exists {
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
		Resource:      rules.Resource,
		Fields:        make([]mappingField, 0, len(fieldNames)),
		ProviderOwned: make([]providerOwnedMapping, 0, len(providerOwned)),
	}
	attributes := make([]codeAttribute, 0, len(fieldNames)+len(providerOwned))
	for _, name := range fieldNames {
		structural := sourceFields[name]
		field := policyFields[name]
		terraformType := field.TerraformType
		if terraformType == "" {
			terraformType = structural.Type
		}
		mapping.Fields = append(mapping.Fields, mappingField{
			StructuralName: structural.Name,
			TerraformName:  field.TerraformName,
			StructuralType: structural.Type,
			TerraformType:  terraformType,
			Disposition:    field.Disposition,
		})
		if field.Disposition == "managed" || field.Disposition == "computed" {
			attribute, err := makeCodeAttribute(field.TerraformName, terraformType, field.Attribute)
			if err != nil {
				return Result{}, fmt.Errorf("field %q: %w", name, err)
			}
			attributes = append(attributes, attribute)
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

	generatorName := rules.GeneratorName
	if generatorName == "" {
		generatorName = rules.Resource
	}
	specification := codeSpecification{
		Version:  "0.1",
		Provider: codeProvider{Name: "unifi"},
		Resources: []codeResource{{
			Name: generatorName,
			Schema: codeSchema{
				Attributes:          attributes,
				MarkdownDescription: rules.Description,
			},
		}},
	}
	impact := impactReport{
		FormatVersion:     1,
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
	if rules.OperationDigest == "" || catalog.Admission.OperationDigest != rules.OperationDigest {
		return bootstrap{}, fmt.Errorf("admitted operation digest mismatch")
	}
	if catalog.Admission.State != "candidate" && catalog.Admission.State != "admitted" {
		return bootstrap{}, fmt.Errorf("catalog admission state %q is not consumable", catalog.Admission.State)
	}
	if catalog.Sources.SpecificationSHA256 != rules.SourceSpecificationSHA256 {
		return bootstrap{}, fmt.Errorf(
			"catalog specification digest mismatch: catalog %q, policy %q",
			catalog.Sources.SpecificationSHA256,
			rules.SourceSpecificationSHA256,
		)
	}
	if len(catalog.Conflicts) != 0 {
		return bootstrap{}, fmt.Errorf("catalog contains %d unresolved conflicts", len(catalog.Conflicts))
	}

	coverage := make(map[string]string, len(catalog.Coverage))
	for _, record := range catalog.Coverage {
		if _, duplicate := coverage[record.ID]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate catalog coverage for %q", record.ID)
		}
		coverage[record.ID] = record.State
	}
	fields := make([]bootstrapField, 0, len(catalog.StructuralRecords))
	seen := make(map[string]struct{}, len(catalog.StructuralRecords))
	for _, record := range catalog.StructuralRecords {
		expectedID := "unifi.network.dns_record.field." + record.Field
		if record.ID != expectedID {
			return bootstrap{}, fmt.Errorf("unstable catalog ID %q for field %q", record.ID, record.Field)
		}
		if _, duplicate := seen[record.Field]; duplicate {
			return bootstrap{}, fmt.Errorf("duplicate catalog field %q", record.Field)
		}
		seen[record.Field] = struct{}{}
		if coverage[record.ID] != "observed" {
			return bootstrap{}, fmt.Errorf("incomplete catalog coverage for %q", record.ID)
		}
		fields = append(fields, bootstrapField{Name: record.Field, Type: record.Type})
	}
	if len(fields) == 0 {
		return bootstrap{}, fmt.Errorf("catalog has no structural records")
	}
	return bootstrap{
		FormatVersion: 1,
		Source: bootstrapSource{
			Repository:          "catalog:" + catalog.CatalogID,
			Commit:              catalog.Admission.OperationDigest,
			SpecificationSHA256: catalog.Sources.SpecificationSHA256,
		},
		Resource: bootstrapSchema{Name: rules.Resource, Fields: fields},
	}, nil
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

func validateBaseline(expected baselineDigestSet, actual map[string]string, resourceName string) error {
	checks := []struct {
		label string
		key   string
		want  string
	}{
		{"resource", "resource_schemas." + resourceName, expected.Resource},
		{"identity", "resource_identity_schemas." + resourceName, expected.Identity},
		{"list resource", "list_resource_schemas." + resourceName, expected.ListResource},
	}
	for _, check := range checks {
		if check.want == "" || actual[check.key] != check.want {
			return fmt.Errorf("baseline %s digest mismatch", check.label)
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
