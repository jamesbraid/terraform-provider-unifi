package unifi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
	resource_setting "github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_setting"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// TestRadioAiBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey is the unit
// half of radio_ai's masked-write gate, shaped exactly like
// TestCountryBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey
// (setting_country_descriptor_test.go): it runs radioAiKitBackend's
// UpdateFields closure -- the same one Configure wires into the live
// resource -- against an httptest server that keeps the raw, undecoded PUT
// body, and asserts it carries exactly the fields the mask named plus
// "key", not setting_preference when only enabled changed.
func TestRadioAiBackendUpdateFieldsSendsOnlyTheNamedWiresPlusKey(t *testing.T) {
	var body map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/proxy/network/status" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"meta":{"server_version":"10.4.57"}}`))
			return
		}
		raw, _ := io.ReadAll(req.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("the provider sent a body that is not an object: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(append(append([]byte(`{"data":[`), raw...), []byte(`]}`)...))
	}))
	t.Cleanup(server.Close)

	api, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("create the API client: %v", err)
	}

	backend := radioAiKitBackend(api)
	sdk := &settings.RadioAi{Enabled: true, SettingPreference: "manual"}
	if _, err := backend.UpdateFields(context.Background(), "default", sdk, "enabled"); err != nil {
		t.Fatalf("UpdateFields: %v", err)
	}

	want := map[string]bool{"key": true, "enabled": true}
	if len(body) != len(want) {
		t.Fatalf("PUT body has %d key(s) %v, want exactly %v", len(body), keysOf(body), want)
	}
	for name := range want {
		if _, ok := body[name]; !ok {
			t.Errorf("PUT body is missing %q; got %v", name, keysOf(body))
		}
	}
}

// TestRadioAiKitSpecConformance runs the same conformance instruments every
// other kit descriptor's test applies (see setting_mgmt_descriptor_test.go's
// TestMgmtKitSpecConformance), scoped to radio_ai's own nested schema rather
// than a whole resource's, since radio_ai is one section of unifi_setting
// rather than a surface of its own.
func TestRadioAiKitSpecConformance(t *testing.T) {
	ctx := context.Background()
	spec := radioAiKitSpec()
	for _, problem := range resourcekit.WireNameProblems(spec) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.NestedProblems(spec) {
		t.Error(problem)
	}
	built := radioAiNestedSchema(ctx)
	for _, problem := range resourcekit.ElideProblems(spec, built) {
		t.Error(problem)
	}
	for _, problem := range resourcekit.ZeroReadProblems(spec, built) {
		t.Error(problem)
	}
}

// TestRadioAiNestedSchemaHasExactlyItsAttributes guards
// radioAiNestedSchema's type assertion against a generator regression:
// "radio_ai" moving off SingleNestedAttribute would panic every
// conformance test above instead of naming the actual problem, so this
// pins the shape ahead of that.
func TestRadioAiNestedSchemaHasExactlyItsAttributes(t *testing.T) {
	ctx := context.Background()
	built := resource_setting.SettingResourceSchema(ctx)
	if _, ok := built.Attributes["radio_ai"]; !ok {
		t.Fatal(`the generated setting schema has no "radio_ai" attribute`)
	}
	nested := radioAiNestedSchema(ctx)
	if len(nested.Attributes) != 16 {
		t.Errorf("radio_ai has %d attribute(s), want 16; update radioAiKitSpec and this count together",
			len(nested.Attributes))
	}
}

