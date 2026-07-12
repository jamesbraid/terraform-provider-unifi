package unifi

import (
	"context"
	"errors"
	"testing"

	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// fakeSettingsClient is a test-only settingsClient implementation used to
// unit-test the (later) settings engine without a live controller. It keeps
// an in-memory map of sections keyed by setting key, supports fault
// injection via failList/failUpdateOn, and records every UpdateRawSetting
// call in puts so tests can assert on what was sent.
type fakeSettingsClient struct {
	sections map[string]settings.RawSetting

	// failList, if non-nil, is returned verbatim by ListSettings instead of
	// the sections snapshot.
	failList error

	// failUpdateOn maps a setting key to an error UpdateRawSetting should
	// return instead of performing the update.
	failUpdateOn map[string]error

	// puts records every RawSetting passed to UpdateRawSetting, in call
	// order, regardless of whether the call ultimately failed.
	puts []settings.RawSetting
}

func newFakeSettingsClient() *fakeSettingsClient {
	return &fakeSettingsClient{
		sections:     make(map[string]settings.RawSetting),
		failUpdateOn: make(map[string]error),
	}
}

func (f *fakeSettingsClient) ListSettings(ctx context.Context, site string) ([]settings.RawSetting, error) {
	if f.failList != nil {
		return nil, f.failList
	}
	out := make([]settings.RawSetting, 0, len(f.sections))
	for _, s := range f.sections {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSettingsClient) UpdateRawSetting(ctx context.Context, site string, s settings.RawSetting) error {
	f.puts = append(f.puts, s)
	if err := f.failUpdateOn[s.Key]; err != nil {
		return err
	}
	f.sections[s.Key] = s
	return nil
}

// Compile-time assertion that the fake satisfies the seam the real adapter
// implements.
var _ settingsClient = (*fakeSettingsClient)(nil)

func TestFakeClientUpdateThenListRoundTrip(t *testing.T) {
	f := newFakeSettingsClient()
	ctx := context.Background()

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"key":           "mgmt",
			"x_ssh_enabled": true,
			"unmodeled":     "keep",
		},
	}

	if err := f.UpdateRawSetting(ctx, "default", rs); err != nil {
		t.Fatalf("UpdateRawSetting: %v", err)
	}

	if len(f.puts) != 1 {
		t.Fatalf("puts recorded = %d, want 1", len(f.puts))
	}
	if f.puts[0].Key != "mgmt" {
		t.Errorf("puts[0].Key = %q, want mgmt", f.puts[0].Key)
	}

	got, err := f.ListSettings(ctx, "default")
	if err != nil {
		t.Fatalf("ListSettings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("ListSettings returned %d sections, want 1", len(got))
	}
	if got[0].Key != "mgmt" {
		t.Errorf("ListSettings()[0].Key = %q, want mgmt", got[0].Key)
	}
	if got[0].Data["unmodeled"] != "keep" {
		t.Errorf("ListSettings()[0].Data[unmodeled] = %v, want keep (round-trip must preserve unmodeled data)", got[0].Data["unmodeled"])
	}
	if got[0].Data["x_ssh_enabled"] != true {
		t.Errorf("ListSettings()[0].Data[x_ssh_enabled] = %v, want true", got[0].Data["x_ssh_enabled"])
	}
}

func TestFakeClientInjectedUpdateFailureReturnsError(t *testing.T) {
	f := newFakeSettingsClient()
	ctx := context.Background()
	wantErr := errors.New("injected update failure")
	f.failUpdateOn["mgmt"] = wantErr

	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data:        map[string]any{"key": "mgmt"},
	}

	err := f.UpdateRawSetting(ctx, "default", rs)
	if !errors.Is(err, wantErr) {
		t.Fatalf("UpdateRawSetting error = %v, want %v", err, wantErr)
	}

	// The failed update must still be recorded (fault injection observes
	// attempted calls) but must NOT have mutated the section store.
	if len(f.puts) != 1 {
		t.Fatalf("puts recorded = %d, want 1", len(f.puts))
	}
	if _, ok := f.sections["mgmt"]; ok {
		t.Errorf("sections[mgmt] present after failed update, want absent")
	}
}

func TestFakeClientInjectedListFailureReturnsError(t *testing.T) {
	f := newFakeSettingsClient()
	ctx := context.Background()
	wantErr := errors.New("injected list failure")
	f.failList = wantErr

	_, err := f.ListSettings(ctx, "default")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ListSettings error = %v, want %v", err, wantErr)
	}
}
