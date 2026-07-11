package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
	"github.com/ubiquiti-community/go-unifi/unifi/settings"
)

// TestRawSettingMarshalPreservesUnmodeled proves that go-unifi's
// settings.RawSetting.MarshalJSON merges the unmodeled Data map into the wire
// body alongside the modeled BaseSetting fields, rather than dropping unknown
// keys. This is the wire-format preservation guarantee the settingsClient
// seam depends on.
func TestRawSettingMarshalPreservesUnmodeled(t *testing.T) {
	rs := settings.RawSetting{BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{"key": "mgmt", "x_ssh_enabled": true, "unmodeled": "keep"}}
	b, err := json.Marshal(&rs)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.Contains(s, `"unmodeled":"keep"`) || !strings.Contains(s, `"x_ssh_enabled":true`) {
		t.Fatalf("merged map not marshalled to wire: %s", s)
	}
}

// TestSettingClientAdapterUpdateSendsMergedPUTBody stands up an httptest
// server, constructs a real go-unifi ApiClient (API-key auth, so the login
// POST is skipped entirely) pointed at it, and drives an update through
// realSettingsClient. It asserts the captured HTTP request is a PUT to
// api/s/default/set/setting/mgmt whose decoded body carries both the modeled
// key field and the unmodeled data key — proving the full
// realSettingsClient -> UpdateSetting -> HTTP path, not just marshalling.
func TestSettingClientAdapterUpdateSendsMergedPUTBody(t *testing.T) {
	type captured struct {
		method string
		path   string
		body   map[string]any
	}
	var got captured

	// API-key auth prefixes every path with the "new style" apiPath
	// (/proxy/network), determined internally by the client rather than
	// passed in — so route by suffix instead of hardcoding that prefix.
	// ui.New also issues a best-effort bootstrap probe (setServerVersion,
	// via GET .../status, falling back to GET .../stat/sysinfo) even under
	// API-key auth; its error is ignored by New(), but the handler answers
	// it permissively anyway to keep the test's intent obvious.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/set/setting/mgmt") && r.Method == http.MethodPut:
			got.method = r.Method
			got.path = r.URL.Path
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding request body: %v", err)
			}
			got.body = body
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[` + mustJSON(t, body) + `]}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			_, _ = w.Write([]byte(`{"meta":{"server_version":"9.0.0","uuid":"test"}}`))
		case strings.HasSuffix(r.URL.Path, "/stat/sysinfo"):
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[{"version":"9.0.0"}]}`))
		default:
			// Permissive fallback for any other bootstrap request so ui.New
			// is never blocked by an unanticipated probe.
			_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	apiClient, err := ui.New(ctx, &ui.Config{
		BaseURL:       srv.URL,
		APIKey:        "test",
		AllowInsecure: true,
	})
	if err != nil {
		t.Fatalf("ui.New: %v", err)
	}
	client := &Client{ApiClient: apiClient, Site: "default"}

	r := realSettingsClient{c: client}
	rs := settings.RawSetting{
		BaseSetting: settings.BaseSetting{Key: "mgmt"},
		Data: map[string]any{
			"key":           "mgmt",
			"x_ssh_enabled": true,
			"unmodeled":     "keep",
		},
	}

	if err := r.UpdateRawSetting(ctx, "default", rs); err != nil {
		t.Fatalf("UpdateRawSetting: %v", err)
	}

	if got.method != http.MethodPut {
		t.Errorf("method = %q, want PUT", got.method)
	}
	if !strings.HasSuffix(got.path, "/set/setting/mgmt") {
		t.Errorf("path = %q, want suffix /set/setting/mgmt", got.path)
	}
	if got.body["key"] != "mgmt" {
		t.Errorf("body[key] = %v, want mgmt", got.body["key"])
	}
	if got.body["x_ssh_enabled"] != true {
		t.Errorf("body[x_ssh_enabled] = %v, want true", got.body["x_ssh_enabled"])
	}
	if got.body["unmodeled"] != "keep" {
		t.Errorf("body[unmodeled] = %v, want %q (unmodeled data must survive the PUT)", got.body["unmodeled"], "keep")
	}
}

// mustJSON marshals v for embedding into a handcrafted JSON response body in
// the adapter test above.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("mustJSON: %v", err)
	}
	return string(b)
}
