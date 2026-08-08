package controllertest

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"time"
)

// MigrateZoneBasedFirewall enables the controller feature required before the
// firewall/zone collection becomes writable. It deliberately lives in the
// acceptance fixture rather than the provider runtime: migration is disposable
// test setup, not an implicit provider mutation.
func MigrateZoneBasedFirewall(
	ctx context.Context,
	endpoint, site, username, password string,
) error {
	return migrateZoneBasedFirewallWithClient(ctx, nil, endpoint, site, username, password)
}

func migrateZoneBasedFirewallWithClient(
	ctx context.Context,
	client *http.Client,
	endpoint, site, username, password string,
) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return fmt.Errorf("create controller cookie jar: %w", err)
	}
	if client == nil {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- controller fixtures use self-signed certificates.
		client = &http.Client{Transport: transport}
	}
	transportClient := *client
	transportClient.Jar = jar
	client = &transportClient

	loginBody, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return fmt.Errorf("encode controller login: %w", err)
	}
	baseURL := strings.TrimRight(endpoint, "/")
	var loginResp *http.Response
	for attempt := 0; attempt < 8; attempt++ {
		loginReq, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			baseURL+"/api/auth/login",
			bytes.NewReader(loginBody),
		)
		if err != nil {
			return fmt.Errorf("create controller login request: %w", err)
		}
		// UniFi can set a stale session cookie on a rejected login. Clear it
		// before retrying, matching go-unifi's login path.
		jar.SetCookies(loginReq.URL, []*http.Cookie{
			{Name: "TOKEN", MaxAge: -1, Path: "/"},
			{Name: "unifises", MaxAge: -1, Path: "/"},
		})
		loginReq.Header.Set("Accept", "application/json")
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp, err = client.Do(loginReq)
		if err != nil {
			return fmt.Errorf("login to controller: %w", err)
		}
		if loginResp.StatusCode >= http.StatusOK && loginResp.StatusCode < http.StatusMultipleChoices {
			break
		}
		_, _ = io.Copy(io.Discard, loginResp.Body)
		_ = loginResp.Body.Close()
		if (loginResp.StatusCode != http.StatusUnauthorized &&
			loginResp.StatusCode != http.StatusTooManyRequests) || attempt == 7 {
			return fmt.Errorf("controller login returned HTTP %d", loginResp.StatusCode)
		}
		wait := time.Second << attempt
		if wait > 30*time.Second {
			wait = 30 * time.Second
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	_, _ = io.Copy(io.Discard, loginResp.Body)
	_ = loginResp.Body.Close()

	csrf := loginResp.Header.Get("X-Updated-Csrf-Token")
	if csrf == "" {
		csrf = loginResp.Header.Get("X-Csrf-Token")
	}
	migrateReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+"/proxy/network/v2/api/site/"+site+"/firewall/migrate",
		nil,
	)
	if err != nil {
		return fmt.Errorf("create zone migration request: %w", err)
	}
	migrateReq.Header.Set("Accept", "application/json")
	migrateReq.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		migrateReq.Header.Set("X-Csrf-Token", csrf)
	}
	migrateResp, err := client.Do(migrateReq)
	if err != nil {
		return fmt.Errorf("migrate zone-based firewall: %w", err)
	}
	_, _ = io.Copy(io.Discard, migrateResp.Body)
	_ = migrateResp.Body.Close()
	if migrateResp.StatusCode < http.StatusOK || migrateResp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("zone-based firewall migration returned HTTP %d", migrateResp.StatusCode)
	}
	return nil
}
