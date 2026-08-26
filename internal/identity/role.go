package identity

import (
	"slices"
	"strings"

	"github.com/vance1852/gridvault-ess/internal/fault"
)

type Role string

const (
	RoleDispatcher Role = "dispatcher"
	RoleOperator   Role = "operator"
	RoleAuditor    Role = "auditor"
)

type Permission string

const (
	PermissionPlanWrite       Permission = "plan:write"
	PermissionPlanApprove     Permission = "plan:approve"
	PermissionPlanDispatch    Permission = "plan:dispatch"
	PermissionTelemetryWrite  Permission = "telemetry:write"
	PermissionAlarmManage     Permission = "alarm:manage"
	PermissionSettlementClose Permission = "settlement:close"
	PermissionAuditRead       Permission = "audit:read"
	PermissionSiteManage      Permission = "site:manage"
)

var grants = map[Role][]Permission{
	RoleDispatcher: {
		PermissionPlanWrite,
		PermissionTelemetryWrite,
		PermissionAlarmManage,
		PermissionAuditRead,
	},
	RoleOperator: {
		PermissionPlanApprove,
		PermissionPlanDispatch,
		PermissionTelemetryWrite,
		PermissionAlarmManage,
		PermissionSettlementClose,
		PermissionAuditRead,
		PermissionSiteManage,
	},
	RoleAuditor: {
		PermissionAuditRead,
	},
}

func ParseRole(value string) (Role, error) {
	role := Role(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := grants[role]; !ok {
		return "", fault.WithFields(
			fault.New(fault.Invalid, "invalid_role", "role must be dispatcher, operator, or auditor"),
			map[string]string{"role": value},
		)
	}
	return role, nil
}

func (r Role) Valid() bool {
	_, ok := grants[r]
	return ok
}

func (r Role) Allows(permission Permission) bool {
	return slices.Contains(grants[r], permission)
}

func Require(role Role, permission Permission) error {
	if !role.Valid() {
		return fault.ErrUnauthorized
	}
	if !role.Allows(permission) {
		return fault.WithFields(fault.ErrPermission, map[string]string{
			"role":       string(role),
			"permission": string(permission),
		})
	}
	return nil
}

func Permissions(role Role) []Permission {
	values := grants[role]
	result := make([]Permission, len(values))
	copy(result, values)
	return result
}
