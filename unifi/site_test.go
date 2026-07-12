package unifi

import "testing"

func TestResolveSite(t *testing.T) {
	t.Run("configured non-empty returns configured", func(t *testing.T) {
		got := resolveSite("configured-site", "default-site")
		if got != "configured-site" {
			t.Errorf("resolveSite() = %q, want %q", got, "configured-site")
		}
	})

	t.Run("configured empty returns default", func(t *testing.T) {
		got := resolveSite("", "default-site")
		if got != "default-site" {
			t.Errorf("resolveSite() = %q, want %q", got, "default-site")
		}
	})
}

func TestParseSiteID(t *testing.T) {
	const def = "other-site"

	t.Run("site:id returns explicit site and id", func(t *testing.T) {
		site, id, err := parseSiteID("site:id", def)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if site != "site" || id != "id" {
			t.Errorf("parseSiteID() = (%q, %q), want (%q, %q)", site, id, "site", "id")
		}
	})

	t.Run("bare id uses default site", func(t *testing.T) {
		site, id, err := parseSiteID("id", def)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if site != def || id != "id" {
			t.Errorf("parseSiteID() = (%q, %q), want (%q, %q)", site, id, def, "id")
		}
	})

	t.Run("empty import ID is an error", func(t *testing.T) {
		_, _, err := parseSiteID("", def)
		if err == nil {
			t.Fatal("expected error for empty import ID, got nil")
		}
	})

	t.Run("empty id component (site:) is an error", func(t *testing.T) {
		site, id, err := parseSiteID("site:", def)
		if err == nil {
			t.Fatalf("expected error for empty id component, got nil (site=%q, id=%q)", site, id)
		}
	})

	t.Run("too many parts is an error", func(t *testing.T) {
		site, id, err := parseSiteID("a:b:c", def)
		if err == nil {
			t.Fatalf("expected error for too many parts, got nil (site=%q, id=%q)", site, id)
		}
	})

	t.Run("bare id and explicit-default-site id are equivalent", func(t *testing.T) {
		siteBare, idBare, errBare := parseSiteID("id", def)
		if errBare != nil {
			t.Fatalf("unexpected error (bare): %v", errBare)
		}

		siteExplicit, idExplicit, errExplicit := parseSiteID(def+":id", def)
		if errExplicit != nil {
			t.Fatalf("unexpected error (explicit): %v", errExplicit)
		}

		if siteBare != def || idBare != "id" {
			t.Errorf("bare form = (%q, %q), want (%q, %q)", siteBare, idBare, def, "id")
		}
		if siteExplicit != def || idExplicit != "id" {
			t.Errorf("explicit form = (%q, %q), want (%q, %q)", siteExplicit, idExplicit, def, "id")
		}
		if siteBare != siteExplicit || idBare != idExplicit {
			t.Errorf("bare and explicit forms not equivalent: (%q,%q) vs (%q,%q)",
				siteBare, idBare, siteExplicit, idExplicit)
		}
	})
}
