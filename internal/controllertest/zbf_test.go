package controllertest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrateZoneBasedFirewall(t *testing.T) {
	var migrated bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			// UniFi OS serves the console root directly.
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if body["username"] != "admin" || body["password"] != "secret" {
				t.Fatalf("login body = %#v", body)
			}
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: "session", Path: "/"})
			w.Header().Set("X-Csrf-Token", "csrf")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/migrate":
			if r.Header.Get("X-Csrf-Token") != "csrf" {
				t.Fatalf("migration csrf = %q", r.Header.Get("X-Csrf-Token"))
			}
			if _, err := r.Cookie("TOKEN"); err != nil {
				t.Fatalf("migration cookie: %v", err)
			}
			migrated = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/zone":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"_id":"z1","name":"Internal","zone_key":"internal"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := server.Client()

	if err := migrateZoneBasedFirewallWithClient(context.Background(), client, server.URL, "default", "admin", "secret"); err != nil {
		t.Fatalf("migrateZoneBasedFirewall: %v", err)
	}
	if !migrated {
		t.Fatal("migration endpoint was not called")
	}
}

func TestMigrateZoneBasedFirewall_StandaloneController(t *testing.T) {
	var migrated bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			// A standalone Network controller redirects the root to /manage.
			http.Redirect(w, r, "/manage", http.StatusFound)
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode login body: %v", err)
			}
			if body["username"] != "admin" || body["password"] != "secret" {
				t.Fatalf("login body = %#v", body)
			}
			http.SetCookie(w, &http.Cookie{Name: "unifises", Value: "session", Path: "/"})
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			t.Fatal("standalone controller must not see a UniFi OS login")
		case r.Method == http.MethodPost && r.URL.Path == "/v2/api/site/default/firewall/migrate":
			if _, err := r.Cookie("unifises"); err != nil {
				t.Fatalf("migration cookie: %v", err)
			}
			migrated = true
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/proxy/network/"):
			t.Fatalf("standalone controller must not see console path %s", r.URL.Path)
		case r.Method == http.MethodGet && r.URL.Path == "/v2/api/site/default/firewall/zone":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"_id":"z1","name":"Internal","zone_key":"internal"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	if err := migrateZoneBasedFirewallWithClient(
		context.Background(), server.Client(), server.URL, "default", "admin", "secret",
	); err != nil {
		t.Fatalf("migrateZoneBasedFirewall: %v", err)
	}
	if !migrated {
		t.Fatal("migration endpoint was not called")
	}
}

func TestMigrateZoneBasedFirewall_AllowsControllerCertificate(t *testing.T) {
	var migrated bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/migrate":
			migrated = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/zone":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"_id":"z1","name":"Internal","zone_key":"internal"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	if err := MigrateZoneBasedFirewall(
		context.Background(), server.URL, "default", "admin", "secret",
	); err != nil {
		t.Fatalf("migrateZoneBasedFirewall: %v", err)
	}
	if !migrated {
		t.Fatal("migration endpoint was not called")
	}
}

func TestMigrateZoneBasedFirewall_RetriesUnauthorizedLogin(t *testing.T) {
	loginAttempts := 0
	var migrated bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/auth/login":
			loginAttempts++
			if loginAttempts == 1 {
				http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: "stale", Path: "/"})
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if _, err := r.Cookie("TOKEN"); err == nil {
				t.Fatalf("retry sent stale TOKEN cookie")
			}
			http.SetCookie(w, &http.Cookie{Name: "TOKEN", Value: "session", Path: "/"})
			w.Header().Set("X-Csrf-Token", "csrf")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/migrate":
			migrated = true
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/proxy/network/v2/api/site/default/firewall/zone":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"_id":"z1","name":"Internal","zone_key":"internal"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	if err := migrateZoneBasedFirewallWithClient(
		context.Background(), server.Client(), server.URL, "default", "admin", "secret",
	); err != nil {
		t.Fatalf("migrateZoneBasedFirewall: %v", err)
	}
	if loginAttempts != 2 {
		t.Fatalf("login attempts = %d, want 2", loginAttempts)
	}
	if !migrated {
		t.Fatal("migration endpoint was not called")
	}
}

func TestMigrateZoneBasedFirewall_UnexpectedProbeStatus(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	err := migrateZoneBasedFirewallWithClient(
		context.Background(), server.Client(), server.URL, "default", "admin", "secret",
	)
	if err == nil || !strings.Contains(err.Error(), "controller style probe returned HTTP 404") {
		t.Fatalf("err = %v, want probe status error", err)
	}
}
