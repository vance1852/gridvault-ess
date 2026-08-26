package stagechecks_test

import (
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/idempotency"
)

func TestCompletedResponseOwnsItsBytes0015(t *testing.T) {
	now := time.Date(2026, 8, 26, 15, 0, 0, 0, time.UTC)
	request := []byte("create-plan")
	record, err := idempotency.Start("operator-15", "POST", "/plans", "stable-key-0015", request, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"state":"accepted"}`)
	completed, err := record.Complete(202, body, now)
	if err != nil {
		t.Fatal(err)
	}
	copy(body, []byte(`{"state":"corrupt"}`))
	status, replayed, err := completed.Replay(request, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if status != 202 || string(replayed) != `{"state":"accepted"}` {
		t.Fatalf("stored response aliased caller buffer: %d %s", status, replayed)
	}
}
