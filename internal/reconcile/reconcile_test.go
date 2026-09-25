package reconcile_test

import (
	"context"
	"errors"
	"testing"

	"github.com/roberteggl/scim-kaneo-adapter/internal/assignments"
	"github.com/roberteggl/scim-kaneo-adapter/internal/kaneo"
	"github.com/roberteggl/scim-kaneo-adapter/internal/reconcile"
	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

var errWorkspaceMissing = errors.New("workspace missing")

type fakeKaneo struct {
	workspaces map[string]*kaneo.Workspace
	members    map[string][]kaneo.Member
	invites    []string
	updates    []string
	removes    []string
}

func (f *fakeKaneo) ResolveWorkspace(_ context.Context, idOrSlug string) (*kaneo.Workspace, error) {
	ws := f.workspaces[idOrSlug]
	if ws == nil {
		return nil, errWorkspaceMissing
	}
	return ws, nil
}
func (f *fakeKaneo) ListMembers(_ context.Context, orgID string) ([]kaneo.Member, error) {
	return f.members[orgID], nil
}
func (f *fakeKaneo) InviteMember(_ context.Context, orgID, email, role string) error {
	f.invites = append(f.invites, orgID+":"+email+":"+role)
	f.members[orgID] = append(f.members[orgID], kaneo.Member{ID: "m-" + email, Role: role, Email: email})
	return nil
}
func (f *fakeKaneo) UpdateMemberRole(_ context.Context, orgID, memberID, role string) error {
	f.updates = append(f.updates, orgID+":"+memberID+":"+role)
	for i := range f.members[orgID] {
		if f.members[orgID][i].ID == memberID {
			f.members[orgID][i].Role = role
		}
	}
	return nil
}
func (f *fakeKaneo) RemoveMember(_ context.Context, orgID, memberIDOrEmail string) error {
	f.removes = append(f.removes, orgID+":"+memberIDOrEmail)
	var kept []kaneo.Member
	for _, m := range f.members[orgID] {
		if m.ID != memberIDOrEmail && m.Email != memberIDOrEmail {
			kept = append(kept, m)
		}
	}
	f.members[orgID] = kept
	return nil
}

func TestReconcileInviteAndElevate(t *testing.T) {
	st := store.New("")
	_ = st.UpsertUser(&store.User{ID: "u1", Email: "a@example.com", Active: true, UserName: "a"})
	_ = st.UpsertGroup(&store.Group{ID: "g1", DisplayName: "kaneo", Members: []string{"u1"}})

	fk := &fakeKaneo{
		workspaces: map[string]*kaneo.Workspace{
			"neuland-next": {ID: "ws1", Slug: "neuland-next", Name: "Neuland Next"},
		},
		members: map[string][]kaneo.Member{"ws1": nil},
	}
	engine := &reconcile.Engine{
		Store: st,
		Assignments: &assignments.Config{Assignments: []assignments.Assignment{
			{Group: "kaneo", Workspace: "neuland-next", Role: assignments.RoleViewer},
			{Group: "vorstand", Workspace: "neuland-next", Role: assignments.RoleAdmin},
		}},
		Kaneo: fk,
	}
	if err := engine.User(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if len(fk.invites) != 1 || fk.invites[0] != "ws1:a@example.com:viewer" {
		t.Fatalf("invites: %v", fk.invites)
	}

	_ = st.UpsertGroup(&store.Group{ID: "g2", DisplayName: "vorstand", Members: []string{"u1"}})
	if err := engine.User(context.Background(), "u1"); err != nil {
		t.Fatal(err)
	}
	if len(fk.updates) != 1 {
		t.Fatalf("expected role update, got %v", fk.updates)
	}
}
