package assignments

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesiredWorkspacesHighestRoleWins(t *testing.T) {
	c := &Config{Assignments: []Assignment{
		{Group: "kaneo", Workspace: "neuland-next", Role: RoleViewer},
		{Group: "software-neuland-next", Workspace: "neuland-next", Role: RoleMember},
		{Group: "vorstand", Workspace: "neuland-next", Role: RoleAdmin},
		{Group: "vorstand", Workspace: "neuland-ressorts", Role: RoleAdmin},
	}}
	got := c.DesiredWorkspaces([]string{"kaneo", "vorstand"})
	if got["neuland-next"] != RoleAdmin {
		t.Fatalf("neuland-next: want admin, got %s", got["neuland-next"])
	}
	if got["neuland-ressorts"] != RoleAdmin {
		t.Fatalf("neuland-ressorts: want admin, got %s", got["neuland-ressorts"])
	}
}

func TestDesiredWorkspacesCaseInsensitiveGroups(t *testing.T) {
	c := &Config{Assignments: []Assignment{
		{Group: "Kaneo", Workspace: "neuland-next", Role: RoleViewer},
	}}
	got := c.DesiredWorkspaces([]string{"kaneo"})
	if got["neuland-next"] != RoleViewer {
		t.Fatalf("want viewer, got %v", got)
	}
}

func TestLoadAndValidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "assignments.yaml")
	content := `
assignments:
  - group: kaneo
    workspace: neuland-next
    role: viewer
  - group: vorstand
    workspace: neuland-ressorts
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
