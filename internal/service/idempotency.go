package service

import (
	"context"
	"errors"
	"time"

	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/idempotency"
)

type IdempotencyStore interface {
	InsertIdempotency(context.Context, idempotency.Record) error
	IdempotencyByScope(context.Context, string, string, string, string) (idempotency.Record, error)
	CompleteIdempotency(context.Context, idempotency.Record) error
}

type IdempotencyService struct {
	store IdempotencyStore
	clock clock.Clock
	ttl   time.Duration
}

func NewIdempotencyService(store IdempotencyStore, timer clock.Clock, ttl time.Duration) *IdempotencyService {
	return &IdempotencyService{store: store, clock: timer, ttl: ttl}
}

type IdempotencyDecision struct {
	Record idempotency.Record
	Replay bool
	Status int
	Body   []byte
}

func (s *IdempotencyService) Begin(ctx context.Context, principal Principal, method, path, key string, request []byte) (IdempotencyDecision, error) {
	if err := ctx.Err(); err != nil {
		return IdempotencyDecision{}, fault.Wrap(fault.Unavailable, "request_cancelled", "request was cancelled", "service.IdempotencyService.Begin", err)
	}
	existing, err := s.store.IdempotencyByScope(ctx, principal.User.ID, method, path, key)
	if err == nil {
		status, body, replayErr := existing.Replay(request, s.clock.Now())
		if replayErr != nil {
			return IdempotencyDecision{}, replayErr
		}
		return IdempotencyDecision{Record: existing, Replay: true, Status: status, Body: body}, nil
	}
	if !fault.IsKind(err, fault.NotFound) {
		return IdempotencyDecision{}, err
	}
	record, err := idempotency.Start(principal.User.ID, method, path, key, request, s.clock.Now(), s.ttl)
	if err != nil {
		return IdempotencyDecision{}, err
	}
	if err := s.store.InsertIdempotency(ctx, record); err != nil {
		if fault.IsKind(err, fault.Conflict) {
			return s.Begin(ctx, principal, method, path, key, request)
		}
		return IdempotencyDecision{}, err
	}
	return IdempotencyDecision{Record: record}, nil
}
func (s *IdempotencyService) Complete(ctx context.Context, record idempotency.Record, status int, body []byte) error {
	completed, err := record.Complete(status, body, s.clock.Now())
	if err != nil {
		return err
	}
	return s.store.CompleteIdempotency(ctx, completed)
}

var _ = errors.Is
