package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
)

// Mail declares Pocket CRM (deals from email, deals with the sender) in its manifest.
func TestCRMUseDeclared(t *testing.T) {
	var got *core.Use
	for _, u := range linkUses() {
		if u.App == "crm" {
			got = &u
		}
	}
	if got == nil || !slices.Equal(got.Scopes, []string{"crm:write", "crm:read"}) || got.Reason == "" {
		t.Fatalf("crm use = %+v", got)
	}
	if a, ok := appBySlug("crm"); !ok || a.name != "Pocket CRM" {
		t.Fatalf("appBySlug(crm) = %+v %v", a, ok)
	}
}

func TestMakeDealInCRM(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) {
		switch h.Path {
		case "/api/v1/deals":
			return 201, map[string]any{"id": 12, "title": h.JSON["title"], "status": "open", "stage_name": "Lead"}
		case "/api/v1/lookup":
			return 200, map[string]any{"url": "https://crm.internal", "deals": []map[string]any{
				{"id": 12, "title": "Your invoice for October", "stage_name": "Lead", "status": "open", "amount": 0, "currency": "EUR"},
				{"id": 3, "title": "Website", "stage_name": "Won", "status": "won", "amount": 4500, "currency": "EUR"},
			}}
		}
		return 404, map[string]any{"error": "not found"}
	})
	e.direct(t, e.alice.ID, "crm", srv.URL)
	path := "/api/v1/messages/" + idPath(e.msg.ID) + "/deal"
	code, out := e.do(t, e.aliceTok, "POST", path, nil)
	if code != 200 || !strings.Contains(out["message"].(string), "Deal made in Pocket CRM") {
		t.Fatalf("deal: %d %v", code, out)
	}
	h := hits()[0]
	if h.Method != "POST" || h.Auth != "Bearer tok-crm" || h.JSON["title"] != "Your invoice for October" || h.JSON["contact_name"] != "Ana Novak" ||
		h.JSON["contact_email"] != "ana@x.org" || h.JSON["source"] != "email" || h.JSON["source_ref"] != "message:"+idPath(e.msg.ID) || h.JSON["note"] != "From an email" {
		t.Fatalf("deal body: %+v", h.JSON)
	}
	if _, ok := h.JSON["company"]; ok {
		t.Fatalf("company is not guessed: %+v", h.JSON)
	}
	desc := h.JSON["description"].(string)
	if !strings.Contains(desc, "Please pay by Friday") || !strings.Contains(desc, "https://mail.example.com/message/"+idPath(e.msg.ID)) {
		t.Fatalf("description: %q", desc)
	}
	if l := out["link"].(map[string]any); l["item_url"] != "https://crm.example/deals/12" || l["app"] != "crm" {
		t.Fatalf("deal link: %v", l)
	}
	// Pressed again: CRM answers with the same deal, Mail keeps one link.
	code, out = e.do(t, e.aliceTok, "POST", path, nil)
	if code != 200 || !strings.Contains(out["message"].(string), "Already") {
		t.Fatalf("deal again: %d %v", code, out)
	}
	links, _ := e.s.store.AppLinksForThread(context.Background(), e.alice.ID, e.msg.ThreadKey, []int64{e.msg.ID})
	if len(links) != 1 || links[0].ItemID != "12" {
		t.Fatalf("links: %+v", links)
	}
	// The deals with the sender, linked to CRM's pages.
	code, out = e.do(t, e.aliceTok, "GET", "/api/v1/apps/crm/deals?addr=ana@x.org", nil)
	deals, _ := out["deals"].([]any)
	if code != 200 || len(deals) != 2 || out["open"] != float64(1) || deals[1].(map[string]any)["url"] != "https://crm.example/deals/3" {
		t.Fatalf("crm deals: %d %v", code, out)
	}
	if lk := hits()[2]; lk.Path != "/api/v1/lookup" || lk.Query != "email=ana%40x.org" {
		t.Fatalf("lookup: %+v", lk)
	}
	// Bob cannot make a deal from Alice's mail.
	e.direct(t, e.bob.ID, "crm", srv.URL)
	if code, _ := e.do(t, e.bobTok, "POST", path, nil); code != http.StatusNotFound {
		t.Fatalf("bob on alice's message: %d", code)
	}
}

