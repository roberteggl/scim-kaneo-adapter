package assignments

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesiredWorkspacesHighestRoleWins(t *testing.T) {
	c := &Config{Assignments: []Assignment{
		{Group: "kaneo", Workspace: "product", Role: RoleViewer},
		{Group: "software-product", Workspace: "product", Role: RoleMember},
		{Group: "admins", Workspace: "product", Role: RoleAdmin},
		{Group: "admins", Workspace: "platform", Role: RoleAdmin},
	}}
	got := c.DesiredWorkspaces([]string{"kaneo", "admins"})
	if got["product"] != RoleAdmin {
		t.Fatalf("product: want admin, got %s", got["product"])
	}
	if got["platform"] != RoleAdmin {
		t.Fatalf("platform: want admin, got %s", got["platform"])
	}
}

func TestDesiredWorkspacesCaseInsensitiveGroups(t *testing.T) {
	c := &Config{Assignments: []Assignment{
		{Group: "Kaneo", Workspace: "product", Role: RoleViewer},
	}}
	got := c.DesiredWorkspaces([]string{"kaneo"})
	if got["product"] != RoleViewer {
		t.Fatalf("want viewer, got %v", got)
	}
}

func TestLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assignments.yaml")
	content := `
assignments:
  - group: kaneo
    workspace: product
    role: viewer
  - group: admins
    workspace: platform
    role: ADMIN
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Assignments[1].Role != RoleAdmin {
		t.Fatalf("role not normalized: %s", c.Assignments[1].Role)
	}
}

func TestValidateRejectsOwner(t *testing.T) {
	c := &Config{Assignments: []Assignment{
		{Group: "x", Workspace: "y", Role: "owner"},
	}}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for owner role")
	}
}
