package service

import (
	"context"
	"strings"
	"time"

	"github.com/vance1852/gridvault-ess/internal/audit"
	"github.com/vance1852/gridvault-ess/internal/clock"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"github.com/vance1852/gridvault-ess/internal/identity"
)

type AuthStore interface {
	InsertUser(context.Context, identity.User) error
	UserByEmail(context.Context, string) (identity.User, error)
	UserByID(context.Context, string) (identity.User, error)
	InsertSession(context.Context, identity.Session) error
	SessionByHash(context.Context, string) (identity.Session, error)
	RevokeSession(context.Context, string, time.Time) error
	TouchSession(context.Context, string, time.Time) error
	DeleteExpiredSessions(context.Context, time.Time) (int64, error)
	InsertAudit(context.Context, audit.Event) error
}

type AuthService struct {
	store         AuthStore
	clock         clock.Clock
	ttl           time.Duration
	touchInterval time.Duration
}

func NewAuthService(store AuthStore, timer clock.Clock, ttl time.Duration) *AuthService {
	return &AuthService{store: store, clock: timer, ttl: ttl, touchInterval: 5 * time.Minute}
}

type LoginInput struct{ Email, Password, RequestID string }
type LoginResult struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      UserView  `json:"user"`
}
type UserView struct {
	ID, Email, DisplayName string
	Role                   identity.Role
	Permissions            []identity.Permission
}

func viewUser(user identity.User) UserView {
	return UserView{ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, Role: user.Role, Permissions: identity.Permissions(user.Role)}
}

func (s *AuthService) Bootstrap(ctx context.Context, input identity.NewUser) (identity.User, error) {
	email, err := identity.NormalizeEmail(input.Email)
	if err != nil {
		return identity.User{}, err
	}
	existing, err := s.store.UserByEmail(ctx, email)
	if err == nil {
		return existing, nil
	}
	if !fault.IsKind(err, fault.NotFound) {
		return identity.User{}, err
	}
	user, err := identity.CreateUser(input, s.clock.Now())
	if err != nil {
		return identity.User{}, err
	}
	if err = s.store.InsertUser(ctx, user); err != nil {
		return identity.User{}, err
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, input LoginInput) (LoginResult, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, fault.Wrap(fault.Unavailable, "request_cancelled", "request was cancelled", "service.Auth.Login", err)
	}
	email, err := identity.NormalizeEmail(input.Email)
	if err != nil {
		return LoginResult{}, fault.New(fault.Unauthenticated, "invalid_credentials", "email or password is incorrect")
	}
	user, err := s.store.UserByEmail(ctx, email)
	if err != nil {
		return LoginResult{}, fault.New(fault.Unauthenticated, "invalid_credentials", "email or password is incorrect")
	}
	if err = user.VerifyPassword(input.Password); err != nil {
		return LoginResult{}, err
	}
	issued, err := identity.IssueSession(user.ID, s.clock.Now(), s.ttl)
	if err != nil {
		return LoginResult{}, err
	}
	if err = s.store.InsertSession(ctx, issued.Session); err != nil {
		return LoginResult{}, err
	}
	event, _ := audit.NewEvent(user.ID, requestID(input.RequestID), "session", issued.Session.ID, "login", "success", map[string]any{"expires_at": issued.Session.ExpiresAt}, s.clock.Now())
	if err = s.store.InsertAudit(ctx, event); err != nil {
		return LoginResult{}, fault.Wrap(fault.Internal, "audit_failed", "login could not be completed", "service.Auth.Login", err)
	}
	return LoginResult{Token: issued.Token, ExpiresAt: issued.Session.ExpiresAt, User: viewUser(user)}, nil
}

type Principal struct {
	User    identity.User
	Session identity.Session
}

func (p Principal) Require(permission identity.Permission) error { return p.User.Can(permission) }

func (s *AuthService) Authenticate(ctx context.Context, token string) (Principal, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Principal{}, fault.ErrUnauthorized
	}
	session, err := s.store.SessionByHash(ctx, identity.HashToken(token))
	if err != nil {
		return Principal{}, fault.ErrUnauthorized
	}
	now := s.clock.Now()
	if err = session.Validate(now); err != nil {
		return Principal{}, err
	}
	user, err := s.store.UserByID(ctx, session.UserID)
	if err != nil {
		return Principal{}, fault.ErrUnauthorized
	}
	if !user.Active {
		return Principal{}, fault.New(fault.Forbidden, "account_inactive", "account is inactive")
	}
	if touched, ok := session.Touch(now, s.touchInterval); ok {
		if err = s.store.TouchSession(ctx, touched.ID, touched.LastSeenAt); err != nil {
			return Principal{}, err
		}
		session = touched
	}
	return Principal{User: user, Session: session}, nil
}

func (s *AuthService) Logout(ctx context.Context, principal Principal, requestIDValue string) error {
	now := s.clock.Now()
	if err := s.store.RevokeSession(ctx, principal.Session.ID, now); err != nil {
		return err
	}
	event, _ := audit.NewEvent(principal.User.ID, requestID(requestIDValue), "session", principal.Session.ID, "logout", "success", nil, now)
	return s.store.InsertAudit(ctx, event)
}
func (s *AuthService) Cleanup(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredSessions(ctx, s.clock.Now())
}
func requestID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "system"
	}
	return value
}
