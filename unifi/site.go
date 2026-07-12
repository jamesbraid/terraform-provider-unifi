package unifi

import (
	"fmt"
	"strings"

	"github.com/ubiquiti-community/terraform-provider-unifi/unifi/util"
)

// resolveSite returns the effective site: the configured value when non-empty,
// otherwise the provider default. Codifies the pattern used throughout the
// provider (e.g. setting_resource.go: `site := data.Site.ValueString(); if
// site == "" { site = r.client.Site }`).
func resolveSite(configured, def string) string {
	if configured == "" {
		return def
	}
	return configured
}

// parseSiteID splits a composite import ID of the form "id" or "site:id" into
// its site and id components. "id" (no site prefix) resolves to the provider
// default site; "site:id" uses the explicit site. An empty id — whether from
// an empty importID or an empty component after the ":" (e.g. "site:") — is
// always an error, never a silent default.
//
// This is shared infra for the composite "<site>:<id>" imports used by later
// PRs (NAT, content-filtering); it is not used by the settings resource's own
// import, which takes a bare site name.
//
// Delegates the ":" splitting to util.ParseImportID and layers the
// default-site fallback and empty-id validation on top.
func parseSiteID(importID, def string) (site, id string, err error) {
	m, diags := util.ParseImportID(importID, 1, 2)
	if diags.HasError() {
		var msgs []string
		for _, d := range diags {
			msgs = append(msgs, fmt.Sprintf("%s: %s", d.Summary(), d.Detail()))
		}
		return "", "", fmt.Errorf("invalid import ID %q: %s", importID, strings.Join(msgs, "; "))
	}

	id = m["id"]
	if id == "" {
		return "", "", fmt.Errorf("invalid import ID %q: empty object id", importID)
	}
	// util.ParseImportID uses strings.SplitN(id, ":", maxParts): with maxParts=2
	// this bounds the *number of parts returned*, not the number of colons
	// accepted — any colons beyond the first are absorbed into the id part
	// (e.g. "a:b:c" -> site="a", id="b:c"), so it never itself reports "too
	// many parts". Reject that case explicitly rather than silently treating
	// a malformed multi-colon ID as a valid (if surprising) object id.
	if strings.Contains(id, ":") {
		return "", "", fmt.Errorf("invalid import ID %q: too many parts, expected \"id\" or \"site:id\"", importID)
	}

	site = m["site"]
	if site == "" {
		site = def
	}

	return site, id, nil
}
