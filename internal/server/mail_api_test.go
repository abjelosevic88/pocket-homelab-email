package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func idOf(v any) int64 {
	f, _ := v.(float64)
	return int64(f)
}

// Bob can see, change or answer none of Alice's mail, accounts, drafts or folders.
func TestMailCrossUserIsolation(t *testing.T) {
	e := newTestServer(t)
	ctx := context.Background()
	d := store.Draft{UserID: e.alice.ID, AccountID: e.acct.ID, Subject: "secret plan", BodyHTML: "<p>x</p>"}
	if err := e.s.store.CreateDraft(ctx, &d); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		method, path string
		body         any
	}{
		{"GET", fmt.Sprintf("/api/v1/messages/%d", e.msg.ID), nil},
		{"GET", fmt.Sprintf("/api/v1/threads/%d", e.msg.ID), nil},
		{"GET", fmt.Sprintf("/api/v1/messages/%d/raw", e.msg.ID), nil},
		{"GET", fmt.Sprintf("/api/v1/attachments/%d", e.pdf.ID), nil},
		{"GET", fmt.Sprintf("/api/v1/messages?folder=%d", e.inbox.ID), nil},
		{"GET", fmt.Sprintf("/api/v1/accounts/%d", e.acct.ID), nil},
		{"PUT", fmt.Sprintf("/api/v1/accounts/%d", e.acct.ID), map[string]any{"label": "mine now"}},
		{"DELETE", fmt.Sprintf("/api/v1/accounts/%d", e.acct.ID), nil},
		{"POST", fmt.Sprintf("/api/v1/accounts/%d/sync", e.acct.ID), nil},
		{"PUT", fmt.Sprintf("/api/v1/folders/%d", e.inbox.ID), map[string]any{"sync": false}},
		{"POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "trash"}},
		{"POST", "/api/v1/compose", map[string]any{"kind": "reply", "message_id": e.msg.ID}},
		{"GET", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil},
		{"PUT", fmt.Sprintf("/api/v1/drafts/%d", d.ID), map[string]any{"subject": "hijacked"}},
		{"POST", fmt.Sprintf("/api/v1/drafts/%d/send", d.ID), nil},
		{"DELETE", fmt.Sprintf("/api/v1/drafts/%d", d.ID), nil},
	}
	for _, c := range cases {
		code, _ := e.do(t, e.bobTok, c.method, c.path, c.body)
		if code != http.StatusNotFound {
			t.Errorf("Bob %s %s = %d, want 404", c.method, c.path, code)
		}
	}
	_, out := e.do(t, e.bobTok, "GET", "/api/v1/messages?box=all", nil)
	if list, _ := out["messages"].([]any); len(list) != 0 {
		t.Fatalf("Bob's views show %d of Alice's messages", len(list))
	}
	_, out = e.do(t, e.bobTok, "GET", "/api/v1/messages?q=invoice", nil)
	if list, _ := out["messages"].([]any); len(list) != 0 {
		t.Fatal("Bob's search finds Alice's mail")
	}
	// still Alice's, untouched
	m, err := e.s.store.GetMessage(ctx, e.alice.ID, e.msg.ID)
	if err != nil || m.FolderID != e.inbox.ID {
		t.Fatalf("Alice's message moved: %v %+v", err, m)
	}
	if got, _ := e.s.store.GetDraft(ctx, e.alice.ID, d.ID); got.Subject != "secret plan" || got.Status != "draft" {
		t.Fatalf("Alice's draft changed: %+v", got)
	}
	if a, _ := e.s.store.GetAccount(ctx, e.alice.ID, e.acct.ID); a.Label != "" {
		t.Fatal("Bob relabelled Alice's account")
	}
}

