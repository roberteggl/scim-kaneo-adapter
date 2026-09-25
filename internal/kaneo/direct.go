package kaneo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const idAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// OpenDB opens a pgx database handle from a Postgres URL. Returns nil, nil when url is empty.
func OpenDB(databaseURL string) (*sql.DB, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return nil, nil
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("kaneo db ping: %w", err)
	}
	return db, nil
}

// WithDB enables direct workspace membership via Postgres (bypasses invite emails).
func (c *Client) WithDB(db *sql.DB) *Client {
	c.db = db
	return c
}

// EnsureMember creates the Kaneo user if needed and adds them to the workspace
// with the given role. When a DB handle is configured this skips the invitation
// email flow; otherwise it falls back to InviteMember.
func (c *Client) EnsureMember(ctx context.Context, organizationID, email, displayName, role string) error {
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email required")
	}
	if c.db == nil {
		return c.InviteMember(ctx, organizationID, email, role)
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = strings.Split(email, "@")[0]
	}
	userID, err := c.ensureUser(ctx, email, name)
	if err != nil {
		return err
	}
	if err := c.ensureMembership(ctx, organizationID, userID, role); err != nil {
		return err
	}
	// Drop any pending invites for this email/workspace so the UI stays clean.
	if _, err := c.db.ExecContext(ctx, `
UPDATE invitation SET status='canceled'
WHERE status='pending' AND workspace_id=$1 AND lower(email)=lower($2)`, organizationID, email); err != nil {
		slog.Warn("cancel pending invites", "email", email, "workspace", organizationID, "err", err)
	}
	return nil
}

func (c *Client) ensureUser(ctx context.Context, email, name string) (string, error) {
	var id string
	err := c.db.QueryRowContext(ctx, `SELECT id FROM "user" WHERE lower(email)=lower($1)`, email).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("lookup user %s: %w", email, err)
	}
	id, err = randomID(32)
	if err != nil {
		return "", err
	}
	_, err = c.db.ExecContext(ctx, `
INSERT INTO "user" (id, name, email, email_verified, role, created_at, updated_at, is_anonymous, banned)
VALUES ($1, $2, $3, true, 'user', NOW(), NOW(), false, false)
ON CONFLICT (email) DO NOTHING`, id, name, email)
	if err != nil {
		return "", fmt.Errorf("insert user %s: %w", email, err)
	}
	// Re-read in case of concurrent insert.
	if err := c.db.QueryRowContext(ctx, `SELECT id FROM "user" WHERE lower(email)=lower($1)`, email).Scan(&id); err != nil {
		return "", fmt.Errorf("reload user %s: %w", email, err)
	}
	slog.Info("created kaneo user", "email", email, "id", id)
	return id, nil
}

func (c *Client) ensureMembership(ctx context.Context, workspaceID, userID, role string) error {
	var memberID, existingRole string
	err := c.db.QueryRowContext(ctx, `
SELECT id, role FROM workspace_member WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).
		Scan(&memberID, &existingRole)
	switch {
	case err == nil:
		if strings.EqualFold(existingRole, role) {
			return nil
		}
		_, err = c.db.ExecContext(ctx, `UPDATE workspace_member SET role=$1 WHERE id=$2`, role, memberID)
		if err != nil {
			return fmt.Errorf("update membership role: %w", err)
		}
		return nil
	case err != sql.ErrNoRows:
		return fmt.Errorf("lookup membership: %w", err)
	}
	memberID, err = randomID(32)
	if err != nil {
		return err
	}
	_, err = c.db.ExecContext(ctx, `
INSERT INTO workspace_member (id, workspace_id, role, joined_at, user_id)
VALUES ($1, $2, $3, NOW(), $4)`, memberID, workspaceID, role, userID)
	if err != nil {
		return fmt.Errorf("insert membership: %w", err)
	}
	return nil
}

func randomID(n int) (string, error) {
	out := make([]byte, n)
	max := big.NewInt(int64(len(idAlphabet)))
	for i := range out {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		out[i] = idAlphabet[v.Int64()]
	}
	return string(out), nil
}
