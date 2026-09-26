package scim

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var in Group
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", "invalid JSON")
		return
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "invalidValue", "displayName required")
		return
	}
	// Prefer IdP externalId as the SCIM id so mappings survive store rebuilds
	// when Authentik reuses the same Authentik-group UUID as externalId.
	id := strings.TrimSpace(in.ExternalID)
	if id == "" {
		id = uuid.NewString()
	} else if existing, ok := s.store.GetGroup(id); ok {
		// Idempotent create: refresh and reconcile.
		before := append([]string(nil), existing.Members...)
		existing.ExternalID = in.ExternalID
		existing.DisplayName = in.DisplayName
		existing.Members = memberIDs(in.Members)
		if err := s.store.UpsertGroup(existing); err != nil {
			writeError(w, http.StatusInternalServerError, "", err.Error())
			return
		}
		s.reconcileMembers(r, union(before, existing.Members))
		writeJSON(w, http.StatusOK, s.toSCIMGroup(r, existing))
		return
	}
	g := &store.Group{
		ID:          id,
		ExternalID:  in.ExternalID,
		DisplayName: in.DisplayName,
		Members:     memberIDs(in.Members),
	}
	if err := s.store.UpsertGroup(g); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	s.reconcileMembers(r, g.Members)
	writeJSON(w, http.StatusCreated, s.toSCIMGroup(r, g))
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups := s.store.ListGroups()
	filter := r.URL.Query().Get("filter")
	if filter != "" {
		groups = filterGroups(groups, filter)
	}
	start, count := pageParams(r)
	page, total := paginate(groups, start, count)
	resources := make([]any, 0, len(page))
	for _, g := range page {
		resources = append(resources, s.toSCIMGroup(r, g))
	}
	writeJSON(w, http.StatusOK, ListResponse{
		Schemas:      []string{schemaListResp},
		TotalResults: total,
		StartIndex:   start,
		ItemsPerPage: len(resources),
		Resources:    resources,
	})
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	g, ok := s.store.GetGroup(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "", "group not found")
		return
	}
	writeJSON(w, http.StatusOK, s.toSCIMGroup(r, g))
}

func (s *Server) putGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, ok := s.store.GetGroup(id)
	if !ok {
		cur = &store.Group{ID: id, ExternalID: id}
		slogRehydrateGroup(id)
	}
	var in Group
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", "invalid JSON")
		return
	}
	before := append([]string(nil), cur.Members...)
	cur.DisplayName = firstNonEmpty(in.DisplayName, cur.DisplayName)
	cur.ExternalID = in.ExternalID
	cur.Members = memberIDs(in.Members)
	if err := s.store.UpsertGroup(cur); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	s.reconcileMembers(r, union(before, cur.Members))
	writeJSON(w, http.StatusOK, s.toSCIMGroup(r, cur))
}