// Through Pocket Core: a link that is off or an app that is missing greys the
// action out with the reason in plain words; switched on, the deal is made.
func TestCRMThroughCoreLink(t *testing.T) {
	e := newTestServer(t)
	crm, hits := fakeApp(t, func(h hit) (int, any) { return 201, map[string]any{"id": 7, "title": "x"} })
	var mu sync.Mutex
	reason := "target_missing"
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		why := reason
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/links/status":
			var list []core.LinkState
			if r.URL.Query().Get("app") == "crm" {
				for _, sc := range []string{"crm:write", "crm:read"} {
					list = append(list, core.LinkState{App: "crm", Scope: sc, State: firstNonEmpty(why, "on"), On: why == ""})
				}
			}
			_ = json.NewEncoder(w).Encode(list)
		case "/api/v1/links/token":
			var in struct {
				App    string   `json:"app"`
				Scopes []string `json:"scopes"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			if why != "" || in.App != "crm" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "no link", "reason": firstNonEmpty(why, "not_declared")})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "pcx_crm", "scope": strings.Join(in.Scopes, " "), "app": "crm", "url": crm.URL, "public_url": "https://crm.bjelke.example"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.Close)
	e.s.core = core.New(fake.URL, core.Options{Token: "pca_mail"})
	set := func(r string) { mu.Lock(); reason = r; mu.Unlock() }
	path := "/api/v1/messages/" + idPath(e.msg.ID) + "/deal"

	// CRM not on the box yet: greyed with the hint, the action answers 409 in plain words.
	_, out := e.do(t, e.aliceTok, "GET", "/api/v1/apps", nil)
	st := out["apps"].(map[string]any)["crm"].(map[string]any)
	if st["configured"] != false || st["detail"] != "Pocket CRM is not registered with Pocket Core" {
		t.Fatalf("crm status while missing: %v", st)
	}
	if code, out := e.do(t, e.aliceTok, "POST", path, nil); code != http.StatusConflict || out["error"] != "Pocket CRM is not set up: Pocket CRM is not registered with Pocket Core" {
		t.Fatalf("deal while missing: %d %v", code, out)
	}
	// The link is there but still off in Core → Links.
	set("off_admin")
	if code, out := e.do(t, e.aliceTok, "POST", path, nil); code != http.StatusConflict || !strings.Contains(out["error"].(string), "the Mail → Pocket CRM link is switched off in Pocket Core (Links)") {
		t.Fatalf("deal while off: %d %v", code, out)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/apps", nil)
	if st := out["apps"].(map[string]any)["crm"].(map[string]any); st["configured"] != false || !strings.Contains(st["detail"].(string), "switched off in Pocket Core") {
		t.Fatalf("crm status while off: %v", st)
	}
	if len(hits()) != 0 {
		t.Fatal("CRM was called without a link")
	}
	// Switched on: a five-minute token for crm:write only, and the deal links to CRM's public page.
	set("")
	code, out := e.do(t, e.aliceTok, "POST", path, nil)
	if code != 200 || out["link"].(map[string]any)["item_url"] != "https://crm.bjelke.example/deals/7" {
		t.Fatalf("deal through Core: %d %v", code, out)
	}
	if h := hits()[0]; h.Auth != "Bearer pcx_crm" || h.Path != "/api/v1/deals" {
		t.Fatalf("CRM call: %+v", h)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/apps", nil)
	if st := out["apps"].(map[string]any)["crm"].(map[string]any); st["configured"] != true {
		t.Fatalf("crm status when on: %v", st)
	}
}
