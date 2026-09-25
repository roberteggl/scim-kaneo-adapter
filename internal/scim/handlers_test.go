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
