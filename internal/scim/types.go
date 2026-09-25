package scim

import (
	"encoding/json"
	"net/http"
	"strconv"
)

const contentType = "application/scim+json"

const (
	schemaUser        = "urn:ietf:params:scim:schemas:core:2.0:User"
	schemaGroup       = "urn:ietf:params:scim:schemas:core:2.0:Group"
	schemaError       = "urn:ietf:params:scim:api:messages:2.0:Error"
	schemaListResp    = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	schemaPatchOp     = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	schemaServiceConf = "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"
)

// Name is the SCIM name attribute.
type Name struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

// Email is a SCIM email entry.
type Email struct {
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
	Type    string `json:"type,omitempty"`
}

// Meta is SCIM resource metadata.
type Meta struct {
	ResourceType string `json:"resourceType"`
	Location     string `json:"location,omitempty"`
}

// User is a SCIM user resource.
type User struct {
	Schemas    []string `json:"schemas"`
	ID         string   `json:"id"`
	ExternalID string   `json:"externalId,omitempty"`
	UserName   string   `json:"userName"`
	Name       *Name    `json:"name,omitempty"`
	Emails     []Email  `json:"emails,omitempty"`
	Active     *bool    `json:"active,omitempty"`
	Meta       *Meta    `json:"meta,omitempty"`
}

// Member is a SCIM group member reference.
type Member struct {
	Value   string `json:"value"`
	Ref     string `json:"$ref,omitempty"`
	Display string `json:"display,omitempty"`
}

// Group is a SCIM group resource.
type Group struct {
	Schemas     []string `json:"schemas"`
	ID          string   `json:"id"`
	ExternalID  string   `json:"externalId,omitempty"`
	DisplayName string   `json:"displayName"`
	Members     []Member `json:"members"`
	Meta        *Meta    `json:"meta,omitempty"`
}

// ListResponse is the SCIM list envelope.
type ListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

type scimError struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	SCIMType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, scimType, detail string) {
	writeJSON(w, status, scimError{
		Schemas:  []string{schemaError},
		Status:   strconv.Itoa(status),
		SCIMType: scimType,
		Detail:   detail,
	})
}

func serviceProviderConfig() map[string]any {
	supported := func(b bool) map[string]any { return map[string]any{"supported": b} }
	return map[string]any{
		"schemas":        []string{schemaServiceConf},
		"patch":          supported(true),
		"filter":         map[string]any{"supported": true, "maxResults": 200},
		"bulk":           map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"sort":           supported(false),
		"changePassword": supported(false),
		"etag":           supported(false),
		"authenticationSchemes": []any{
			map[string]any{
				"type":        "oauthbearertoken",
				"name":        "OAuth Bearer Token",
				"description": "Authentication via bearer token",
				"specUri":     "http://www.rfc-editor.org/info/rfc6750",
				"primary":     true,
			},
		},
	}
}

func primaryEmail(emails []Email, userName string) string {
	for _, e := range emails {
		if e.Primary && e.Value != "" {
			return e.Value
		}
	}
	for _, e := range emails {
		if e.Value != "" {
			return e.Value
		}
	}
	return userName
}

func displayName(n *Name, userName string) string {
	if n == nil {
		return userName
	}
	if n.Formatted != "" {
		return n.Formatted
	}
	if n.GivenName != "" || n.FamilyName != "" {
		return n.GivenName + " " + n.FamilyName
	}
	return userName
}

func boolVal(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
