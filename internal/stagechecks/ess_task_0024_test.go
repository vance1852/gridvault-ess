package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/idempotency"
)

func TestReplayRemainsValidBeforeExactExpiry0024(t *testing.T) {
	now := time.Date(2026, 8, 26, 10, 10, 45, 0, time.UTC)
	request := []byte("dispatch")
	record, err := idempotency.Start("operator-24", "POST", "/dispatch", "expiry-key-0024", request, now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := record.Complete(201, []byte("created"), now)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := completed.Replay(request, completed.ExpiresAt.Add(-30*time.Second))
	if err != nil || string(body) != "created" {
		t.Fatalf("replay expired early: %v %q", err, body)
	}
}