// TestRadioAiOmitZeroInt64 pins radioAiOmitZeroInt64's three cases directly:
// this is the by-hand equivalent of Int64PtrField's own OmitZero, protecting
// the three nested *int64 fields (channels_blacklist.channel/channel_width,
// radios_configuration.channel_width) that OmitZeroProblems can't reach
// because they live inside a hand-written Encode closure, not a top-level
// Int64PtrField -- see this file's own top comment.
func TestRadioAiOmitZeroInt64(t *testing.T) {
	cases := []struct {
		name  string
		value types.Int64
		want  bool // true if the result should be nil
	}{
		{"null", types.Int64Null(), true},
		{"unknown", types.Int64Unknown(), true},
		{"explicit zero", types.Int64Value(0), true},
		{"a real channel", types.Int64Value(36), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := radioAiOmitZeroInt64(tc.value)
			if (got == nil) != tc.want {
				t.Errorf("radioAiOmitZeroInt64(%v) = %v, want nil: %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestRadioAiAfterReceiveNullsEveryUnconfiguredAttribute is radio_ai's own
// CoManaged proof: every one of the section's sixteen exposed attributes
// must come back null when prior (the plan) never configured it, matching
// mgmt's/usg's own plan-conditioned-null shape. A live controller read
// would otherwise leak the controller's own AI-invented channel/power
// assignments into state for an attribute the practitioner never set,
// producing permanent diff noise -- the hazard this dispatch's brief calls
// out by name.
func TestRadioAiAfterReceiveNullsEveryUnconfiguredAttribute(t *testing.T) {
	ctx := context.Background()
	sdk := &settings.RadioAi{
		AutoAdjustChannelsToCountry: true,
		AutoChannelPresetsType:      "maximum_speed",
		Channels6E:                  []int64{37, 53},
		ChannelsBlacklist: []settings.SettingRadioAiChannelsBlacklist{
			{Channel: intPtr(36), ChannelWidth: intPtr(20), Radio: "na"},
		},
		ChannelsNa:          []int64{36, 40},
		ChannelsNg:          []int64{1, 6, 11},
		CronExpr:            "0 3 * * *",
		Enabled:             true,
		ExcludeDevices:      []string{"aa:bb:cc:dd:ee:ff"},
		HighPriorityDevices: []string{"11:22:33:44:55:66"},
		HtModesNa:           []int64{80},
		HtModesNg:           []int64{40},
		Optimize:            []string{"channel", "power"},
		Radios:              []string{"na", "ng"},
		RadiosConfiguration: []settings.SettingRadioAiRadiosConfiguration{
			{ChannelWidth: intPtr(80), Dfs: true, Radio: "na"},
		},
		SettingPreference: "auto",
	}

	spec := radioAiKitSpec()
	var model settingRadioAiModel
	if d := spec.ToModel(ctx, sdk, &model, "default"); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}

	// prior is the zero model: every attribute null/unknown, exactly what a
	// practitioner's plan looks like when radio_ai is never configured.
	var prior settingRadioAiModel
	if d := radioAiAfterReceive(ctx, sdk, &model, prior); d.HasError() {
		t.Fatalf("radioAiAfterReceive: %v", d)
	}

	checks := map[string]bool{
		"auto_adjust_channels_to_country": model.AutoAdjustChannelsToCountry.IsNull(),
		"auto_channel_presets_type":       model.AutoChannelPresetsType.IsNull(),
		"channels_6e":                     model.Channels6E.IsNull(),
		"channels_blacklist":              model.ChannelsBlacklist.IsNull(),
		"channels_na":                     model.ChannelsNa.IsNull(),
		"channels_ng":                     model.ChannelsNg.IsNull(),
		"cron_expr":                       model.CronExpr.IsNull(),
		"enabled":                         model.Enabled.IsNull(),
		"exclude_devices":                 model.ExcludeDevices.IsNull(),
		"high_priority_devices":           model.HighPriorityDevices.IsNull(),
		"ht_modes_na":                     model.HtModesNa.IsNull(),
		"ht_modes_ng":                     model.HtModesNg.IsNull(),
		"optimize":                        model.Optimize.IsNull(),
		"radios":                          model.Radios.IsNull(),
		"radios_configuration":            model.RadiosConfiguration.IsNull(),
		"setting_preference":              model.SettingPreference.IsNull(),
	}
	for name, isNull := range checks {
		if !isNull {
			t.Errorf("%s: not null after radioAiAfterReceive with an unconfigured prior", name)
		}
	}
}

// TestRadioAiAfterReceiveLeavesAConfiguredAttributeAlone is
// TestRadioAiAfterReceiveNullsEveryUnconfiguredAttribute's own control: a
// configured attribute must survive AfterReceive untouched, carrying
// whatever the controller's read decoded -- including a value that has
// drifted from what the plan set, which the section's own top comment
// records as intended, not a bug this hook should hide.
func TestRadioAiAfterReceiveLeavesAConfiguredAttributeAlone(t *testing.T) {
	ctx := context.Background()
	sdk := &settings.RadioAi{Enabled: true, ChannelsNa: []int64{36, 44}}

	spec := radioAiKitSpec()
	var model settingRadioAiModel
	if d := spec.ToModel(ctx, sdk, &model, "default"); d.HasError() {
		t.Fatalf("ToModel: %v", d)
	}

	prior := settingRadioAiModel{
		Enabled:    types.BoolValue(true),
		ChannelsNa: model.ChannelsNa,
	}
	if d := radioAiAfterReceive(ctx, sdk, &model, prior); d.HasError() {
		t.Fatalf("radioAiAfterReceive: %v", d)
	}

	if model.Enabled.IsNull() || !model.Enabled.ValueBool() {
		t.Errorf("enabled = %v, want true (configured, must survive)", model.Enabled)
	}
	if model.ChannelsNa.IsNull() {
		t.Error("channels_na is null, want the controller's own configured value to survive")
	}
}

func intPtr(v int64) *int64 { return &v }
