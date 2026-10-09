package auth

import (
	"errors"
	"testing"

	"github.com/FlexEbat/Netscribe/internal/model"
)

// The matrix of section 9.3, written out independently of the implementation.
func TestPermissionMatrix(t *testing.T) {
	all := []model.Permission{
		model.PermTopologyRead, model.PermExportRun, model.PermTokensManage,
		model.PermScansRun, model.PermDevicesWrite, model.PermAuditRead, model.PermUsersManage,
	}
	want := map[model.Role]map[model.Permission]bool{
		model.RoleViewer: {
			model.PermTopologyRead: true, model.PermExportRun: true, model.PermTokensManage: true,
		},
		model.RoleOperator: {
			model.PermTopologyRead: true, model.PermExportRun: true, model.PermTokensManage: true,
			model.PermScansRun: true, model.PermDevicesWrite: true,
		},
		model.RoleAdmin: {
			model.PermTopologyRead: true, model.PermExportRun: true, model.PermTokensManage: true,
			model.PermScansRun: true, model.PermDevicesWrite: true, model.PermAuditRead: true, model.PermUsersManage: true,
		},
	}
	for role, perms := range want {
		for _, p := range all {
			if got := Can(role, p); got != perms[p] {
				t.Errorf("Can(%s, %s) = %v, want %v", role, p, got, perms[p])
			}
		}
	}
}

func TestUnknownRoleHoldsNothing(t *testing.T) {
	for _, r := range []model.Role{"", "root", "ADMIN", "superuser"} {
		if Can(r, model.PermTopologyRead) || Can(r, model.PermUsersManage) {
			t.Errorf("role %q holds a permission", r)
		}
		if len(Permissions(r)) != 0 {
			t.Errorf("Permissions(%q) is not empty", r)
		}
	}
}

func TestParseRole(t *testing.T) {
	for _, s := range []string{"viewer", "operator", "admin"} {
		if r, err := ParseRole(s); err != nil || string(r) != s {
			t.Errorf("ParseRole(%q) = %v, %v", s, r, err)
		}
	}
	for _, s := range []string{"", "Admin", "root", " admin"} {
		if _, err := ParseRole(s); !errors.Is(err, ErrRoleUnknown) {
			t.Errorf("ParseRole(%q) = %v, want ErrRoleUnknown", s, err)
		}
	}
}

func TestPermissionsReturnsACopy(t *testing.T) {
	p := Permissions(model.RoleViewer)
	p[0] = model.PermUsersManage
	if Can(model.RoleViewer, model.PermUsersManage) {
		t.Error("modifying the returned slice changed the role matrix")
	}
}

func TestAdminHoldsEverythingViewerDoes(t *testing.T) {
	for _, p := range Permissions(model.RoleViewer) {
		if !Can(model.RoleOperator, p) || !Can(model.RoleAdmin, p) {
			t.Errorf("a higher role lacks %s", p)
		}
	}
}