func TestMailReadThreadAndActions(t *testing.T) {
	e := newTestServer(t)
	code, out := e.do(t, e.aliceTok, "GET", fmt.Sprintf("/api/v1/threads/%d", e.msg.ID), nil)
	if code != 200 {
		t.Fatalf("thread %d %v", code, out)
	}
	msgs, _ := out["messages"].([]any)
	if len(msgs) != 1 || out["subject"] != "Your invoice for October" {
		t.Fatalf("thread %v", out)
	}
	atts := msgs[0].(map[string]any)["attachments"].([]any)
	if len(atts) != 2 {
		t.Fatalf("attachments %v", atts)
	}
	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "star"})
	if code != 200 || out["ok"] != true {
		t.Fatalf("star %d %v", code, out)
	}
	if m, _ := e.s.store.GetMessage(context.Background(), e.alice.ID, e.msg.ID); !m.Flagged {
		t.Fatal("not starred")
	}
	until := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	code, _ = e.do(t, e.aliceTok, "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "snooze", "until": until})
	if code != 200 {
		t.Fatalf("snooze %d", code)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/messages?box=inbox", nil)
	if list, _ := out["messages"].([]any); len(list) != 0 {
		t.Fatal("a snoozed message is still in the inbox")
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/messages?box=snoozed", nil)
	if list, _ := out["messages"].([]any); len(list) != 1 {
		t.Fatal("the snoozed view is empty")
	}
	code, _ = e.do(t, e.aliceTok, "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "snooze", "until": "2001-01-01T00:00:00Z"})
	if code != 400 {
		t.Fatalf("snooze into the past = %d", code)
	}
	code, _ = e.do(t, e.aliceTok, "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "explode"})
	if code != 400 {
		t.Fatalf("unknown action = %d", code)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/counts", nil)
	if idOf(out["starred"]) != 1 || idOf(out["snoozed"]) != 1 {
		t.Fatalf("counts %v", out)
	}
}

func TestMailComposeReplySendUndoSchedule(t *testing.T) {
	e := newTestServer(t)
	code, d := e.do(t, e.aliceTok, "POST", "/api/v1/compose", map[string]any{"kind": "reply", "message_id": e.msg.ID})
	if code != 201 {
		t.Fatalf("compose %d %v", code, d)
	}
	to := d["to"].([]any)
	if d["subject"] != "Re: Your invoice for October" || len(to) != 1 || to[0].(map[string]any)["addr"] != "ana@x.org" {
		t.Fatalf("reply prefilled wrong: %v", d)
	}
	if !strings.Contains(d["body_html"].(string), "wrote:") {
		t.Fatalf("no quote: %v", d["body_html"])
	}
	id := idOf(d["id"])
	code, d = e.do(t, e.aliceTok, "PUT", fmt.Sprintf("/api/v1/drafts/%d", id), map[string]any{"body_html": `<p onclick="x()">Thanks!<script>alert(1)</script></p>`})
	if code != 200 || strings.Contains(d["body_html"].(string), "script") || strings.Contains(d["body_html"].(string), "onclick") {
		t.Fatalf("draft body not cleaned: %d %v", code, d["body_html"])
	}
	code, res := e.do(t, e.aliceTok, "POST", fmt.Sprintf("/api/v1/drafts/%d/send", id), nil)
	if code != 200 || res["undo_until"] == "" {
		t.Fatalf("send %d %v", code, res)
	}
	if dr := res["draft"].(map[string]any); dr["status"] != "queued" || dr["message_id"] == "" {
		t.Fatalf("not queued: %v", dr)
	}
	code, d = e.do(t, e.aliceTok, "POST", fmt.Sprintf("/api/v1/drafts/%d/cancel", id), nil)
	if code != 200 || d["status"] != "draft" {
		t.Fatalf("undo send %d %v", code, d)
	}
	later := time.Now().Add(20 * time.Hour).UTC().Format(time.RFC3339)
	code, res = e.do(t, e.aliceTok, "POST", fmt.Sprintf("/api/v1/drafts/%d/send", id), map[string]any{"send_at": later})
	if code != 200 {
		t.Fatalf("schedule %d %v", code, res)
	}
	_, out := e.do(t, e.aliceTok, "GET", "/api/v1/drafts?status=queued", nil)
	if list, _ := out["drafts"].([]any); len(list) != 1 {
		t.Fatalf("scheduled list %v", out)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/counts", nil)
	if idOf(out["scheduled"]) != 1 {
		t.Fatalf("counts %v", out)
	}
	// nobody to send to
	code, d = e.do(t, e.aliceTok, "POST", "/api/v1/compose", map[string]any{"kind": "new"})
	if code != 201 {
		t.Fatalf("new draft %d", code)
	}
	if code, _ = e.do(t, e.aliceTok, "POST", fmt.Sprintf("/api/v1/drafts/%d/send", idOf(d["id"])), nil); code != 400 {
		t.Fatalf("sending without recipients = %d", code)
	}
}

func TestMailForwardCopiesAttachments(t *testing.T) {
	e := newTestServer(t)
	code, d := e.do(t, e.aliceTok, "POST", "/api/v1/compose", map[string]any{"kind": "forward", "message_id": e.msg.ID})
	if code != 201 {
		t.Fatalf("forward %d %v", code, d)
	}
	if files, _ := d["files"].([]any); len(files) != 2 || d["subject"] != "Fwd: Your invoice for October" {
		t.Fatalf("forward %v", d)
	}
}

func TestMailAttachmentNeverRunsOnThisOrigin(t *testing.T) {
	e := newTestServer(t)
	ctx := context.Background()
	m := store.Message{UserID: e.alice.ID, AccountID: e.acct.ID, FolderID: e.inbox.ID, UID: 9, MessageID: "h@x", Subject: "page", Date: "2026-10-21T10:00:00Z", BodyFetched: true}
	atts := []store.Attachment{{Part: "2", Filename: "page.html", MIME: "text/html", Data: []byte("<script>alert(1)</script>")}}
	if err := e.s.store.InsertMessage(ctx, &m, atts); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/attachments/%d", atts[0].ID), nil)
	req.Header.Set("Authorization", "Bearer "+e.aliceTok)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "attachment") || rec.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("html attachment served unsafely: %d %v", rec.Code, rec.Header())
	}
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/v1/attachments/%d", e.png.ID), nil)
	req.Header.Set("Authorization", "Bearer "+e.aliceTok)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if !strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") {
		t.Fatalf("image not shown inline: %v", rec.Header())
	}
}

func TestMailRulesTemplatesSendersSubscriptions(t *testing.T) {
	e := newTestServer(t)
	ctx := context.Background()
	if code, _ := e.do(t, e.aliceTok, "POST", "/api/v1/rules", map[string]any{"actions": map[string]any{"star": true}}); code != 400 {
		t.Fatalf("rule without a match = %d", code)
	}
	code, r := e.do(t, e.aliceTok, "POST", "/api/v1/rules", map[string]any{"enabled": true, "match": map[string]any{"from": "ana@x.org"}, "actions": map[string]any{"save_to": []string{"documents"}}})
	if code != 201 || r["name"] != "From ana@x.org" {
		t.Fatalf("rule %d %v", code, r)
	}
	if code, _ := e.do(t, e.bobTok, "DELETE", fmt.Sprintf("/api/v1/rules/%d", idOf(r["id"])), nil); code != 404 {
		t.Fatalf("Bob deleted Alice's rule: %d", code)
	}
	code, tpl := e.do(t, e.aliceTok, "POST", "/api/v1/templates", map[string]any{"name": "Thanks", "body_html": "<p>Thanks!</p><img src=x onerror=alert(1)>"})
	if code != 201 || strings.Contains(tpl["body_html"].(string), "onerror") {
		t.Fatalf("template %d %v", code, tpl)
	}
	code, sd := e.do(t, e.aliceTok, "PUT", "/api/v1/senders/Ana@X.org", map[string]any{"vip": true})
	if code != 200 || sd["vip"] != true || sd["addr"] != "ana@x.org" {
		t.Fatalf("sender %d %v", code, sd)
	}
	_, out := e.do(t, e.aliceTok, "GET", "/api/v1/messages?box=vip", nil)
	if list, _ := out["messages"].([]any); len(list) != 1 {
		t.Fatalf("VIP view %v", out)
	}
	n := store.Message{UserID: e.alice.ID, AccountID: e.acct.ID, FolderID: e.inbox.ID, UID: 20, MessageID: "n1@news", Subject: "Weekly", Category: "newsletters",
		From: store.Address{Name: "News", Addr: "news@letter.example"}, Date: "2026-10-22T08:00:00Z", ListUnsubscribe: "<https://letter.example/u>", UnsubscribePost: true}
	if err := e.s.store.InsertMessage(ctx, &n, nil); err != nil {
		t.Fatal(err)
	}
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/subscriptions", nil)
	subs, _ := out["subscriptions"].([]any)
	if len(subs) != 1 || subs[0].(map[string]any)["one_click"] != true {
		t.Fatalf("subscriptions %v", out)
	}
	_, out = e.do(t, e.bobTok, "GET", "/api/v1/subscriptions", nil)
	if subs, _ := out["subscriptions"].([]any); len(subs) != 0 {
		t.Fatal("Bob sees Alice's subscriptions")
	}
}

func TestMailOAuthNeedsSetupAndAccountsValidate(t *testing.T) {
	e := newTestServer(t)
	if code, _ := e.do(t, e.aliceTok, "GET", "/api/v1/oauth/google/start?email=a@gmail.com", nil); code != http.StatusConflict {
		t.Fatalf("oauth without a client = %d", code)
	}
	if code, _ := e.do(t, e.aliceTok, "GET", "/api/v1/oauth/yahoo/start", nil); code != 404 {
		t.Fatalf("unknown oauth provider = %d", code)
	}
	if code, _ := e.do(t, e.aliceTok, "GET", "/api/v1/accounts/discover?email=nope", nil); code != 400 {
		t.Fatalf("discover bad address = %d", code)
	}
	if code, _ := e.do(t, e.aliceTok, "POST", "/api/v1/accounts", map[string]any{"email": "x@example.com", "provider": "demo", "password": "p"}); code != 400 {
		t.Fatalf("demo accounts from the API = %d", code)
	}
	if code, _ := e.do(t, e.aliceTok, "POST", "/api/v1/accounts", map[string]any{"email": "alice@example.com", "password": "p"}); code != 409 {
		t.Fatalf("same address twice = %d", code)
	}
	_, st := e.do(t, e.bobTok, "GET", "/api/v1/settings", nil)
	if _, ok := st["google_client_id"]; ok {
		t.Fatal("a member sees the OAuth client")
	}
	if code, out := e.do(t, e.bobTok, "PUT", "/api/v1/settings", map[string]any{"google_client_id": "evil"}); code != 200 {
		t.Fatalf("settings %d %v", code, out)
	}
	if box, _ := e.s.store.GetSettings(context.Background(), 0); box.GoogleClientID != "" {
		t.Fatal("a member changed the box's OAuth client")
	}
}

func TestMailExportImportIdempotent(t *testing.T) {
	e := newTestServer(t)
	ctx := context.Background()
	_ = e.s.store.SaveRule(ctx, &store.Rule{UserID: e.alice.ID, Name: "R", Enabled: true, Match: store.RuleMatch{From: "a"}, Actions: store.RuleActions{Star: true}})
	_ = e.s.store.SaveTemplate(ctx, &store.Template{UserID: e.alice.ID, Name: "T", BodyHTML: "<p>t</p>"})
	req := httptest.NewRequest("GET", "/api/v1/export.json", nil)
	req.Header.Set("Authorization", "Bearer "+e.aliceTok)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), `"secret"`) {
		t.Fatalf("export %d", rec.Code)
	}
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	for i, want := range []float64{1, 0} {
		b, _ := json.Marshal(doc)
		req := httptest.NewRequest("POST", "/api/v1/import", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+e.bobTok)
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		var counts map[string]float64
		_ = json.Unmarshal(rec.Body.Bytes(), &counts)
		if counts["rules"] != want || counts["templates"] != want {
			t.Fatalf("import %d: %v", i, counts)
		}
	}
	accts, _ := e.s.store.ListAccounts(ctx, e.bob.ID)
	for _, a := range accts {
		if a.Email == "alice@example.com" {
			t.Fatal("a demo account was imported")
		}
	}
}
