package unifi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

type dnsRecordFieldUpdater interface {
	UpdateDNSRecordFields(
		context.Context,
		string,
		*ui.DNSRecord,
		...string,
	) (*ui.DNSRecord, error)
}

var _ dnsRecordFieldUpdater = (*ui.ApiClient)(nil)

func TestDNSRecordBackendDependency(t *testing.T) {
	t.Helper()
}

func TestPrivateDNSRecordBackendNormalizesLifecycle(t *testing.T) {
	const site = "default"
	want := dnsRecordModel{
		ID:         "record-1",
		Enabled:    true,
		Name:       "service.example.test",
		Port:       int64Pointer(8443),
		Priority:   10,
		RecordType: "SRV",
		TTL:        300,
		Value:      "target.example.test",
		Weight:     20,
	}
	var requests []string
	server := newDNSBackendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodGet:
			writeJSON(t, w, []dnsRecordModel{want})
		case http.MethodPost:
			writeJSON(t, w, want)
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	})
	backend := newPrivateDNSRecordBackend(newDNSBackendTestClient(t, server))

	created, err := backend.Create(context.Background(), site, dnsRecordIntent{
		Enabled:    want.Enabled,
		Name:       want.Name,
		Port:       want.Port,
		Priority:   want.Priority,
		RecordType: want.RecordType,
		TTL:        want.TTL,
		Value:      want.Value,
		Weight:     want.Weight,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !reflect.DeepEqual(created, want) {
		t.Fatalf("Create = %#v, want %#v", created, want)
	}

	read, err := backend.Read(context.Background(), site, want.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !reflect.DeepEqual(read, want) {
		t.Fatalf("Read = %#v, want %#v", read, want)
	}

	listed, err := backend.List(context.Background(), site)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !reflect.DeepEqual(listed, []dnsRecordModel{want}) {
		t.Fatalf("List = %#v, want %#v", listed, []dnsRecordModel{want})
	}

	if err := backend.Delete(context.Background(), site, want.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	wantRequests := []string{
		"POST /proxy/network/v2/api/site/default/static-dns",
		"GET /proxy/network/v2/api/site/default/static-dns",
		"GET /proxy/network/v2/api/site/default/static-dns",
		"DELETE /proxy/network/v2/api/site/default/static-dns/record-1",
	}
	if !reflect.DeepEqual(requests, wantRequests) {
		t.Fatalf("operation requests = %v, want %v", requests, wantRequests)
	}
}

func TestPrivateDNSRecordBackendWritesOnlyPatchFields(t *testing.T) {
	var body map[string]json.RawMessage
	server := newDNSBackendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		writeJSON(t, w, dnsRecordModel{
			ID:      "record-1",
			Enabled: false,
			Name:    "changed.example.test",
			Value:   "preserved.example.test",
		})
	})
	backend := newPrivateDNSRecordBackend(newDNSBackendTestClient(t, server))

	_, err := backend.Update(context.Background(), "default", dnsRecordPatch{
		ID: "record-1",
		Values: dnsRecordIntent{
			Enabled: false,
			Name:    "changed.example.test",
			Value:   "must-not-be-written.example.test",
		},
		Fields: []dnsRecordField{dnsRecordFieldEnabled, dnsRecordFieldName},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got, want := sortedJSONKeys(body), []string{"_id", "enabled", "key"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("request keys = %v, want %v", got, want)
	}
	if string(body["_id"]) != `"record-1"` || string(body["enabled"]) != "false" || string(body["key"]) != `"changed.example.test"` {
		t.Fatalf("request body = %s", mustMarshalJSON(t, body))
	}
}

func TestDNSRecordPatchRejectsUnsafeMasksBeforeRequest(t *testing.T) {
	requests := 0
	server := newDNSBackendTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	})
	backend := newPrivateDNSRecordBackend(newDNSBackendTestClient(t, server))

	for name, fields := range map[string][]dnsRecordField{
		"empty":     nil,
		"unknown":   {dnsRecordField("not_a_dns_field")},
		"duplicate": {dnsRecordFieldName, dnsRecordFieldName},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := backend.Update(context.Background(), "default", dnsRecordPatch{ID: "record-1", Fields: fields})
			if err == nil {
				t.Fatal("Update succeeded with an unsafe field mask")
			}
			if !strings.Contains(err.Error(), "DNS record patch") {
				t.Fatalf("Update error = %q, want DNS record patch context", err)
			}
		})
	}
	if requests != 0 {
		t.Fatalf("unsafe patches issued %d request(s), want 0", requests)
	}
}

func newDNSBackendTestServer(t *testing.T, operation http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/proxy/network/status" {
			writeJSON(t, w, map[string]any{"meta": map[string]any{"server_version": "10.4.57"}})
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/proxy/network/v2/api/site/default/static-dns") {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		operation(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func newDNSBackendTestClient(t *testing.T, server *httptest.Server) *ui.ApiClient {
	t.Helper()
	client, err := ui.New(context.Background(), &ui.Config{BaseURL: server.URL, APIKey: "test-key"})
	if err != nil {
		t.Fatalf("new API client: %v", err)
	}
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sortedJSONKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func int64Pointer(value int64) *int64 {
	return &value
}
