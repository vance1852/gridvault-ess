package service

import (
	"context"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/identity"
)

type AuditStore interface {
	ListAudit(context.Context, audit.Filter) (audit.Page, error)
}
type AuditService struct{ store AuditStore }

func NewAuditService(store AuditStore) *AuditService { return &AuditService{store: store} }
func (s *AuditService) List(ctx context.Context, principal Principal, filter audit.Filter) (audit.Page, error) {
	if err := principal.Require(identity.PermissionAuditRead); err != nil {
		return audit.Page{}, err
	}
	snapshot := audit.SnapshotFilter(filter)
	return s.store.ListAudit(ctx, snapshot)
}
