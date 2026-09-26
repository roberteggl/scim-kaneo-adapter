// Package kaneo is a minimal HTTP client for Kaneo organization (workspace) APIs.
package kaneo

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to a Kaneo instance using an admin API key.
type Client struct {
	base   string
	apiKey string
	http   *http.Client
	db     *sql.DB // optional; enables direct membership (no invite emails)
}

// New builds a Client. baseURL is the Kaneo origin (no trailing slash).
func New(baseURL, apiKey string) *Client {
	return &Client{
		base:   strings.TrimRight(baseURL, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// Workspace is a Kaneo organization.
type Workspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// Member is a workspace membership row.
type Member struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Email string `json:"email"`
	User  *struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
}

// ListWorkspaces returns organizations visible to the API key user.
func (c *Client) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var out []Workspace
	if err := c.do(ctx, http.MethodGet, "/api/auth/organization/list", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveWorkspace finds a workspace by id or slug (case-insensitive).
func (c *Client) ResolveWorkspace(ctx context.Context, idOrSlug string) (*Workspace, error) {
	list, err := c.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(strings.TrimSpace(idOrSlug))
	for i := range list {
		if list[i].ID == idOrSlug || strings.ToLower(list[i].Slug) == want {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("workspace %q not found", idOrSlug)
}

// ListMembers returns members of a workspace.
func (c *Client) ListMembers(ctx context.Context, organizationID string) ([]Member, error) {
	q := url.Values{"organizationId": {organizationID}}
	path := "/api/auth/organization/list-members?" + q.Encode()
	raw, err := c.doRaw(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Members []Member `json:"members"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Members != nil {
		return envelope.Members, nil
	}
	var bare []Member
	if err := json.Unmarshal(raw, &bare); err != nil {
		return nil, fmt.Errorf("decode members: %w", err)
	}
	return bare, nil
}

// InviteMember invites email into organizationID with role.
func (c *Client) InviteMember(ctx context.Context, organizationID, email, role string) error {
	body := map[string]any{
		"organizationId": organizationID,
		"email":          email,
		"role":           role,
		"resend":         false,
	}
	return c.do(ctx, http.MethodPost, "/api/auth/organization/invite-member", body, nil)
}

// UpdateMemberRole changes a member's role.
func (c *Client) UpdateMemberRole(ctx context.Context, organizationID, memberID, role string) error {
	if c.db != nil {
		res, err := c.db.ExecContext(ctx, `
UPDATE workspace_member SET role=$1 WHERE id=$2 AND workspace_id=$3`, role, memberID, organizationID)
		if err != nil {
			return fmt.Errorf("update membership role: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("membership %s not found in workspace %s", memberID, organizationID)
		}
		return nil
	}
	body := map[string]any{
		"organizationId": organizationID,
		"memberId":       memberID,
		"role":           role,
	}
	return c.do(ctx, http.MethodPost, "/api/auth/organization/update-member-role", body, nil)
}

// RemoveMember removes a member by membership id or email.
func (c *Client) RemoveMember(ctx context.Context, organizationID, memberIDOrEmail string) error {
	if c.db != nil {
		res, err := c.db.ExecContext(ctx, `
DELETE FROM workspace_member
WHERE workspace_id=$1 AND (id=$2 OR user_id IN (
  SELECT id FROM "user" WHERE lower(email)=lower($2)
))`, organizationID, memberIDOrEmail)
		if err != nil {
			return fmt.Errorf("remove membership: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("membership %s not found in workspace %s", memberIDOrEmail, organizationID)
		}
		return nil
	}
	body := map[string]any{
		"organizationId":  organizationID,
		"memberIdOrEmail": memberIDOrEmail,
	}
	return c.do(ctx, http.MethodPost, "/api/auth/organization/remove-member", body, nil)
}

// FindMemberByEmail locates a membership by user email.
func FindMemberByEmail(members []Member, email string) *Member {
	want := strings.ToLower(strings.TrimSpace(email))
	for i := range members {
		m := &members[i]
		if strings.ToLower(m.Email) == want {
			return m
		}
		if m.User != nil && strings.ToLower(m.User.Email) == want {
			return m
		}
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	raw, err := c.doRaw(ctx, method, path, body)
	if err != nil {
		return err
	}
	if out == nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode %s: %w (%s)", path, err, truncate(string(raw), 200))
	}
	return nil
}

func (c *Client) doRaw(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("kaneo %s %s: %s: %s", method, path, res.Status, truncate(string(payload), 300))
	}
	return payload, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
