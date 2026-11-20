package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// moveEnv is the test server behind a fake Core whose pcx_<who>_<export|import|read>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newTestServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "mail:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"mail","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(fake.Close)
	e.s.core = core.New(fake.URL, core.Options{Token: "pca_mail", DB: e.s.store.DB})
	return e
}

func (e *testEnv) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func TestMoveLinkScopes(t *testing.T) {
	e := moveEnv(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/messages", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/rules", 403},
		{imp, "POST", "/api/v1/send", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := e.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := e.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 2 || p.Exports[0].Kind != core.KindPocket || len(p.Imports) != 1 {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's setup to Bob: the import adds once, no secret leaves,
// and nothing of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	e := moveEnv(t)
	ctx := context.Background()
	work := store.Account{UserID: e.alice.ID, Name: "Alice", Email: "alice@work.example", Provider: "imap", IMAPHost: "imap.work.example", Secret: "sealed-password", AccessToken: "sealed-token"}
	if err := e.s.store.CreateAccount(ctx, &work); err != nil {
		t.Fatal(err)
	}
	if err := e.s.store.SaveRule(ctx, &store.Rule{UserID: e.alice.ID, Name: "Receipts", Enabled: true, Match: store.RuleMatch{From: "shop"}}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.store.SaveTemplate(ctx, &store.Template{UserID: e.alice.ID, Name: "Thanks", Subject: "Thank you"}); err != nil {
		t.Fatal(err)
	}
	if err := e.s.store.PutSender(ctx, store.Sender{UserID: e.alice.ID, Addr: "ana@x.org", VIP: true}); err != nil {
		t.Fatal(err)
	}

	code, export := e.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "alice@work.example") || !strings.Contains(string(export), "Receipts") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if strings.Contains(string(export), "sealed-") {
		t.Fatalf("the export holds a secret: %s", export)
	}
	if _, bob := e.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), "alice@work.example") || strings.Contains(string(bob), "Receipts") {
		t.Fatalf("bob's export holds alice's setup: %s", bob)
	}

	imp := func() map[string]any {
		code, out := e.raw("pcx_bob_import", "POST", "/api/v1/import", "application/json", export)
		if code != 200 {
			t.Fatalf("import → %d %s", code, out)
		}
		var m map[string]any
		_ = json.Unmarshal(out, &m)
		return m
	}
	n := func(m map[string]any, k string) int { v, _ := m[k].(float64); return int(v) }
	first := imp()
	if n(first, "accounts") != 1 || n(first, "rules") != 1 || n(first, "templates") != 1 || n(first, "senders") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := imp()
	if n(second, "accounts")+n(second, "rules")+n(second, "templates")+n(second, "senders") != 0 || n(second, "skipped") == 0 {
		t.Fatalf("second import added: %v", second)
	}
	accts, _ := e.s.store.ListAccounts(ctx, e.bob.ID)
	rules, _ := e.s.store.ListRules(ctx, e.bob.ID, false)
	if len(accts) != 2 || len(rules) != 1 {
		t.Fatalf("bob has %d accounts, %d rules", len(accts), len(rules))
	}
	for _, a := range accts {
		if a.Email == "alice@work.example" && a.Secret != "" {
			t.Fatal("an imported account came with a secret")
		}
	}
	if rules, _ := e.s.store.ListRules(ctx, e.alice.ID, false); len(rules) != 1 {
		t.Fatalf("alice's rules changed: %d", len(rules))
	}
}
