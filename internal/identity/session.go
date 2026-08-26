package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Session struct {
	ID         string
	UserID     string
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

type IssuedSession struct {
	Session Session
	Token   string
}

func IssueSession(userID string, now time.Time, ttl time.Duration) (IssuedSession, error) {
	if strings.TrimSpace(userID) == "" {
		return IssuedSession{}, fault.New(fault.Invalid, "missing_user", "session user is required")
	}
	if ttl < time.Minute || ttl > 30*24*time.Hour {
		return IssuedSession{}, fault.New(fault.Invalid, "invalid_session_ttl", "session lifetime is outside policy")
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return IssuedSession{}, fault.Wrap(fault.Internal, "token_generation_failed", "could not issue session", "identity.IssueSession", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	now = now.UTC()
	return IssuedSession{
		Session: Session{
			ID:         uuid.NewString(),
			UserID:     userID,
			TokenHash:  HashToken(token),
			ExpiresAt:  now.Add(ttl),
			LastSeenAt: now,
			CreatedAt:  now,
		},
		Token: token,
	}, nil
}

func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func SecureHashMatches(token, expectedHash string) bool {
	actual, err := hex.DecodeString(HashToken(token))
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(expectedHash)
	if err != nil || len(actual) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (s Session) Validate(now time.Time) error {
	if s.RevokedAt != nil {
		return fault.New(fault.Unauthenticated, "session_revoked", "session has been revoked")
	}
	if !now.UTC().Before(s.ExpiresAt) {
		return fault.New(fault.Unauthenticated, "session_expired", "session has expired")
	}
	return nil
}

func (s Session) Revoke(now time.Time) Session {
	copy := s
	if copy.RevokedAt == nil {
		at := now.UTC()
		copy.RevokedAt = &at
	}
	return copy
}

func (s Session) Touch(now time.Time, interval time.Duration) (Session, bool) {
	now = now.UTC()
	if now.Sub(s.LastSeenAt) < interval {
		return s, false
	}
	copy := s
	copy.LastSeenAt = now
	return copy, true
}
