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
	EnsureMember(ctx context.Context, organizationID, email, displayName, role string) error
	SyncUserName(ctx context.Context, email, displayName string) error
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
	groups := e.Store.GroupNamesForUser(userID)
	if u.Active {
		desired = e.Assignments.DesiredWorkspaces(groups)
	} else {
		desired = map[string]assignments.Role{}
	}

	email := strings.TrimSpace(u.Email)
	if email == "" {
		return fmt.Errorf("user %s has no email", userID)
	}

	// Refresh Kaneo display name even when membership is already correct.
	// EnsureMember only runs for new members, so existing rows would otherwise
	// keep the email-as-name from the first SQL insert.
	if u.Active {
		if err := e.Kaneo.SyncUserName(ctx, email, u.Display); err != nil {
			slog.Warn("sync user name failed", "email", email, "err", err)
		}
	}

	// Authentik often POSTs Users before Groups. An active user with no SCIM
	// group membership yet must not be stripped from workspaces — wait until
	// groups arrive (or the user is deactivated).
	if u.Active && len(groups) == 0 {
		slog.Info("skip reconcile until groups synced", "email", email, "user", userID)
		return nil
	}

	// Ensure every assignment-referenced workspace is considered for removal.
	targets := make(map[string]struct{})
	for _, w := range e.Assignments.Workspaces() {
		targets[w] = struct{}{}
	}
	for w := range desired {
		targets[w] = struct{}{}
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
			if err := e.Kaneo.EnsureMember(ctx, ws.ID, email, u.Display, string(wantRole)); err != nil {
				slog.Warn("ensure member failed", "email", email, "workspace", ws.Slug, "role", wantRole, "err", err)
				continue
			}
			slog.Info("ensured membership", "email", email, "workspace", ws.Slug, "role", wantRole)

		case !strings.EqualFold(member.Role, string(wantRole)):
			if err := e.Kaneo.UpdateMemberRole(ctx, ws.ID, member.ID, string(wantRole)); err != nil {
				return fmt.Errorf("update role %s on %s: %w", email, ws.Slug, err)
			}
			slog.Info("updated role", "email", email, "workspace", ws.Slug, "from", member.Role, "to", wantRole)
		}
	}
	return nil
}

// AllUsers reconciles every stored SCIM user (used on startup to backfill).
func (e *Engine) AllUsers(ctx context.Context) {
	users := e.Store.ListUsers()
	slog.Info("startup reconcile", "users", len(users))
	for _, u := range users {
		if err := e.User(ctx, u.ID); err != nil {
			slog.Warn("startup reconcile failed", "user", u.ID, "email", u.Email, "err", err)
		}
	}
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
