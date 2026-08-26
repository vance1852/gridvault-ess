package idempotency

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type State string

const (
	Started   State = "started"
	Completed State = "completed"
)

type Record struct {
	ID, ActorID, Method, Path, Key, RequestHash string
	ResponseStatus                              int
	ResponseBody                                []byte
	State                                       State
	ExpiresAt, CreatedAt, UpdatedAt             time.Time
}

func Start(actorID, method, path, key string, request []byte, now time.Time, ttl time.Duration) (Record, error) {
	actorID = strings.TrimSpace(actorID)
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	key = strings.TrimSpace(key)
	if actorID == "" || method == "" || path == "" || key == "" {
		return Record{}, fault.New(fault.Invalid, "invalid_idempotency_scope", "actor, method, path, and key are required")
	}
	if len(key) < 8 || len(key) > 128 {
		return Record{}, fault.New(fault.Invalid, "invalid_idempotency_key", "idempotency key must contain 8 to 128 characters")
	}
	if ttl < time.Minute || ttl > 7*24*time.Hour {
		return Record{}, fault.New(fault.Invalid, "invalid_idempotency_ttl", "idempotency lifetime is outside policy")
	}
	digest := sha256.Sum256(request)
	now = now.UTC()
	return Record{ID: uuid.NewString(), ActorID: actorID, Method: method, Path: path, Key: key, RequestHash: hex.EncodeToString(digest[:]), State: Started, ExpiresAt: now.Add(ttl), CreatedAt: now, UpdatedAt: now}, nil
}
func (r Record) Matches(request []byte) bool {
	digest := sha256.Sum256(request)
	return r.RequestHash == hex.EncodeToString(digest[:])
}
func (r Record) Complete(status int, body []byte, now time.Time) (Record, error) {
	if r.State != Started {
		return Record{}, fault.New(fault.Conflict, "idempotency_completed", "idempotency record is already complete")
	}
	if status < 100 || status > 599 {
		return Record{}, fault.New(fault.Invalid, "invalid_response_status", "response status is invalid")
	}
	if len(body) > 1<<20 {
		return Record{}, fault.New(fault.Invalid, "response_too_large", "idempotent response exceeds one MiB")
	}
	copy := r
	copy.State = Completed
	copy.ResponseStatus = status
	copy.ResponseBody = append([]byte(nil), body...)
	copy.UpdatedAt = now.UTC()
	return copy, nil
}
func (r Record) Replay(request []byte, now time.Time) (int, []byte, error) {
	if !now.UTC().Before(r.ExpiresAt) {
		return 0, nil, fault.New(fault.Conflict, "idempotency_expired", "idempotency record expired")
	}
	if !r.Matches(request) {
		return 0, nil, fault.New(fault.Conflict, "idempotency_payload_mismatch", "idempotency key was used with a different request")
	}
	if r.State != Completed {
		return 0, nil, fault.New(fault.Conflict, "idempotency_in_progress", "request with this idempotency key is still in progress")
	}
	return r.ResponseStatus, append([]byte(nil), r.ResponseBody...), nil
}
