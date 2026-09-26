package scim_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/roberteggl/scim-kaneo-adapter/internal/scim"
	"github.com/roberteggl/scim-kaneo-adapter/internal/store"
)

func TestCreateAndGetUser(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)
	body := `{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"ada@example.com","emails":[{"value":"ada@example.com","primary":true}],"active":true}`
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/scim+json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("missing id")
	}

	req = httptest.NewRequest(http.MethodGet, "/scim/v2/Users/"+id, nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status %d", rec.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	srv := scim.NewServer(store.New(""), "secret", nil)
	req := httptest.NewRequest(http.MethodGet, "/scim/v2/Users", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestGroupMembershipPatchReconciles(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(
		`{"userName":"b@example.com","emails":[{"value":"b@example.com","primary":true}]}`,
	))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	uid := user["id"].(string)

	gbody := `{"displayName":"kaneo","members":[{"value":"` + uid + `"}]}`
	req = httptest.NewRequest(http.MethodPost, "/scim/v2/Groups", bytes.NewBufferString(gbody))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("group create %d: %s", rec.Code, rec.Body.String())
	}
	names := st.GroupNamesForUser(uid)
	if len(names) != 1 || names[0] != "kaneo" {
		t.Fatalf("group names: %v", names)
	}
}

func TestPatchRemoveFilterPathDropsMember(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(
		`{"userName":"d@example.com","emails":[{"value":"d@example.com","primary":true}]}`,
	))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	uid := user["id"].(string)

	gbody := `{"displayName":"authentik Admins","externalId":"e6491517-4b0c-4e7d-bb7b-411c9be68a20","members":[{"value":"` + uid + `"}]}`
	req = httptest.NewRequest(http.MethodPost, "/scim/v2/Groups", bytes.NewBufferString(gbody))
	req.Header.Set("Authorization", "Bearer secret")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var group map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &group)
	gid := group["id"].(string)

	// Authentik removal form: path filter, no value body.
	patch := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"members[value eq \"` + uid + `\"]"}]}`
	req = httptest.NewRequest(http.MethodPatch, "/scim/v2/Groups/"+gid, bytes.NewBufferString(patch))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/scim+json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	g, _ := st.GetGroup(gid)
	if len(g.Members) != 0 {
		t.Fatalf("member not removed, still %v", g.Members)
	}
	names := st.GroupNamesForUser(uid)
	if len(names) != 0 {
		t.Fatalf("user still in groups %v", names)
	}
}

func TestPatchMissingGroupRehydratesAndRemoves(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)

	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Users", bytes.NewBufferString(
		`{"userName":"c@example.com","emails":[{"value":"c@example.com","primary":true}]}`,
	))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	uid := user["id"].(string)

	// Authentik still knows a remote group id that is gone from our store.
	staleID := "88bee88a-1acd-4aa4-b779-bda7150f01f9"
	patch := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"members","value":[{"value":"` + uid + `"}]}]}`
	req = httptest.NewRequest(http.MethodPatch, "/scim/v2/Groups/"+staleID, bytes.NewBufferString(patch))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/scim+json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 rehydrate, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, ok := st.GetGroup(staleID); !ok {
		t.Fatal("expected rehydrated group in store")
	}
}

func TestPatchReplaceNullPathSetsDisplayName(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)
	id := "e6491517-4b0c-4e7d-bb7b-411c9be68a20"
	_ = st.UpsertGroup(&store.Group{ID: id, ExternalID: id})

	patch := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"replace","value":{"displayName":"authentik Admins","externalId":"` + id + `"}}]}`
	req := httptest.NewRequest(http.MethodPatch, "/scim/v2/Groups/"+id, bytes.NewBufferString(patch))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", "application/scim+json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	g, ok := st.GetGroup(id)
	if !ok || g.DisplayName != "authentik Admins" {
		t.Fatalf("displayName not applied: %+v", g)
	}
}

func TestCreateGroupUsesExternalID(t *testing.T) {
	st := store.New("")
	srv := scim.NewServer(st, "secret", nil)
	ext := "e6491517-4b0c-4e7d-bb7b-411c9be68a20"
	body := `{"displayName":"authentik Admins","externalId":"` + ext + `","members":[]}`
	req := httptest.NewRequest(http.MethodPost, "/scim/v2/Groups", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["id"] != ext {
		t.Fatalf("want id=%s got %v", ext, created["id"])
	}
}
