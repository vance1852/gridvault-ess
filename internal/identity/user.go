package identity

import (
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/vance1852/gridvault-ess/internal/fault"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           string
	Email        string
	PasswordHash string
	DisplayName  string
	Role         Role
	Active       bool
	Version      int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type NewUser struct {
	Email       string
	Password    string
	DisplayName string
	Role        Role
}

func CreateUser(input NewUser, now time.Time) (User, error) {
	email, err := NormalizeEmail(input.Email)
	if err != nil {
		return User{}, err
	}
	name := strings.TrimSpace(input.DisplayName)
	if count := utf8.RuneCountInString(name); count < 2 || count > 80 {
		return User{}, fault.New(fault.Invalid, "invalid_display_name", "display name must contain 2 to 80 characters")
	}
	if !input.Role.Valid() {
		return User{}, fault.New(fault.Invalid, "invalid_role", "user role is not supported")
	}
	if err := ValidatePassword(input.Password); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fault.Wrap(fault.Internal, "password_hash_failed", "could not secure password", "identity.CreateUser", err)
	}
	now = now.UTC()
	return User{
		ID:           uuid.NewString(),
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  name,
		Role:         input.Role,
		Active:       true,
		Version:      1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func NormalizeEmail(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || len(normalized) > 254 {
		return "", fault.New(fault.Invalid, "invalid_email", "email address is invalid")
	}
	return normalized, nil
}

func ValidatePassword(password string) error {
	if len(password) < 12 || len(password) > 128 {
		return fault.New(fault.Invalid, "weak_password", "password must contain 12 to 128 characters")
	}
	var lower, upper, digit, symbol bool
	for _, value := range password {
		switch {
		case value >= 'a' && value <= 'z':
			lower = true
		case value >= 'A' && value <= 'Z':
			upper = true
		case value >= '0' && value <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	if !lower || !upper || !digit || !symbol {
		return fault.New(fault.Invalid, "weak_password", "password must include lower, upper, digit, and symbol characters")
	}
	return nil
}

func (u User) VerifyPassword(password string) error {
	if !u.Active {
		return fault.New(fault.Forbidden, "account_inactive", "account is inactive")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return fault.New(fault.Unauthenticated, "invalid_credentials", "email or password is incorrect")
	}
	return nil
}

func (u User) Can(permission Permission) error {
	if !u.Active {
		return fault.New(fault.Forbidden, "account_inactive", "account is inactive")
	}
	return Require(u.Role, permission)
}

func (u User) Deactivate(now time.Time) User {
	copy := u
	copy.Active = false
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy
}

func (u User) Rename(name string, now time.Time) (User, error) {
	name = strings.TrimSpace(name)
	if count := utf8.RuneCountInString(name); count < 2 || count > 80 {
		return User{}, fault.New(fault.Invalid, "invalid_display_name", "display name must contain 2 to 80 characters")
	}
	copy := u
	copy.DisplayName = name
	copy.Version++
	copy.UpdatedAt = now.UTC()
	return copy, nil
}
