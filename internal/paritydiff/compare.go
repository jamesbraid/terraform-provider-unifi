package paritydiff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

var dimensionOrder = []Dimension{
	RequestDimension,
	ControllerResultDimension,
	StateDimension,
	DiagnosticsDimension,
	PlanDimension,
}

func Compare(scenario Scenario, baseline, candidate Observation) Attempt {
	attempt := Attempt{
		FormatVersion: 1,
		Scenario:      scenario,
		BaselineID:    baseline.AdapterID,
		CandidateID:   candidate.AdapterID,
		Differences:   []Difference{},
	}
	if err := validateScenario(scenario); err != nil {
		attempt.Result = Invalid
		attempt.Differences = append(attempt.Differences, Difference{Pointer: "/scenario", Candidate: err.Error()})
		return attempt
	}
	if baseline.AdapterID == "" || candidate.AdapterID == "" || baseline.AdapterID == candidate.AdapterID {
		attempt.Result = Invalid
		attempt.Differences = append(attempt.Differences, Difference{Pointer: "/adapter_id", Baseline: baseline.AdapterID, Candidate: candidate.AdapterID})
		return attempt
	}
	if baseline.ExecutionError != "" || candidate.ExecutionError != "" {
		attempt.Result = Inconclusive
		attempt.Differences = append(attempt.Differences, Difference{
			Pointer:   "/execution_error",
			Baseline:  baseline.ExecutionError,
			Candidate: candidate.ExecutionError,
		})
		return attempt
	}

	dimensions, err := comparisonDimensions(scenario.RequiredDimensions, baseline, candidate)
	if err != nil {
		attempt.Result = Invalid
		attempt.Differences = append(attempt.Differences, Difference{Pointer: "/required_dimensions", Candidate: err.Error()})
		return attempt
	}
	if len(dimensions) == 0 {
		attempt.Result = Uncovered
		attempt.Differences = append(attempt.Differences, Difference{Pointer: "/observations", Candidate: "no comparable dimensions"})
		return attempt
	}

	for _, dimension := range dimensions {
		baselineRaw := observationDimension(baseline, dimension)
		candidateRaw := observationDimension(candidate, dimension)
		if len(baselineRaw) == 0 || len(candidateRaw) == 0 {
			attempt.Result = Uncovered
			attempt.Differences = append(attempt.Differences, Difference{
				Dimension: dimension,
				Pointer:   "/",
				Baseline:  presenceLabel(baselineRaw),
				Candidate: presenceLabel(candidateRaw),
			})
			continue
		}
		baselineValue, err := decodeJSON(baselineRaw)
		if err != nil {
			attempt.Result = Invalid
			attempt.Differences = append(attempt.Differences, Difference{Dimension: dimension, Pointer: "/", Baseline: err.Error()})
			continue
		}
		candidateValue, err := decodeJSON(candidateRaw)
		if err != nil {
			attempt.Result = Invalid
			attempt.Differences = append(attempt.Differences, Difference{Dimension: dimension, Pointer: "/", Candidate: err.Error()})
			continue
		}
		diffValues(dimension, "", baselineValue, candidateValue, &attempt.Differences)
	}

	sort.Slice(attempt.Differences, func(i, j int) bool {
		if attempt.Differences[i].Dimension != attempt.Differences[j].Dimension {
			return attempt.Differences[i].Dimension < attempt.Differences[j].Dimension
		}
		return attempt.Differences[i].Pointer < attempt.Differences[j].Pointer
	})
	if attempt.Result == Invalid || attempt.Result == Uncovered {
		return attempt
	}
	if len(attempt.Differences) != 0 {
		attempt.Result = Divergent
		return attempt
	}
	attempt.Result = Pass
	return attempt
}

func validateScenario(scenario Scenario) error {
	if scenario.ID == "" {
		return fmt.Errorf("scenario ID is required")
	}
	if !strings.HasPrefix(scenario.Surface.Name, "unifi_") {
		return fmt.Errorf("surface name %q is invalid", scenario.Surface.Name)
	}
	switch scenario.Surface.Kind {
	case catalogparity.ManagedResource, catalogparity.DataSource, catalogparity.ListResource, catalogparity.Action:
		return nil
	default:
		return fmt.Errorf("surface kind %q is invalid", scenario.Surface.Kind)
	}
}

