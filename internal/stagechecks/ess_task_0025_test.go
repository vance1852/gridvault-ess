package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/identity"
)

func TestSessionTouchRejectsClockRollback0025(t *testing.T) {
	lastSeen := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	session := identity.Session{ID: "session-25", LastSeenAt: lastSeen}
	changed, touched := session.Touch(lastSeen.Add(-time.Hour), 5*time.Minute)
	if touched || !changed.LastSeenAt.Equal(lastSeen) {
		t.Fatalf("clock rollback moved last_seen_at: %v %v", touched, changed.LastSeenAt)
	}
}
