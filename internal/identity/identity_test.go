package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

var identityNow = time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)

func TestCreateUserValidatesAndHashesCredentials(t *testing.T) {
	user, err := CreateUser(NewUser{Email: " Operator@Example.COM ", Password: "Strong!Pass123", DisplayName: "Storage Operator", Role: RoleOperator}, identityNow)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.ID == "" {
		t.Fatal("ID was not generated")
	}
	if user.Email != "operator@example.com" {
		t.Fatalf("email=%q", user.Email)
	}
	if user.DisplayName != "Storage Operator" {
		t.Fatalf("name=%q", user.DisplayName)
	}
	if user.Role != RoleOperator {
		t.Fatalf("role=%q", user.Role)
	}
	if !user.Active {
		t.Fatal("new user is inactive")
	}
	if user.Version != 1 {
		t.Fatalf("version=%d", user.Version)
	}
	if user.PasswordHash == "Strong!Pass123" {
		t.Fatal("password was stored as plaintext")
	}
	if err := user.VerifyPassword("Strong!Pass123"); err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if err := user.VerifyPassword("wrong password"); !fault.IsKind(err, fault.Unauthenticated) {
		t.Fatalf("wrong password error=%v", err)
	}
}

func TestCreateUserRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input NewUser
		code  string
	}{
		{"bad email", NewUser{Email: "broken", Password: "Strong!Pass123", DisplayName: "Valid Name", Role: RoleOperator}, "invalid_email"},
		{"short name", NewUser{Email: "a@example.com", Password: "Strong!Pass123", DisplayName: "A", Role: RoleOperator}, "invalid_display_name"},
		{"unsupported role", NewUser{Email: "a@example.com", Password: "Strong!Pass123", DisplayName: "Valid Name", Role: "owner"}, "invalid_role"},
		{"short password", NewUser{Email: "a@example.com", Password: "A!1short", DisplayName: "Valid Name", Role: RoleOperator}, "weak_password"},
		{"no uppercase", NewUser{Email: "a@example.com", Password: "lower!case123", DisplayName: "Valid Name", Role: RoleOperator}, "weak_password"},
		{"no lowercase", NewUser{Email: "a@example.com", Password: "UPPER!CASE123", DisplayName: "Valid Name", Role: RoleOperator}, "weak_password"},
		{"no digit", NewUser{Email: "a@example.com", Password: "Strong!Password", DisplayName: "Valid Name", Role: RoleOperator}, "weak_password"},
		{"no symbol", NewUser{Email: "a@example.com", Password: "StrongPassword123", DisplayName: "Valid Name", Role: RoleOperator}, "weak_password"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CreateUser(tt.input, identityNow)
			if fault.Code(err) != tt.code {
				t.Fatalf("code=%q err=%v", fault.Code(err), err)
			}
		})
	}
}

func TestRolePermissionsAreBusinessSpecific(t *testing.T) {
	if err := Require(RoleDispatcher, PermissionPlanWrite); err != nil {
		t.Fatalf("dispatcher write: %v", err)
	}
	if err := Require(RoleDispatcher, PermissionPlanApprove); !fault.IsKind(err, fault.Forbidden) {
		t.Fatalf("dispatcher approve=%v", err)
	}
	if err := Require(RoleOperator, PermissionPlanApprove); err != nil {
		t.Fatalf("operator approve: %v", err)
	}
	if err := Require(RoleOperator, PermissionSettlementClose); err != nil {
		t.Fatalf("operator close: %v", err)
	}
	if err := Require(RoleAuditor, PermissionAuditRead); err != nil {
		t.Fatalf("auditor read: %v", err)
	}
	if err := Require(RoleAuditor, PermissionPlanWrite); !fault.IsKind(err, fault.Forbidden) {
		t.Fatalf("auditor write=%v", err)
	}
	permissions := Permissions(RoleOperator)
	permissions[0] = Permission("mutated")
	if Permissions(RoleOperator)[0] == Permission("mutated") {
		t.Fatal("permission slice leaked shared storage")
	}
}

