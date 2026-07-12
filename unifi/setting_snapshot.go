package unifi

import "github.com/ubiquiti-community/go-unifi/unifi/settings"

// rawSettings is a keyed, read-only snapshot over the []settings.RawSetting
// returned by one controller ListSettings call. It is the authoritative
// source of truth for a single settings-engine pass: every section's write
// overlay starts from a dataCopy of its entry here, never from the live
// Data map.
type rawSettings struct {
	byKey map[string]settings.RawSetting
}

// newRawSettings indexes raw by each section's Key. A later entry with a
// duplicate key overwrites an earlier one, matching the controller's own
// one-setting-per-key invariant.
func newRawSettings(raw []settings.RawSetting) rawSettings {
	byKey := make(map[string]settings.RawSetting, len(raw))
	for _, s := range raw {
		byKey[s.Key] = s
	}
	return rawSettings{byKey: byKey}
}

// section returns the raw section for key and whether it was present in the
// snapshot.
func (s rawSettings) section(key string) (settings.RawSetting, bool) {
	sec, ok := s.byKey[key]
	return sec, ok
}

// has reports whether key is present in the snapshot.
func (s rawSettings) has(key string) bool {
	_, ok := s.byKey[key]
	return ok
}

// dataCopy returns a deep copy of key's section Data, safe for a caller to
// mutate in place (including deleting keys) as the base of a write overlay
// without corrupting this snapshot or any other section's view of it. If
// key is absent from the snapshot, dataCopy returns an empty, non-nil map
// so a caller can always overlay onto the result without a nil-map check.
//
// TODO(go-unifi): every section's overlay() starts here rather than from a
// go-unifi typed settings struct (settings.SettingRadius, SettingMgmt, ...),
// even though go-unifi already models each section's fields. PERMANENT: the
// raw map[string]any is the read-modify-write base specifically so fields
// the Terraform schema does not expose (e.g. mgmt's alert_enabled/
// boot_sound/led_enabled — see setting_section_mgmt.go) survive an update
// verbatim. That is a provider schema-scope choice, not a go-unifi gap, and
// holds regardless of how completely go-unifi types a section; there is no
// SDK change that retires this.
func (s rawSettings) dataCopy(key string) map[string]any {
	sec, ok := s.byKey[key]
	if !ok {
		return map[string]any{}
	}
	return deepCopyAny(sec.Data).(map[string]any)
}

// deepCopyAny recursively deep-copies the shapes json.Unmarshal produces
// into map[string]any: nested map[string]any and []any are copied
// recursively; everything else (string, float64, bool, nil, and any other
// scalar) is a JSON-decoded value type and is returned as-is.
func deepCopyAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = deepCopyAny(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = deepCopyAny(val)
		}
		return out
	default:
		return v
	}
}
