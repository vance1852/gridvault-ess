package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/identity"
)

func TestSessionRevocationsDoNotShareTimestamp0017(t *testing.T) {
	firstAt := time.Date(2026, 8, 26, 16, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(time.Hour)
	first := (identity.Session{ID: "first"}).Revoke(firstAt)
	second := (identity.Session{ID: "second"}).Revoke(secondAt)
	if first.RevokedAt == nil || second.RevokedAt == nil {
		t.Fatal("revocation timestamp missing")
	}
	if !first.RevokedAt.Equal(firstAt) {
		t.Fatalf("first revocation changed after second session: %v", first.RevokedAt)
	}
}
