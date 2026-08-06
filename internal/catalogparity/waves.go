package catalogparity

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type WavePolicy struct {
	FormatVersion   int         `json:"format_version"`
	ProviderAddress string      `json:"provider_address"`
	Waves           []WaveGroup `json:"waves"`
}

type WaveGroup struct {
	Wave             int      `json:"wave"`
	ManagedResources []string `json:"managed_resources,omitempty"`
	DataSources      []string `json:"data_sources,omitempty"`
	ListResources    []string `json:"list_resources,omitempty"`
	Actions          []string `json:"actions,omitempty"`
}

type SurfaceContract struct {
	SurfaceKey
	Wave                 int      `json:"wave"`
	BaselineSchemaSHA256 string   `json:"baseline_schema_sha256"`
	SchemaVersion        *int64   `json:"schema_version"`
	RequiredDimensions   []string `json:"required_dimensions"`
	EvidenceGates        []string `json:"evidence_gates"`
}

type SurfaceContractCorpus struct {
	FormatVersion   int               `json:"format_version"`
	ProviderAddress string            `json:"provider_address"`
	BaselineSHA256  string            `json:"baseline_sha256"`
	SchemaSHA256    string            `json:"schema_sha256"`
	PolicySHA256    string            `json:"policy_sha256"`
	Contracts       []SurfaceContract `json:"contracts"`
}

func ParseWavePolicy(data []byte) (WavePolicy, error) {
	var policy WavePolicy
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return WavePolicy{}, fmt.Errorf("decode wave policy: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return WavePolicy{}, fmt.Errorf("decode wave policy: %w", err)
	}
	return policy, nil
}

func BuildSurfaceContracts(baseline Baseline, versions SchemaVersions, policy WavePolicy) (SurfaceContractCorpus, error) {
	if err := validateBaselineForLedger(baseline); err != nil {
		return SurfaceContractCorpus{}, err
	}
	if versions.SchemaSHA256 != baseline.CanonicalSchemaSHA256 || len(versions.Versions) != len(baseline.Surfaces) {
		return SurfaceContractCorpus{}, fmt.Errorf("schema versions do not match the released baseline")
	}
	if policy.FormatVersion != 1 {
		return SurfaceContractCorpus{}, fmt.Errorf("unsupported wave policy format version %d", policy.FormatVersion)
	}
	if policy.ProviderAddress != CanonicalProviderAddress {
		return SurfaceContractCorpus{}, fmt.Errorf("wave policy provider address %q is not canonical", policy.ProviderAddress)
	}

	assignments := make(map[SurfaceKey]int, len(baseline.Surfaces))
	seenWaves := make(map[int]struct{}, len(policy.Waves))
	for _, group := range policy.Waves {
		if group.Wave < 1 || group.Wave > 5 {
			return SurfaceContractCorpus{}, fmt.Errorf("invalid wave %d", group.Wave)
		}
		if _, duplicate := seenWaves[group.Wave]; duplicate {
			return SurfaceContractCorpus{}, fmt.Errorf("duplicate wave group %d", group.Wave)
		}
		seenWaves[group.Wave] = struct{}{}
		groups := []struct {
			kind  SurfaceKind
			names []string
		}{
			{kind: ManagedResource, names: group.ManagedResources},
			{kind: DataSource, names: group.DataSources},
			{kind: ListResource, names: group.ListResources},
			{kind: Action, names: group.Actions},
		}
		for _, surfaces := range groups {
			for _, name := range surfaces.names {
				key := SurfaceKey{Kind: surfaces.kind, Name: name}
				if _, duplicate := assignments[key]; duplicate {
					return SurfaceContractCorpus{}, fmt.Errorf("duplicate wave assignment for %s/%s", key.Kind, key.Name)
				}
				if _, exists := baseline.Surface(key); !exists {
					return SurfaceContractCorpus{}, fmt.Errorf("wave policy references unknown surface %s/%s", key.Kind, key.Name)
				}
				want := expectedWave(key)
				if want == 0 || group.Wave != want {
					return SurfaceContractCorpus{}, fmt.Errorf("surface %s/%s is assigned to wave %d, want wave %d", key.Kind, key.Name, group.Wave, want)
				}
				assignments[key] = group.Wave
			}
		}
	}

	contracts := make([]SurfaceContract, 0, len(baseline.Surfaces))
	for _, surface := range baseline.Surfaces {
		wave, exists := assignments[surface.SurfaceKey]
		if !exists {
			return SurfaceContractCorpus{}, fmt.Errorf("wave policy is missing surface %s/%s", surface.Kind, surface.Name)
		}
		version, exists := versions.Versions[surface.SurfaceKey]
		if !exists {
			return SurfaceContractCorpus{}, fmt.Errorf("schema version is missing for %s/%s", surface.Kind, surface.Name)
		}
		dimensions, gates := parityRequirements(surface.Kind)
		contracts = append(contracts, SurfaceContract{
			SurfaceKey:           surface.SurfaceKey,
			Wave:                 wave,
			BaselineSchemaSHA256: surface.BaselineSchemaSHA256,
			SchemaVersion:        cloneVersion(version),
			RequiredDimensions:   dimensions,
			EvidenceGates:        gates,
		})
	}
	policyData, err := json.Marshal(policy)
	if err != nil {
		return SurfaceContractCorpus{}, fmt.Errorf("encode wave policy: %w", err)
	}
	return SurfaceContractCorpus{
		FormatVersion:   1,
		ProviderAddress: baseline.ProviderAddress,
		BaselineSHA256:  baseline.SourceSHA256,
		SchemaSHA256:    versions.SchemaSHA256,
		PolicySHA256:    byteSHA256(append(policyData, '\n')),
		Contracts:       contracts,
	}, nil
}

