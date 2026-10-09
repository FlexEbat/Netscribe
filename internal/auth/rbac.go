package auth

import (
	"errors"
	"slices"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// ErrRoleUnknown is returned for a role outside viewer, operator and admin.
var ErrRoleUnknown = errors.New("role is unknown")

// rolePermissions is the matrix of section 9.3.
var rolePermissions = map[model.Role][]model.Permission{
	model.RoleViewer: {
		model.PermTopologyRead, model.PermExportRun, model.PermTokensManage,
	},
	model.RoleOperator: {
		model.PermTopologyRead, model.PermExportRun, model.PermTokensManage,
		model.PermScansRun, model.PermDevicesWrite,
	},
	model.RoleAdmin: {
		model.PermTopologyRead, model.PermExportRun, model.PermTokensManage,
		model.PermScansRun, model.PermDevicesWrite,
		model.PermAuditRead, model.PermUsersManage,
	},
}

// ParseRole converts text to a Role, or returns ErrRoleUnknown.
func ParseRole(s string) (model.Role, error) {
	r := model.Role(s)
	if _, ok := rolePermissions[r]; !ok {
		return "", ErrRoleUnknown
	}
	return r, nil
}

// Can reports whether role holds perm. An unknown role holds nothing.
func Can(role model.Role, perm model.Permission) bool {
	return slices.Contains(rolePermissions[role], perm)
}

// Permissions lists what role may do, in the order of the matrix.
func Permissions(role model.Role) []model.Permission {
	return slices.Clone(rolePermissions[role])
}
