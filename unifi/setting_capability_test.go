package unifi

import (
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

func TestSectionCapability(t *testing.T) {
	rs := newRawSettings([]settings.RawSetting{
		{
			BaseSetting: settings.BaseSetting{Key: "mgmt"},
			Data:        map[string]any{"foo": "bar"},
		},
	})

	if got := sectionCapability(rs, "mgmt"); got != capSupported {
		t.Errorf("sectionCapability(%q) = %v, want capSupported", "mgmt", got)
	}
	if got := sectionCapability(rs, "nonexistent"); got != capUnsupported {
		t.Errorf("sectionCapability(%q) = %v, want capUnsupported", "nonexistent", got)
	}
}

func TestCapabilityState_configuredError(t *testing.T) {
	tests := []struct {
		name      string
		state     capabilityState
		wantError bool
	}{
		{"supported", capSupported, false},
		{"unsupported", capUnsupported, true},
		{"unmaterialized", capUnmaterialized, false},
		{"unauthorized", capUnauthorized, true},
		{"unknown", capUnknown, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := tt.state.configuredError("mgmt")
			if got := diags.HasError(); got != tt.wantError {
				t.Errorf("configuredError() HasError() = %v, want %v (diags: %v)", got, tt.wantError, diags)
			}
			if !tt.wantError && len(diags) != 0 {
				t.Errorf("configuredError() = %v, want empty diagnostics", diags)
			}
		})
	}
}
