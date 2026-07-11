package unifi

import "testing"

func TestOwnershipClassPolicy(t *testing.T) {
	cases := []struct {
		c                          ownershipClass
		writes, reads, secret, sfu bool
	}{
		{ownerManaged, true, true, false, false},
		{ownerCoManaged, true, true, false, true},
		{ownerComputed, false, true, false, true},
		{ownerWriteOnlySecret, true, false, true, false},
		{ownerGeneratedSecret, false, true, true, true},
		{ownerPreservedUnmanaged, false, false, false, false},
	}
	for _, tc := range cases {
		if tc.c.writesToPUT() != tc.writes || tc.c.readsFromAPI() != tc.reads ||
			tc.c.isSecret() != tc.secret || tc.c.usesStateForUnknown() != tc.sfu {
			t.Errorf("class %d policy mismatch", tc.c)
		}
	}
}
