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

	baseURL := strings.TrimRight(endpoint, "/")

	// A UniFi OS console and a standalone Network controller expose different
	// login and API paths. Probe the way go-unifi does: "/" is served directly
	// (200) on UniFi OS and redirects to /manage (302) on a standalone
	// controller. Hardcoding either style gets a 401 from the other.
	loginPath := "/api/auth/login"
	migratePath := "/proxy/network/v2/api/site/" + site + "/firewall/migrate"
	probeClient := *client
	probeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	probeReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/", nil)
	if err != nil {
		return fmt.Errorf("create controller style probe: %w", err)
	}
	probeResp, err := probeClient.Do(probeReq)
	if err != nil {
		return fmt.Errorf("probe controller style: %w", err)
	}
	probeStatus := probeResp.StatusCode
	_, _ = io.Copy(io.Discard, probeResp.Body)
	_ = probeResp.Body.Close()
	switch {
	case probeStatus == http.StatusFound:
		loginPath = "/api/login"
		migratePath = "/v2/api/site/" + site + "/firewall/migrate"
	case probeStatus >= http.StatusOK && probeStatus < http.StatusMultipleChoices:
		// UniFi OS serves "/": keep the console paths.
	default:
		return fmt.Errorf("controller style probe returned HTTP %d", probeStatus)
	}

	loginBody, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		return fmt.Errorf("encode controller login: %w", err)
	}
	var loginResp *http.Response
	for attempt := 0; attempt < 8; attempt++ {
		loginReq, err := http.NewRequestWithContext(
			ctx,
			http.MethodPost,
			baseURL+loginPath,
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
		status := loginResp.StatusCode
		retryable := status == http.StatusUnauthorized || status == http.StatusTooManyRequests
		if !retryable || attempt == 7 {
			snippet := responseSnippet(loginResp)
			_ = loginResp.Body.Close()
			return fmt.Errorf("controller login at %s returned HTTP %d%s", loginPath, status, snippet)
		}
		_, _ = io.Copy(io.Discard, loginResp.Body)
		_ = loginResp.Body.Close()
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
		baseURL+migratePath,
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
	if migrateResp.StatusCode < http.StatusOK || migrateResp.StatusCode >= http.StatusMultipleChoices {
		snippet := responseSnippet(migrateResp)
		_ = migrateResp.Body.Close()
		return fmt.Errorf("zone-based firewall migration at %s returned HTTP %d%s",
			migratePath, migrateResp.StatusCode, snippet)
	}
	_, _ = io.Copy(io.Discard, migrateResp.Body)
	_ = migrateResp.Body.Close()
	return nil
}

// responseSnippet reads a short prefix of a response body so a failing
// controller call reports what the controller said, not just a bare status.
func responseSnippet(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	_, _ = io.Copy(io.Discard, resp.Body)
	trimmed := bytes.TrimSpace(body)
	if err != nil || len(trimmed) == 0 {
		return ""
	}
	return ": " + string(trimmed)
}