func TestUserLifecyclePreservesVersions(t *testing.T) {
	user, _ := CreateUser(NewUser{Email: "user@example.com", Password: "Strong!Pass123", DisplayName: "Initial Name", Role: RoleDispatcher}, identityNow)
	renamed, err := user.Rename("Updated Name", identityNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if renamed.DisplayName != "Updated Name" {
		t.Fatalf("name=%q", renamed.DisplayName)
	}
	if renamed.Version != user.Version+1 {
		t.Fatalf("version=%d", renamed.Version)
	}
	if user.DisplayName != "Initial Name" {
		t.Fatal("Rename mutated original")
	}
	deactivated := renamed.Deactivate(identityNow.Add(2 * time.Minute))
	if deactivated.Active {
		t.Fatal("user remains active")
	}
	if deactivated.Version != renamed.Version+1 {
		t.Fatalf("version=%d", deactivated.Version)
	}
	if err := deactivated.VerifyPassword("Strong!Pass123"); !fault.IsKind(err, fault.Forbidden) {
		t.Fatalf("inactive verification=%v", err)
	}
}

func TestIssueSessionLifecycle(t *testing.T) {
	issued, err := IssueSession("user-1", identityNow, 2*time.Hour)
	if err != nil {
		t.Fatalf("IssueSession: %v", err)
	}
	if len(issued.Token) < 40 {
		t.Fatalf("token too short: %d", len(issued.Token))
	}
	if strings.Contains(issued.Session.TokenHash, issued.Token) {
		t.Fatal("hash contains raw token")
	}
	if !SecureHashMatches(issued.Token, issued.Session.TokenHash) {
		t.Fatal("token does not match hash")
	}
	if SecureHashMatches("wrong", issued.Session.TokenHash) {
		t.Fatal("wrong token matched")
	}
	if err := issued.Session.Validate(identityNow.Add(time.Hour)); err != nil {
		t.Fatalf("valid session: %v", err)
	}
	if err := issued.Session.Validate(identityNow.Add(2 * time.Hour)); fault.Code(err) != "session_expired" {
		t.Fatalf("expiry=%v", err)
	}
	revoked := issued.Session.Revoke(identityNow.Add(time.Minute))
	if revoked.RevokedAt == nil {
		t.Fatal("revocation time missing")
	}
	if issued.Session.RevokedAt != nil {
		t.Fatal("revoke mutated original")
	}
	if err := revoked.Validate(identityNow.Add(2 * time.Minute)); fault.Code(err) != "session_revoked" {
		t.Fatalf("revoked validation=%v", err)
	}
}

func TestSessionTouchUsesMinimumInterval(t *testing.T) {
	issued, _ := IssueSession("user-1", identityNow, time.Hour)
	unchanged, changed := issued.Session.Touch(identityNow.Add(time.Minute), 5*time.Minute)
	if changed {
		t.Fatal("session touched too early")
	}
	if !unchanged.LastSeenAt.Equal(identityNow) {
		t.Fatalf("last_seen=%v", unchanged.LastSeenAt)
	}
	touched, changed := issued.Session.Touch(identityNow.Add(6*time.Minute), 5*time.Minute)
	if !changed {
		t.Fatal("session was not touched")
	}
	if !touched.LastSeenAt.Equal(identityNow.Add(6 * time.Minute)) {
		t.Fatalf("last_seen=%v", touched.LastSeenAt)
	}
}

func TestIssueSessionRejectsInvalidOwnershipAndTTL(t *testing.T) {
	if _, err := IssueSession("", identityNow, time.Hour); fault.Code(err) != "missing_user" {
		t.Fatalf("missing user=%v", err)
	}
	if _, err := IssueSession("user", identityNow, 30*time.Second); fault.Code(err) != "invalid_session_ttl" {
		t.Fatalf("short ttl=%v", err)
	}
	if _, err := IssueSession("user", identityNow, 31*24*time.Hour); fault.Code(err) != "invalid_session_ttl" {
		t.Fatalf("long ttl=%v", err)
	}
}
