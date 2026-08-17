package releasequalification

import (
	"strings"
	"testing"

	"github.com/ubiquiti-community/terraform-provider-unifi/internal/catalogparity"
)

const testDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// acceptableReceipt returns a receipt that VerifyMigrationRecoveryReceipt
// accepts, so each test below can break exactly one thing and see it named.
func acceptableReceipt() MigrationRecoveryReceipt {
	surfaces := []MigrationRecoverySurface{
		{
			SurfaceKey:    catalogparity.SurfaceKey{Kind: "managed_resource", Name: "unifi_dns_record"},
			State:         "migration_recovery_pass",
			Strategy:      "identity",
			EvidenceMode:  EvidenceDifferentialScenario,
			ReceiptSHA256: testDigest,
		},
		{
			SurfaceKey:    catalogparity.SurfaceKey{Kind: "managed_resource", Name: "unifi_user"},
			State:         "migration_recovery_pass",
			Strategy:      "identity",
			EvidenceMode:  EvidenceSourceIdentity,
			ReceiptSHA256: testDigest,
		},
	}
	return MigrationRecoveryReceipt{
		Result:        "pass",
		SurfaceCount:  len(surfaces),
		RecoveryCount: len(surfaces),
		StrategyCounts: map[string]int{
			"identity": len(surfaces),
		},
		EvidenceModes: map[string]int{
			EvidenceDifferentialScenario: 1,
			EvidenceSourceIdentity:       1,
		},
		Surfaces: surfaces,
	}
}

func TestVerifyMigrationRecoveryReceiptAcceptsAGoodReceipt(t *testing.T) {
	if err := VerifyMigrationRecoveryReceipt(acceptableReceipt()); err != nil {
		t.Fatalf("a well-formed receipt was rejected: %v", err)
	}
}

// TestVerifyRejectsAnUndeclaredMode is #152 itself, turned into a test.
//
// The campaign gate demanded dns_bidirectional_state and dns_list_controller
// long after both were deleted from the enum. Nothing connected the two,
// because the demand was a jq literal in YAML and the enum is Go. Here the
// question is asked of AllEvidenceModes, so a name that is not a mode is
// reported as one -- whichever direction the drift runs.
func TestVerifyRejectsAnUndeclaredMode(t *testing.T) {
	for _, deleted := range []string{"dns_bidirectional_state", "dns_list_controller"} {
		t.Run(deleted, func(t *testing.T) {
			receipt := acceptableReceipt()
			receipt.EvidenceModes = map[string]int{
				deleted:                1,
				EvidenceSourceIdentity: 1,
			}
			receipt.Surfaces[0].EvidenceMode = deleted

			err := VerifyMigrationRecoveryReceipt(receipt)
			if err == nil {
				t.Fatalf("a receipt naming the deleted mode %q was accepted", deleted)
			}
			if !strings.Contains(err.Error(), deleted) {
				t.Errorf("the error does not name the offending mode: %v", err)
			}
		})
	}
}

// TestDeletedModesAreNotDeclared guards the enum directly. If either name is
// ever reintroduced this fails, which is correct: reintroducing a mode is a
// decision that has to be made deliberately and its readers revisited.
func TestDeletedModesAreNotDeclared(t *testing.T) {
	for _, deleted := range []string{"dns_bidirectional_state", "dns_list_controller"} {
		if IsEvidenceMode(deleted) {
			t.Errorf("%q is declared again; evidence_mode.go explains why it was removed, and "+
				"anything reading AllEvidenceModes needs revisiting before it comes back", deleted)
		}
	}
}

// TestAllEvidenceModesMatchesTheConstants keeps the list honest. Deleting a
// constant already breaks compilation of AllEvidenceModes, which is the
// mechanism that matters; this catches the quieter direction, a list that has
// grown a duplicate or lost an entry while still compiling.
func TestAllEvidenceModesMatchesTheConstants(t *testing.T) {
	want := []string{EvidenceSourceIdentity, EvidenceDifferentialScenario, EvidencePragmaticReference}
	if len(AllEvidenceModes) != len(want) {
		t.Fatalf("AllEvidenceModes has %d entries, want %d: %v",
			len(AllEvidenceModes), len(want), AllEvidenceModes)
	}
	seen := map[string]bool{}
	for _, mode := range AllEvidenceModes {
		if seen[mode] {
			t.Errorf("AllEvidenceModes lists %q twice", mode)
		}
		seen[mode] = true
	}
	for _, mode := range want {
		if !seen[mode] {
			t.Errorf("AllEvidenceModes is missing %q", mode)
		}
		if !IsEvidenceMode(mode) {
			t.Errorf("IsEvidenceMode(%q) is false but the constant is declared", mode)
		}
	}
}

// TestVerifyRejectsASurfaceWithNoMode is the check the old literal was really
// making, expressed so it survives the census moving: every surface must be
// accounted for, whatever the distribution happens to be this run.
func TestVerifyRejectsASurfaceWithNoMode(t *testing.T) {
	receipt := acceptableReceipt()
	receipt.EvidenceModes = map[string]int{EvidenceSourceIdentity: 1}

	err := VerifyMigrationRecoveryReceipt(receipt)
	if err == nil {
		t.Fatal("a receipt accounting for only one of two surfaces was accepted")
	}
	if !strings.Contains(err.Error(), "carry no mode") {
		t.Errorf("the error should say a surface carries no mode: %v", err)
	}
}

func TestVerifyRejectsMalformedSurfaces(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		break_  func(*MigrationRecoveryReceipt)
		wantsub string
	}{
		{
			name:    "not a pass",
			break_:  func(r *MigrationRecoveryReceipt) { r.Result = "blocked_evidence" },
			wantsub: `result is "blocked_evidence"`,
		},
		{
			name:    "unrecovered surface",
			break_:  func(r *MigrationRecoveryReceipt) { r.Surfaces[1].State = "migration_recovery_blocked" },
			wantsub: "migration_recovery_blocked",
		},
		{
			name:    "truncated receipt digest",
			break_:  func(r *MigrationRecoveryReceipt) { r.Surfaces[0].ReceiptSHA256 = "abc" },
			wantsub: "not 64 hex characters",
		},
		{
			name:    "counts disagree with the surface list",
			break_:  func(r *MigrationRecoveryReceipt) { r.RecoveryCount = 1 },
			wantsub: "recovery_count",
		},
		{
			name: "summary map disagrees with the surfaces",
			break_: func(r *MigrationRecoveryReceipt) {
				r.EvidenceModes = map[string]int{EvidenceSourceIdentity: 2}
			},
			wantsub: "the surface list contains",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			receipt := acceptableReceipt()
			testCase.break_(&receipt)
			err := VerifyMigrationRecoveryReceipt(receipt)
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !strings.Contains(err.Error(), testCase.wantsub) {
				t.Errorf("error %q does not mention %q", err, testCase.wantsub)
			}
		})
	}
}