func comparisonDimensions(required []Dimension, baseline, candidate Observation) ([]Dimension, error) {
	if len(required) == 0 {
		dimensions := make([]Dimension, 0, len(dimensionOrder))
		for _, dimension := range dimensionOrder {
			if len(observationDimension(baseline, dimension)) != 0 || len(observationDimension(candidate, dimension)) != 0 {
				dimensions = append(dimensions, dimension)
			}
		}
		return dimensions, nil
	}
	seen := make(map[Dimension]struct{}, len(required))
	for _, dimension := range required {
		if !validDimension(dimension) {
			return nil, fmt.Errorf("unknown dimension %q", dimension)
		}
		if _, duplicate := seen[dimension]; duplicate {
			return nil, fmt.Errorf("duplicate dimension %q", dimension)
		}
		seen[dimension] = struct{}{}
	}
	return append([]Dimension(nil), required...), nil
}

func validDimension(dimension Dimension) bool {
	for _, candidate := range dimensionOrder {
		if dimension == candidate {
			return true
		}
	}
	return false
}

func observationDimension(observation Observation, dimension Dimension) json.RawMessage {
	switch dimension {
	case RequestDimension:
		return observation.Request
	case ControllerResultDimension:
		return observation.ControllerResult
	case StateDimension:
		return observation.State
	case DiagnosticsDimension:
		return observation.Diagnostics
	case PlanDimension:
		return observation.Plan
	default:
		return nil
	}
}

func decodeJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values")
		}
		return nil, err
	}
	return value, nil
}

func diffValues(dimension Dimension, pointer string, baseline, candidate any, differences *[]Difference) {
	baselineMap, baselineIsMap := baseline.(map[string]any)
	candidateMap, candidateIsMap := candidate.(map[string]any)
	if baselineIsMap && candidateIsMap {
		keys := make(map[string]struct{}, len(baselineMap)+len(candidateMap))
		for key := range baselineMap {
			keys[key] = struct{}{}
		}
		for key := range candidateMap {
			keys[key] = struct{}{}
		}
		sortedKeys := make([]string, 0, len(keys))
		for key := range keys {
			sortedKeys = append(sortedKeys, key)
		}
		sort.Strings(sortedKeys)
		for _, key := range sortedKeys {
			left, leftExists := baselineMap[key]
			right, rightExists := candidateMap[key]
			childPointer := pointer + "/" + escapePointer(key)
			if !leftExists || !rightExists {
				*differences = append(*differences, Difference{
					Dimension: dimension,
					Pointer:   childPointer,
					Baseline:  renderPresence(left, leftExists),
					Candidate: renderPresence(right, rightExists),
				})
				continue
			}
			diffValues(dimension, childPointer, left, right, differences)
		}
		return
	}

	baselineArray, baselineIsArray := baseline.([]any)
	candidateArray, candidateIsArray := candidate.([]any)
	if baselineIsArray && candidateIsArray {
		maximum := len(baselineArray)
		if len(candidateArray) > maximum {
			maximum = len(candidateArray)
		}
		for index := 0; index < maximum; index++ {
			childPointer := pointer + "/" + strconv.Itoa(index)
			if index >= len(baselineArray) || index >= len(candidateArray) {
				var left, right any
				leftExists := index < len(baselineArray)
				rightExists := index < len(candidateArray)
				if leftExists {
					left = baselineArray[index]
				}
				if rightExists {
					right = candidateArray[index]
				}
				*differences = append(*differences, Difference{
					Dimension: dimension,
					Pointer:   childPointer,
					Baseline:  renderPresence(left, leftExists),
					Candidate: renderPresence(right, rightExists),
				})
				continue
			}
			diffValues(dimension, childPointer, baselineArray[index], candidateArray[index], differences)
		}
		return
	}

	if reflect.DeepEqual(baseline, candidate) {
		return
	}
	if pointer == "" {
		pointer = "/"
	}
	*differences = append(*differences, Difference{
		Dimension: dimension,
		Pointer:   pointer,
		Baseline:  renderJSON(baseline),
		Candidate: renderJSON(candidate),
	})
}

func escapePointer(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}

func renderPresence(value any, exists bool) string {
	if !exists {
		return "<missing>"
	}
	return renderJSON(value)
}

func renderJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("<unencodable: %v>", err)
	}
	return string(data)
}

func presenceLabel(value []byte) string {
	if len(value) == 0 {
		return "<missing>"
	}
	return "<present>"
}
