// Package store keeps SCIM Users and Groups the IdP drives.
package store

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// User is a SCIM user retained for reconcile.
type User struct {
	ID         string
	ExternalID string
	UserName   string
	Display    string
	Email      string
	Active     bool
}

// Group is a SCIM group with member user IDs.
type Group struct {
	ID          string
	ExternalID  string
	DisplayName string
	Members     []string // SCIM user ids
}

// Memory is a thread-safe in-memory store with optional JSON persistence.
type Memory struct {
	mu     sync.RWMutex
	users  map[string]*User
	groups map[string]*Group
	path   string
}

// New returns an empty store. If path is non-empty, Load is attempted and
// mutations are flushed to that file.
func New(path string) *Memory {
	m := &Memory{
		users:  make(map[string]*User),
		groups: make(map[string]*Group),
		path:   path,
	}
	if path != "" {
		_ = m.Load()
	}
	return m
}

// Load reads persisted state from path.
func (m *Memory) Load() error {
	if m.path == "" {
		return nil
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var snap struct {
		Users  map[string]*User  `json:"users"`
		Groups map[string]*Group `json:"groups"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if snap.Users != nil {
		m.users = snap.Users
	}
	if snap.Groups != nil {
		m.groups = snap.Groups
	}
	return nil
}

func (m *Memory) persistLocked() error {
	if m.path == "" {
		return nil
	}
	snap := struct {
		Users  map[string]*User  `json:"users"`
		Groups map[string]*Group `json:"groups"`
	}{Users: m.users, Groups: m.groups}
	raw, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

// UpsertUser stores a user.
func (m *Memory) UpsertUser(u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *u
	m.users[u.ID] = &cp
	return m.persistLocked()
}

// DeleteUser removes a user and drops them from all groups.
func (m *Memory) DeleteUser(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.users, id)
	for _, g := range m.groups {
		g.Members = filterOut(g.Members, id)
	}
	return m.persistLocked()
}

// GetUser returns a user by id.
func (m *Memory) GetUser(id string) (*User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return nil, false
	}
	cp := *u
	return &cp, true
}

// FindUserByEmail returns a user by email (case-insensitive).
func (m *Memory) FindUserByEmail(email string) (*User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	want := normalize(email)
	for _, u := range m.users {
		if normalize(u.Email) == want {
			cp := *u
			return &cp, true
		}
	}
	return nil, false
}

// FindUserByExternalID looks up by externalId.
func (m *Memory) FindUserByExternalID(ext string) (*User, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, u := range m.users {
		if u.ExternalID == ext {
			cp := *u
			return &cp, true
		}
	}
	return nil, false
}

// ListUsers returns all users.
func (m *Memory) ListUsers() []*User {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*User, 0, len(m.users))
	for _, u := range m.users {
		cp := *u
		out = append(out, &cp)
	}
	return out
}

// UpsertGroup stores a group.
func (m *Memory) UpsertGroup(g *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *g
	cp.Members = append([]string(nil), g.Members...)
	m.groups[g.ID] = &cp
	return m.persistLocked()
}

// DeleteGroup removes a group.
func (m *Memory) DeleteGroup(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.groups, id)
	return m.persistLocked()
}

// GetGroup returns a group by id.
func (m *Memory) GetGroup(id string) (*Group, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	g, ok := m.groups[id]
	if !ok {
		return nil, false
	}
	cp := *g
	cp.Members = append([]string(nil), g.Members...)
	return &cp, true
}

// ListGroups returns all groups.
func (m *Memory) ListGroups() []*Group {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Group, 0, len(m.groups))
	for _, g := range m.groups {
		cp := *g
		cp.Members = append([]string(nil), g.Members...)
		out = append(out, &cp)
	}
	return out
}

// GroupNamesForUser returns displayNames of groups containing userID.
func (m *Memory) GroupNamesForUser(userID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for _, g := range m.groups {
		for _, mid := range g.Members {
			if mid == userID {
				names = append(names, g.DisplayName)
				break
			}
		}
	}
	return names
}

func filterOut(in []string, id string) []string {
	out := in[:0]
	for _, v := range in {
		if v != id {
			out = append(out, v)
		}
	}
	return append([]string(nil), out...)
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
