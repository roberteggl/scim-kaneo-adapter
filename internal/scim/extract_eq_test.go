package scim

import "testing"

func TestExtractEqValueAuthentikPath(t *testing.T) {
	uid := "7154f570-4dc5-46e6-95e6-d64378e24e37"
	got := extractEqValue(`members[value eq "` + uid + `"]`)
	if got != uid {
		t.Fatalf("got %q want %q", got, uid)
	}
	if extractEqValue(`members[value eq "`+uid+`"]`) != uid {
		t.Fatal("quoted path failed")
	}
}
