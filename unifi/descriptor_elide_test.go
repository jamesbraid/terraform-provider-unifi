package unifi

import (
	"context"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/generated/resource_dns_record"
	"github.com/ubiquiti-community/terraform-provider-unifi/internal/resourcekit"
)

// Every descriptor's Elide values must agree with its generated schema.
//
// This exists because the value was unverified: flipping all seven of
// dns_record's left the whole package green, so a descriptor generated across
// every surface could carry the wrong value everywhere and nothing would say
// so. resourcekit's own tests prove the check goes red on that mutation; this
// one applies it to the descriptors we actually ship.
func TestEveryDescriptorElideAgreesWithItsSchema(t *testing.T) {
	ctx := context.Background()

	checks := map[string]func(*testing.T){
		"dns_record": func(t *testing.T) {
			problems := resourcekit.ElideProblems(
				dnsRecordKitSpec(), resource_dns_record.DnsRecordResourceSchema(ctx))
			for _, problem := range problems {
				t.Error(problem)
			}
		},
	}

	// A table that shrank to nothing would pass this test while checking no
	// descriptor at all, which is the failure mode the check exists to remove.
	if len(checks) == 0 {
		t.Fatal("no descriptors are being checked; this test cannot fail and is worthless")
	}
	for name, check := range checks {
		t.Run(name, check)
	}
}
