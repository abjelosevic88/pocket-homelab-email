package server

import (
	"net/http/httptest"
	"testing"
)

// A Pocket Core link only opens the routes its scopes cover.
func TestLinkScopes(t *testing.T) {
	cases := []struct {
		scopes       []string
		method, path string
		want         bool
	}{
		{[]string{"mail:read"}, "GET", "/api/v1/messages", true},
		{[]string{"mail:read"}, "GET", "/api/v1/messages/12", true},
		{[]string{"mail:read"}, "GET", "/api/v1/threads/12", true},
		{[]string{"mail:read"}, "GET", "/api/v1/counts", true},
		{[]string{"mail:read"}, "GET", "/api/v1/summary/2026-10-25", true},
		{[]string{"mail:read"}, "POST", "/api/v1/actions", false},
		{[]string{"mail:read"}, "POST", "/api/v1/send", false},
		{[]string{"mail:read"}, "GET", "/api/v1/messages/12/raw", false},
		{[]string{"mail:read"}, "GET", "/api/v1/attachments/3", false},
		{[]string{"mail:read"}, "GET", "/api/v1/settings", false},
		{[]string{"mail:send"}, "POST", "/api/v1/send", true},
		{[]string{"mail:send"}, "GET", "/api/v1/messages", false},
		{[]string{"mail:send"}, "POST", "/api/v1/drafts/1/send", false},
		{nil, "GET", "/api/v1/auth/me", true},
		{nil, "GET", "/api/v1/messages", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := linkAllows(c.scopes, r); got != c.want {
			t.Errorf("%v %s %s = %v, want %v", c.scopes, c.method, c.path, got, c.want)
		}
	}
}
