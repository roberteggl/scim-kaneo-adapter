package scim

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", "invalid JSON")
		return
	}
	email := primaryEmail(in.Emails, in.UserName)
	if email == "" {
		writeError(w, http.StatusBadRequest, "invalidValue", "email or userName required")
		return
	}
	if existing, ok := s.store.FindUserByEmail(email); ok {
		writeJSON(w, http.StatusOK, s.toSCIMUser(r, existing))
		s.reconcile(r.Context(), existing.ID)
		return
	}
	u := &store.User{
		ID:         uuid.NewString(),
		ExternalID: in.ExternalID,
		UserName:   firstNonEmpty(in.UserName, email),
		Display:    displayName(in.Name, email),
		Email:      email,
		Active:     boolVal(in.Active, true),
	}
	if err := s.store.UpsertUser(u); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s.toSCIMUser(r, u))
	s.reconcile(r.Context(), u.ID)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users := s.store.ListUsers()
	filter := r.URL.Query().Get("filter")
	if filter != "" {
		users = filterUsers(users, filter)
	}
	start, count := pageParams(r)
	page, total := paginate(users, start, count)
	resources := make([]any, 0, len(page))
	for _, u := range page {
		resources = append(resources, s.toSCIMUser(r, u))
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{schemaListResp},
		TotalResults: total,
		StartIndex:   start,
		ItemsPerPage: len(resources),
		Resources:    resources,
	})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	u, ok := s.store.GetUser(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "", "user not found")
		return
	}
	writeJSON(w, http.StatusOK, s.toSCIMUser(r, u))
}

func (s *Server) putUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, ok := s.store.GetUser(id)
	if !ok {
		writeError(w, http.StatusNotFound, "", "user not found")
		return
	}
	var in User
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", "invalid JSON")
		return
	}
	email := primaryEmail(in.Emails, in.UserName)
	if email == "" {
		email = cur.Email
	}
	cur.ExternalID = in.ExternalID
	cur.UserName = firstNonEmpty(in.UserName, email)
	cur.Display = displayName(in.Name, email)
	cur.Email = email
	cur.Active = boolVal(in.Active, cur.Active)
	if err := s.store.UpsertUser(cur); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toSCIMUser(r, cur))
	s.reconcile(r.Context(), cur.ID)
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, ok := s.store.GetUser(id)
	if !ok {
		writeError(w, http.StatusNotFound, "", "user not found")
		return
	}
	ops, err := decodePatch(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}
	for _, op := range ops {
		path := strings.ToLower(op.Path)
		switch {
		case op.Op == "replace" && (path == "active" || path == ""):
			if path == "active" {
				cur.Active = asBool(op.Value, cur.Active)
			} else if m, ok := op.Value.(map[string]any); ok {
				if v, exists := m["active"]; exists {
					cur.Active = asBool(v, cur.Active)
				}
				if v, exists := m["userName"].(string); exists && v != "" {
					cur.UserName = v
				}
				if v, exists := m["externalId"].(string); exists {
					cur.ExternalID = v
				}
			}
		case op.Op == "replace" && path == "username":
			if v, ok := op.Value.(string); ok {
				cur.UserName = v
			}
		}
	}
	if err := s.store.UpsertUser(cur); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.toSCIMUser(r, cur))
	s.reconcile(r.Context(), cur.ID)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.store.GetUser(id); !ok {
		writeError(w, http.StatusNotFound, "", "user not found")
		return
	}
	// Soft-deactivate then reconcile removals, then drop local record.
	if u, ok := s.store.GetUser(id); ok {
		u.Active = false
		_ = s.store.UpsertUser(u)
		s.reconcile(r.Context(), id)
	}
	_ = s.store.DeleteUser(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) toSCIMUser(r *http.Request, u *store.User) User {
	active := u.Active
	return User{
		Schemas:    []string{schemaUser},
		ID:         u.ID,
		ExternalID: u.ExternalID,
		UserName:   u.UserName,
		Name:       &Name{Formatted: u.Display},
		Emails:     []Email{{Value: u.Email, Primary: true, Type: "work"}},
		Active:     &active,
		Meta:       &Meta{ResourceType: "User", Location: location(r, "Users", u.ID)},
	}
}

func filterUsers(users []*store.User, filter string) []*store.User {
	// Support common authentik filters: userName eq "x", emails.value eq "x", externalId eq "x"
	f := strings.ToLower(strings.TrimSpace(filter))
	var out []*store.User
	for _, u := range users {
		switch {
		case strings.Contains(f, "username eq"):
			if matchEq(f, "username eq", u.UserName) {
				out = append(out, u)
			}
		case strings.Contains(f, "emails.value eq") || strings.Contains(f, "email eq"):
			if matchEq(f, "emails.value eq", u.Email) || matchEq(f, "email eq", u.Email) {
				out = append(out, u)
			}
		case strings.Contains(f, "externalid eq"):
			if matchEq(f, "externalid eq", u.ExternalID) {
				out = append(out, u)
			}
		case strings.Contains(f, "id eq"):
			if matchEq(f, "id eq", u.ID) {
				out = append(out, u)
			}
		default:
			out = append(out, u)
		}
	}
	return out
}

func matchEq(filter, prefix, value string) bool {
	idx := strings.Index(filter, prefix)
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(filter[idx+len(prefix):])
	rest = strings.Trim(rest, `"`)
	return strings.EqualFold(rest, value)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func asBool(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true")
	default:
		return def
	}
}