func (s *Server) patchGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	cur, ok := s.store.GetGroup(id)
	if !ok {
		// After an emptyDir/store wipe Authentik still holds the old SCIM group
		// id. Accept the PATCH against that id so member removals (e.g. leaving
		// authentik Admins → fall back to viewer) still reconcile.
		cur = &store.Group{ID: id, ExternalID: id}
		slogRehydrateGroup(id)
	}
	ops, err := decodePatch(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalidSyntax", err.Error())
		return
	}
	before := append([]string(nil), cur.Members...)
	var touched []string
	for _, op := range ops {
		path := strings.ToLower(op.Path)
		switch op.Op {
		case "replace":
			if path == "displayname" {
				if v, ok := op.Value.(string); ok {
					cur.DisplayName = v
				}
			} else if path == "members" {
				cur.Members = valuesAsMemberIDs(op.Value)
				touched = append(touched, cur.Members...)
			} else if path == "" {
				// Authentik sends replace with path=null and a partial Group object
				// (displayName, externalId, …) when syncing group attributes.
				applyGroupReplaceValue(cur, op.Value, &touched)
			}
		case "add":
			if path == "members" || strings.HasPrefix(path, "members") {
				added := valuesAsMemberIDs(op.Value)
				cur.Members = uniqueAppend(cur.Members, added...)
				touched = append(touched, added...)
			}
		case "remove":
			if path == "members" || strings.HasPrefix(path, "members") {
				remove := valuesAsMemberIDs(op.Value)
				if len(remove) == 0 && strings.Contains(path, "value eq") {
					remove = []string{extractEqValue(op.Path)}
				}
				cur.Members = subtract(cur.Members, remove)
				touched = append(touched, remove...)
			}
		}
	}
	if err := s.store.UpsertGroup(cur); err != nil {
		writeError(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	if strings.TrimSpace(cur.DisplayName) == "" {
		slog.Warn("group patch left empty displayName; assignments will not match", "id", id)
	}
	s.reconcileMembers(r, union(union(before, cur.Members), touched))
	writeJSON(w, http.StatusOK, s.toSCIMGroup(r, cur))
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, ok := s.store.GetGroup(id)
	if !ok {
		writeError(w, http.StatusNotFound, "", "group not found")
		return
	}
	members := append([]string(nil), g.Members...)
	_ = s.store.DeleteGroup(id)
	w.WriteHeader(http.StatusNoContent)
	s.reconcileMembers(r, members)
}

func (s *Server) toSCIMGroup(r *http.Request, g *store.Group) Group {
	members := make([]Member, 0, len(g.Members))
	for _, id := range g.Members {
		m := Member{Value: id, Ref: location(r, "Users", id)}
		if u, ok := s.store.GetUser(id); ok {
			m.Display = u.Display
		}
		members = append(members, m)
	}
	return Group{
		Schemas:     []string{schemaGroup},
		ID:          g.ID,
		ExternalID:  g.ExternalID,
		DisplayName: g.DisplayName,
		Members:     members,
		Meta:        &Meta{ResourceType: "Group", Location: location(r, "Groups", g.ID)},
	}
}

func (s *Server) reconcileMembers(r *http.Request, ids []string) {
	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		s.reconcile(r.Context(), id)
	}
}

func memberIDs(members []Member) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		if m.Value != "" {
			out = append(out, m.Value)
		}
	}
	return out
}

func valuesAsMemberIDs(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, item := range t {
			switch m := item.(type) {
			case string:
				out = append(out, m)
			case map[string]any:
				if val, ok := m["value"].(string); ok {
					out = append(out, val)
				}
			}
		}
		return out
	case map[string]any:
		if val, ok := t["value"].(string); ok {
			return []string{val}
		}
	}
	return nil
}

func filterGroups(groups []*store.Group, filter string) []*store.Group {
	f := strings.ToLower(strings.TrimSpace(filter))
	var out []*store.Group
	for _, g := range groups {
		switch {
		case strings.Contains(f, "displayname eq"):
			if matchEq(f, "displayname eq", g.DisplayName) {
				out = append(out, g)
			}
		case strings.Contains(f, "externalid eq"):
			if matchEq(f, "externalid eq", g.ExternalID) {
				out = append(out, g)
			}
		case strings.Contains(f, "id eq"):
			if matchEq(f, "id eq", g.ID) {
				out = append(out, g)
			}
		default:
			out = append(out, g)
		}
	}
	return out
}

func uniqueAppend(base []string, add ...string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, v := range append(base, add...) {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func subtract(base, remove []string) []string {
	drop := map[string]struct{}{}
	for _, r := range remove {
		drop[r] = struct{}{}
	}
	var out []string
	for _, v := range base {
		if _, ok := drop[v]; !ok {
			out = append(out, v)
		}
	}
	return out
}

func union(a, b []string) []string {
	return uniqueAppend(a, b...)
}

func extractEqValue(path string) string {
	lower := strings.ToLower(path)
	idx := strings.Index(lower, "value eq")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(path[idx+len("value eq"):])
	return strings.Trim(rest, ` "`)
}

func slogRehydrateGroup(id string) {
	slog.Info("rehydrating missing SCIM group for patch", "id", id)
}

// applyGroupReplaceValue handles RFC7644 replace with no path (whole resource
// subset), as sent by Authentik's _update_patch_general.
func applyGroupReplaceValue(cur *store.Group, value any, touched *[]string) {
	m, ok := value.(map[string]any)
	if !ok {
		return
	}
	if v, ok := m["displayName"].(string); ok && strings.TrimSpace(v) != "" {
		cur.DisplayName = v
	}
	if v, ok := m["externalId"].(string); ok {
		cur.ExternalID = v
	}
	if raw, has := m["members"]; has {
		cur.Members = valuesAsMemberIDs(raw)
		*touched = append(*touched, cur.Members...)
	}
}
