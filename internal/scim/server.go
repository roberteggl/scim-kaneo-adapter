package scim

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

// Reconciler applies Kaneo workspace membership after SCIM mutations.
type Reconciler interface {
	User(ctx context.Context, userID string) error
}

// Server is the SCIM HTTP API.
type Server struct {
	store  *store.Memory
	token  string
	engine Reconciler
}

// NewServer builds a SCIM server. Pass a nil engine to skip Kaneo reconcile (tests).
func NewServer(st *store.Memory, token string, engine Reconciler) *Server {
	return &Server{store: st, token: token, engine: engine}
}

// Handler returns the authenticated router under /scim/v2.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /scim/v2/ServiceProviderConfig", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, serviceProviderConfig())
	})
	mux.HandleFunc("GET /scim/v2/Schemas", s.listSchemas)
	mux.HandleFunc("GET /scim/v2/ResourceTypes", s.listResourceTypes)

	mux.HandleFunc("POST /scim/v2/Users", s.createUser)
	mux.HandleFunc("GET /scim/v2/Users", s.listUsers)
	mux.HandleFunc("GET /scim/v2/Users/{id}", s.getUser)
	mux.HandleFunc("PUT /scim/v2/Users/{id}", s.putUser)
	mux.HandleFunc("PATCH /scim/v2/Users/{id}", s.patchUser)
	mux.HandleFunc("DELETE /scim/v2/Users/{id}", s.deleteUser)

	mux.HandleFunc("POST /scim/v2/Groups", s.createGroup)
	mux.HandleFunc("GET /scim/v2/Groups", s.listGroups)
	mux.HandleFunc("GET /scim/v2/Groups/{id}", s.getGroup)
	mux.HandleFunc("PUT /scim/v2/Groups/{id}", s.putGroup)
	mux.HandleFunc("PATCH /scim/v2/Groups/{id}", s.patchGroup)
	mux.HandleFunc("DELETE /scim/v2/Groups/{id}", s.deleteGroup)

	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	want := []byte(s.token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			writeError(w, http.StatusUnauthorized, "", "invalid or missing bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func location(r *http.Request, resource, id string) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + r.Host + "/scim/v2/" + resource + "/" + id
}

func (s *Server) reconcile(ctx context.Context, userID string) {
	if s.engine == nil || userID == "" {
		return
	}
	// Authentik often closes the HTTP request after receiving 200; keep
	// reconcile alive so multi-workspace invites are not aborted mid-loop.
	if err := s.engine.User(context.WithoutCancel(ctx), userID); err != nil {
		slog.Error("reconcile failed", "user", userID, "err", err)
	}
}

func (s *Server) listSchemas(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{schemaListResp},
		TotalResults: 2,
		StartIndex:   1,
		ItemsPerPage: 2,
		Resources: []any{
			map[string]any{"id": schemaUser, "name": "User"},
			map[string]any{"id": schemaGroup, "name": "Group"},
		},
	})
}

func (s *Server) listResourceTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{schemaListResp},
		TotalResults: 2,
		StartIndex:   1,
		ItemsPerPage: 2,
		Resources: []any{
			map[string]any{
				"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
				"id":       "User",
				"name":     "User",
				"endpoint": "/Users",
				"schema":   schemaUser,
			},
			map[string]any{
				"schemas":  []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"},
				"id":       "Group",
				"name":     "Group",
				"endpoint": "/Groups",
				"schema":   schemaGroup,
			},
		},
	})
}
