package assignments

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Role is a Kaneo workspace membership role. Owner is never assigned by SCIM.
type Role string

const (
	RoleViewer Role = "viewer"
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

var roleRank = map[Role]int{
	RoleViewer: 1,
	RoleMember: 2,
	RoleAdmin:  3,
}

// Assignment maps one SCIM group displayName to a Kaneo workspace + role.
type Assignment struct {
	Group     string `yaml:"group"`
	Workspace string `yaml:"workspace"` // Kaneo workspace slug or id
	Role      Role   `yaml:"role"`
}

// Config is the on-disk assignment list.
type Config struct {
	Assignments []Assignment `yaml:"assignments"`
}

// Load reads and validates an assignments YAML file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse assignments: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// Validate checks required fields and allowed roles.
func (c *Config) Validate() error {
	if len(c.Assignments) == 0 {
		return fmt.Errorf("assignments: at least one entry is required")
	}
	for i, a := range c.Assignments {
		if strings.TrimSpace(a.Group) == "" {
			return fmt.Errorf("assignments[%d]: group is required", i)
		}
		if strings.TrimSpace(a.Workspace) == "" {
			return fmt.Errorf("assignments[%d]: workspace is required", i)
		}
		role := Role(strings.ToLower(string(a.Role)))
		if _, ok := roleRank[role]; !ok {
			return fmt.Errorf("assignments[%d]: role must be viewer, member, or admin", i)
		}
		c.Assignments[i].Role = role
		c.Assignments[i].Group = strings.TrimSpace(a.Group)
		c.Assignments[i].Workspace = strings.TrimSpace(a.Workspace)
	}
	return nil
}

// DesiredWorkspaces returns workspace → highest role for the given SCIM group names.
func (c *Config) DesiredWorkspaces(groupNames []string) map[string]Role {
	have := make(map[string]struct{}, len(groupNames))
	for _, g := range groupNames {
		have[strings.ToLower(strings.TrimSpace(g))] = struct{}{}
	}
	out := make(map[string]Role)
	for _, a := range c.Assignments {
		if _, ok := have[strings.ToLower(a.Group)]; !ok {
			continue
		}
		cur, exists := out[a.Workspace]
		if !exists || roleRank[a.Role] > roleRank[cur] {
			out[a.Workspace] = a.Role
		}
	}
	return out
}

// Workspaces returns the unique workspace keys referenced by assignments.
func (c *Config) Workspaces() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, a := range c.Assignments {
		if _, ok := seen[a.Workspace]; ok {
			continue
		}
		seen[a.Workspace] = struct{}{}
		out = append(out, a.Workspace)
	}
	return out
}

// GroupNames returns unique SCIM group displayNames used in assignments.
func (c *Config) GroupNames() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, a := range c.Assignments {
		key := strings.ToLower(a.Group)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a.Group)
	}
	return out
}
