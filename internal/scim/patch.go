package scim

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type patchOp struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type patchRequest struct {
	Schemas    []string  `json:"schemas"`
	Operations []patchOp `json:"Operations"`
}

func decodePatch(r *http.Request) ([]patchOp, error) {
	var req patchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}
	ops := make([]patchOp, 0, len(req.Operations))
	for _, op := range req.Operations {
		op.Op = strings.ToLower(strings.TrimSpace(op.Op))
		if op.Op == "" {
			return nil, fmt.Errorf("patch op required")
		}
		ops = append(ops, op)
	}
	return ops, nil
}

func pageParams(r *http.Request) (start, count int) {
	start = 1
	count = 100
	if v := r.URL.Query().Get("startIndex"); v != "" {
		fmt.Sscanf(v, "%d", &start)
		if start < 1 {
			start = 1
		}
	}
	if v := r.URL.Query().Get("count"); v != "" {
		fmt.Sscanf(v, "%d", &count)
		if count < 0 {
			count = 0
		}
		if count > 200 {
			count = 200
		}
	}
	return start, count
}

func paginate[T any](in []T, start, count int) (page []T, total int) {
	total = len(in)
	if count == 0 {
		return nil, total
	}
	i := start - 1
	if i < 0 {
		i = 0
	}
	if i >= len(in) {
		return nil, total
	}
	j := i + count
	if j > len(in) {
		j = len(in)
	}
	return in[i:j], total
}
