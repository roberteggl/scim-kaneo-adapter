// Package reconcile applies assignment-derived workspace membership to Kaneo.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/roberteggl/scim-kaneo-adapter/internal/assignments"
	"github.com/roberteggl/scim-kaneo-adapter/internal/kaneo"
	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

// KaneoAPI is the Kaneo surface reconcile needs.
type KaneoAPI interface {
	ResolveWorkspace(ctx context.Context, idOrSlug string) (*kaneo.Workspace, error)
	ListMembers(ctx context.Context, organizationID string) ([]kaneo.Member, error)
	InviteMember(ctx context.Context, organizationID, email, role string) error
	UpdateMemberRole(ctx context.Context, organizationID, memberID, role string) error
	RemoveMember(ctx context.Context, organizationID, memberIDOrEmail string) error
}

// Engine reconciles one SCIM user's Kaneo memberships.
type Engine struct {
	Store       *store.Memory
	Assignments *assignments.Config
	Kaneo       KaneoAPI
	wsCache     map[string]*kaneo.Workspace
}

// User applies desired workspace roles for the SCIM user id.
func (e *Engine) User(ctx context.Context, userID string) error {
	u, ok := e.Store.GetUser(userID)
	if !ok {
		return fmt.Errorf("unknown user %s", userID)
	}
	if e.wsCache == nil {
		e.wsCache = make(map[string]*kaneo.Workspace)
	}

	var desired map[string]assignments.Role
	if u.Active {
		desired = e.Assignments.DesiredWorkspaces(e.Store.GroupNamesForUser(userID))
	} else {
		desired = map[string]assignments.Role{}
	}

	// Ensure every assignment-referenced workspace is considered for removal.
	targets := make(map[string]struct{})
	for _, w := range e.Assignments.Workspaces() {
		targets[w] = struct{}{}
	}
	for w := range desired {
		targets[w] = struct{}{}
	}

	email := strings.TrimSpace(u.Email)
	if email == "" {
		return fmt.Errorf("user %s has no email", userID)
	}

	for key := range targets {
		ws, err := e.resolve(ctx, key)
		if err != nil {
			return err
		}
		wantRole, keep := desired[key]
		members, err := e.Kaneo.ListMembers(ctx, ws.ID)
		if err != nil {
			return fmt.Errorf("list members %s: %w", ws.Slug, err)
		}
		member := kaneo.FindMemberByEmail(members, email)

		switch {
		case !keep:
			if member == nil {
				continue
			}
			if err := e.Kaneo.RemoveMember(ctx, ws.ID, member.ID); err != nil {
				// Fallback to email if id-based remove fails.
				if err2 := e.Kaneo.RemoveMember(ctx, ws.ID, email); err2 != nil {
					return fmt.Errorf("remove %s from %s: %v / %v", email, ws.Slug, err, err2)
				}
			}
			slog.Info("removed membership", "email", email, "workspace", ws.Slug)

		case member == nil:
			if err := e.Kaneo.InviteMember(ctx, ws.ID, email, string(wantRole)); err != nil {
				// Already invited / already member: treat as soft success and try role update path next sync.
				slog.Warn("invite failed", "email", email, "workspace", ws.Slug, "role", wantRole, "err", err)
				continue
			}
			slog.Info("invited", "email", email, "workspace", ws.Slug, "role", wantRole)

		case !strings.EqualFold(member.Role, string(wantRole)):
			if err := e.Kaneo.UpdateMemberRole(ctx, ws.ID, member.ID, string(wantRole)); err != nil {
				return fmt.Errorf("update role %s on %s: %w", email, ws.Slug, err)
			}
			slog.Info("updated role", "email", email, "workspace", ws.Slug, "from", member.Role, "to", wantRole)
		}
	}
	return nil
}

func (e *Engine) resolve(ctx context.Context, idOrSlug string) (*kaneo.Workspace, error) {
	if ws, ok := e.wsCache[idOrSlug]; ok {
		return ws, nil
	}
	ws, err := e.Kaneo.ResolveWorkspace(ctx, idOrSlug)
	if err != nil {
		return nil, err
	}
	e.wsCache[idOrSlug] = ws
	e.wsCache[ws.ID] = ws
	e.wsCache[ws.Slug] = ws
	return ws, nil
}
