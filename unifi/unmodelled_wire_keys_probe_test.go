package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	ui "github.com/ubiquiti-community/go-unifi/unifi"
)

// TestWhatTheControllerSendsThatTheSDKCannotHold reads the controller's
// documents as raw maps and compares their keys against the union of every
// json tag the go-unifi package declares, since decoding through the SDK
// would only report what it already knows to keep.
//
// The union is deliberately generous: a key modelled by SOME struct is not
// reported here even if it belongs on another type, since that's a mapping
// question rather than a "no struct can hold it" one.
func TestWhatTheControllerSendsThatTheSDKCannotHold(t *testing.T) {
	base, raw := rawWireSession(t)
	modelled := everyModelledKey()
	if len(modelled) < 500 {
		t.Fatalf("only %d json tags found across the SDK; the reflection sweep is not "+
			"reaching the package and every key below would read as unmodelled", len(modelled))
	}

	// _id is on every document and struct; if it comes out unmodelled here,
	// the comparison itself is broken.
	if !modelled["_id"] {
		t.Fatal("_id is not in the modelled set, so the sweep is not reading struct tags")
	}

	collections := []struct{ label, path string }{
		{"networkconf", "/api/s/default/rest/networkconf"},
		{"wlanconf", "/api/s/default/rest/wlanconf"},
		{"portconf", "/api/s/default/rest/portconf"},
		{"firewallgroup", "/api/s/default/rest/firewallgroup"},
		{"firewallrule", "/api/s/default/rest/firewallrule"},
		{"routing", "/api/s/default/rest/routing"},
		{"portforward", "/api/s/default/rest/portforward"},
		{"radiusprofile", "/api/s/default/rest/radiusprofile"},
		{"account", "/api/s/default/rest/account"},
		{"usergroup", "/api/s/default/rest/usergroup"},
		//nolint:misspell // dynamicdns is the controller's own endpoint name
		{"dynamicdns", "/api/s/default/rest/dynamicdns"},
		// stat/device is a telemetry endpoint (uptime, radio stats), not
		// configuration; kept separate so its unmodelled keys don't bury the
		// ones that matter.
		{"device (telemetry)", "/api/s/default/stat/device"},
		{"firewall-zone", "/v2/api/site/default/firewall/zone"},
		{"firewall-policy", "/v2/api/site/default/firewall-policies"},
		{"static-dns", "/v2/api/site/default/static-dns"},
	}

	total := map[string]map[string]bool{}
	measured := 0
	for _, collection := range collections {
		documents, ok := rawDocuments(t, base, raw, collection.path)
		if !ok {
			t.Logf("  %-16s unreadable, skipped", collection.label)
			continue
		}
		if len(documents) == 0 {
			t.Logf("  %-16s no documents on this controller, so it measures nothing",
				collection.label)
			continue
		}
		measured++
		unmodelled := map[string]bool{}
		for _, document := range documents {
			for key := range document {
				if !modelled[key] {
					unmodelled[key] = true
				}
			}
		}
		names := make([]string, 0, len(unmodelled))
		for name := range unmodelled {
			names = append(names, name)
			if total[name] == nil {
				total[name] = map[string]bool{}
			}
			total[name][collection.label] = true
		}
		sort.Strings(names)
		t.Logf("  %-16s %2d document(s), %2d key(s) no struct can hold: %v",
			collection.label, len(documents), len(names), names)
	}

	// Without this the sweep reports a clean result by having read nothing.
	if measured == 0 {
		t.Fatal("no collection returned a document, so this proves nothing about the SDK")
	}
	names := make([]string, 0, len(total))
	for name := range total {
		names = append(names, name)
	}
	sort.Strings(names)
	t.Logf("ACROSS %d COLLECTION(S): %d key(s) the controller sends and no SDK struct "+
		"declares", measured, len(names))
	for _, name := range names {
		where := make([]string, 0, len(total[name]))
		for label := range total[name] {
			where = append(where, label)
		}
		sort.Strings(where)
		t.Logf("    %-40s %v", name, where)
	}
}

// everyModelledKey is the union of every json tag the SDK package declares.
func everyModelledKey() map[string]bool {
	out := map[string]bool{}
	// Reflection over one value per document type reaches the tags without
	// parsing the module source, which a vendored-path regex would have to.
	for _, object := range []any{
		ui.Network{},
		ui.WLAN{},
		ui.PortProfile{},
		ui.FirewallGroup{},
		ui.FirewallRule{},
		ui.Routing{},
		ui.PortForward{},
		ui.RADIUSProfile{},
		ui.Account{},
		ui.DynamicDNS{},
		ui.Device{},
		ui.FirewallZone{},
		ui.FirewallPolicy{},
		ui.DNSRecord{},
		ui.Client{},
		ui.APGroup{},
		ui.WireGuardPeer{},
	} {
		collectKeys(reflect.TypeOf(object), out, map[reflect.Type]bool{})
	}
	return out
}

func collectKeys(typ reflect.Type, into map[string]bool, seen map[reflect.Type]bool) {
	for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := range typ.NumField() {
		field := typ.Field(i)
		if tag, ok := field.Tag.Lookup("json"); ok && tag != "-" {
			if name := strings.Split(tag, ",")[0]; name != "" {
				into[name] = true
			}
		}
		collectKeys(field.Type, into, seen)
	}
}

func rawWireSession(t *testing.T) (string, *http.Client) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance only")
	}
	base := os.Getenv("UNIFI_API")
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:       jar,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec // the test controller uses a self-signed certificate
	}
	encoded, err := json.Marshal(map[string]string{
		"username": os.Getenv("UNIFI_USERNAME"),
		"password": os.Getenv("UNIFI_PASSWORD"),
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(),
		http.MethodPost, base+"/api/login", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("POST /api/login: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Skipf("login returned %d; this probe needs the old-style /api/login", response.StatusCode)
	}
	return base, client
}

func rawDocuments(t *testing.T, base string, client *http.Client, path string) ([]map[string]any, bool) {
	t.Helper()
	response, err := client.Get(base + path)
	if err != nil {
		return nil, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, false
	}
	// v1 REST endpoints wrap their list in {"data": [...]}; v2 endpoints
	// return a bare array.
	var wrapped struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil && wrapped.Data != nil {
		return wrapped.Data, true
	}
	var bare []map[string]any
	if err := json.Unmarshal(body, &bare); err == nil {
		return bare, true
	}
	return nil, false
}
