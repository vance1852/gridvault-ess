package stagechecks_test

import (
	"context"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/identity"
	"github.com/vance1852/gridvault-ess/internal/service"
)

type delayedAuditStore28 struct {
	entered chan struct{}
	release chan struct{}
	seen    audit.Cursor
}

func (s *delayedAuditStore28) ListAudit(_ context.Context, filter audit.Filter) (audit.Page, error) {
	close(s.entered)
	<-s.release
	s.seen = *filter.Cursor
	return audit.Page{}, nil
}

func TestAuditListOwnsCursorSnapshot0028(t *testing.T) {
	originalTime := time.Date(2026, 8, 26, 18, 0, 0, 0, time.UTC)
	cursor := &audit.Cursor{OccurredAt: originalTime, ID: "cursor-original"}
	store := &delayedAuditStore28{entered: make(chan struct{}), release: make(chan struct{})}
	svc := service.NewAuditService(store)
	principal := service.Principal{User: identity.User{ID: "auditor-28", Role: identity.RoleAuditor, Active: true}}
	done := make(chan error, 1)
	go func() {
		_, err := svc.List(context.Background(), principal, audit.Filter{Cursor: cursor, Limit: 20})
		done <- err
	}()
	<-store.entered
	cursor.ID = "cursor-mutated"
	cursor.OccurredAt = originalTime.Add(-time.Hour)
	close(store.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if store.seen.ID != "cursor-original" || !store.seen.OccurredAt.Equal(originalTime) {
		t.Fatalf("queued audit cursor changed: %+v", store.seen)
	}
}