func (c SurfaceContractCorpus) WaveCounts() map[int]int {
	counts := make(map[int]int)
	for _, contract := range c.Contracts {
		counts[contract.Wave]++
	}
	return counts
}

func (c SurfaceContractCorpus) Contract(key SurfaceKey) (SurfaceContract, bool) {
	for _, contract := range c.Contracts {
		if contract.SurfaceKey == key {
			return contract, true
		}
	}
	return SurfaceContract{}, false
}

func expectedWave(key SurfaceKey) int {
	switch key.Kind {
	case DataSource, ListResource:
		return 1
	case Action:
		if key.Name == "unifi_port" {
			return 5
		}
	case ManagedResource:
		switch key.Name {
		case "unifi_site", "unifi_setting", "unifi_network", "unifi_wan", "unifi_dns_record", "unifi_dynamic_dns", "unifi_firewall_zone", "unifi_firewall_group":
			return 2
		case "unifi_ap_group", "unifi_client", "unifi_device", "unifi_wlan", "unifi_port_profile", "unifi_port_forward", "unifi_firewall_policy", "unifi_vpn_server", "unifi_wireguard_peer":
			return 3
		case "unifi_account", "unifi_bgp", "unifi_client_qos_rate", "unifi_firewall_rule", "unifi_power_supervisor", "unifi_radius_profile", "unifi_radius_user", "unifi_site_to_site_vpn", "unifi_static_route", "unifi_traffic_route", "unifi_vpn_client":
			return 4
		}
	}
	return 0
}

func parityRequirements(kind SurfaceKind) ([]string, []string) {
	switch kind {
	case ManagedResource:
		return []string{"request", "controller_result", "state", "diagnostics", "plan"}, []string{"static_contract", "mapping", "adapter_differential", "locked_controller_lifecycle", "migration", "recovery"}
	case DataSource:
		return []string{"request", "controller_result", "state", "diagnostics"}, []string{"static_contract", "mapping", "adapter_differential", "locked_controller_lifecycle"}
	case ListResource:
		return []string{"request", "controller_result", "state", "diagnostics", "plan"}, []string{"static_contract", "mapping", "pagination_filter", "adapter_differential", "locked_controller_lifecycle"}
	case Action:
		return []string{"request", "controller_result", "diagnostics", "plan"}, []string{"static_contract", "mapping", "adapter_differential", "locked_controller_lifecycle", "hardware_claim"}
	default:
		return nil, nil
	}
}
