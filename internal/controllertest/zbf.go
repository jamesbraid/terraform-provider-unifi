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
		// Cloning the default transport keeps the fixture's proxy, dial and
		// timeout behaviour. The assertion is the documented shape of
		// http.DefaultTransport, but a test that replaces it would otherwise
		// panic here rather than say so.
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return fmt.Errorf("http.DefaultTransport is %T, not *http.Transport", http.DefaultTransport)
		}
		transport := defaultTransport.Clone()
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
			{Name: "TOKEN", MaxAge: -1, Path: "/"},    // #nosec G124 -- expires a stale cookie; no credential is set
			{Name: "unifises", MaxAge: -1, Path: "/"}, // #nosec G124 -- expires a stale cookie; no credential is set
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
	// The body is the JSON literal null, matching go-unifi's proven migrate
	// call (a marshalled nil), not an empty body.
	migrateReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		baseURL+migratePath,
		strings.NewReader("null"),
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

	// A controller can answer the migration with 204 without migrating (the
	// no-op service documented in go-unifi's drift harness). Only the zone
	// collection itself proves the feature is on: migration seeds six default
	// zones synchronously, so an empty list means it did not take.
	zonePath := strings.Replace(migratePath, "/firewall/migrate", "/firewall/zone", 1)
	var lastZoneErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		zoneReq, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+zonePath, nil)
		if err != nil {
			return fmt.Errorf("create zone list request: %w", err)
		}
		zoneReq.Header.Set("Accept", "application/json")
		if csrf != "" {
			zoneReq.Header.Set("X-Csrf-Token", csrf)
		}
		zoneResp, err := client.Do(zoneReq)
		if err != nil {
			return fmt.Errorf("list firewall zones: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(zoneResp.Body, 1<<20))
		_ = zoneResp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read firewall zone list: %w", readErr)
		}
		if zoneResp.StatusCode != http.StatusOK {
			lastZoneErr = fmt.Errorf("zone list at %s returned HTTP %d after migration: %s",
				zonePath, zoneResp.StatusCode, bytes.TrimSpace(body))
			continue
		}
		var zones []json.RawMessage
		if err := json.Unmarshal(body, &zones); err != nil {
			return fmt.Errorf("decode firewall zone list: %w (body: %s)", err, bytes.TrimSpace(body))
		}
		if len(zones) > 0 {
			return nil
		}
		lastZoneErr = fmt.Errorf(
			"zone collection still empty after migration answered 2xx: the controller wired a no-op migration")
	}
	return lastZoneErr
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
