package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/secret"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// testEnv is a server with two people (Alice admin, Bob member), API tokens
// for both, and one demo account of Alice's with a message carrying a PDF
// and a PNG (bytes kept locally, so nothing needs a mail server).
type testEnv struct {
	s                *Server
	h                http.Handler
	alice, bob       *store.User
	aliceTok, bobTok string
	acct             store.Account
	inbox, sent      store.Folder
	msg              store.Message
	pdf, png         store.Attachment
	bobAcct          store.Account
}

func newTestServer(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.BaseURL = "https://mail.example.com"
	cfg.MaxAttachmentMB = 25
	st := openTestStore(t, cfg)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := New(cfg, st, log, nil)
	s.box = secret.New(make([]byte, 32))
	s.mail = mail.New(st, s.box, mail.Config{}, log)
	ctx := context.Background()
	e := &testEnv{s: s, h: s.Router()}
	e.alice = &store.User{Email: "alice@example.com", Name: "Alice Archer", Role: "admin", PasswordHash: "x"}
	e.bob = &store.User{Email: "bob@example.com", Name: "Bob", Role: "member", PasswordHash: "x"}
	for _, u := range []*store.User{e.alice, e.bob} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	if e.aliceTok, _, err = st.CreateAPIToken(ctx, e.alice.ID, "t"); err != nil {
		t.Fatal(err)
	}
	if e.bobTok, _, err = st.CreateAPIToken(ctx, e.bob.ID, "t"); err != nil {
		t.Fatal(err)
	}
	e.acct = store.Account{UserID: e.alice.ID, Name: "Alice Archer", Email: "alice@example.com", Provider: "demo", SyncEnabled: true}
	if err := st.CreateAccount(ctx, &e.acct); err != nil {
		t.Fatal(err)
	}
	e.bobAcct = store.Account{UserID: e.bob.ID, Name: "Bob", Email: "bob@example.com", Provider: "demo", SyncEnabled: true}
	if err := st.CreateAccount(ctx, &e.bobAcct); err != nil {
		t.Fatal(err)
	}
	e.inbox = store.Folder{UserID: e.alice.ID, AccountID: e.acct.ID, Name: "INBOX", Display: "Inbox", Role: "inbox", Sync: true, Selectable: true}
	e.sent = store.Folder{UserID: e.alice.ID, AccountID: e.acct.ID, Name: "Sent", Display: "Sent", Role: "sent", Sync: true, Selectable: true}
	for _, f := range []*store.Folder{&e.inbox, &e.sent} {
		if err := st.UpsertFolder(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	e.msg = store.Message{UserID: e.alice.ID, AccountID: e.acct.ID, FolderID: e.inbox.ID, UID: 1, MessageID: "inv-1@shop.example",
		Subject: "Your invoice for October", From: store.Address{Name: "Ana Novak", Addr: "ana@x.org"}, To: []store.Address{{Name: "Alice", Addr: "alice@example.com"}},
		Date: "2026-10-20T09:30:00Z", BodyText: "Hi Alice, the invoice is attached. Please pay by Friday.", BodyFetched: true, HasAttachments: true}
	atts := []store.Attachment{
		{Part: "2", Filename: "invoice.pdf", MIME: "application/pdf", Size: 9, Data: []byte("%PDF-test")},
		{Part: "3", Filename: "photo.png", MIME: "image/png", Size: 4, Data: []byte("\x89PNG")},
	}
	if err := st.InsertMessage(ctx, &e.msg, atts); err != nil {
		t.Fatal(err)
	}
	e.pdf, e.png = atts[0], atts[1]
	sentMsg := store.Message{UserID: e.alice.ID, AccountID: e.acct.ID, FolderID: e.sent.ID, UID: 1, MessageID: "s1@example.com", Subject: "Hello",
		From: store.Address{Addr: "alice@example.com"}, To: []store.Address{{Name: "Ana N.", Addr: "ana@x.org"}, {Name: "Zoran", Addr: "zoran@y.org"}}, Date: "2026-10-19T09:00:00Z", Seen: true}
	if err := st.InsertMessage(ctx, &sentMsg, nil); err != nil {
		t.Fatal(err)
	}
	return e
}

// do calls the API as a person (tok) and decodes a JSON answer.
func (e *testEnv) do(t *testing.T, tok, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// direct points one of Alice's Pocket apps at a fake server.
func (e *testEnv) direct(t *testing.T, uid int64, slug, url string) {
	t.Helper()
	ctx := context.Background()
	st, _ := e.s.store.GetSettings(ctx, uid)
	st.SetApp(slug, store.AppConn{Mode: modeDirect, URL: url, Token: "tok-" + slug, PublicURL: "https://" + slug + ".example"})
	if err := e.s.store.SaveUserSettings(ctx, uid, st); err != nil {
		t.Fatal(err)
	}
}

type hit struct {
	Method, Path, Query string
	Auth, APIKey        string
	Fields              map[string]string
	FileName            string
	File                []byte
	JSON                map[string]any
	Raw                 []byte
}

// fakeApp records every request and answers with reply(path).
func fakeApp(t *testing.T, reply func(h hit) (int, any)) (*httptest.Server, func() []hit) {
	t.Helper()
	var mu sync.Mutex
	var hits []hit
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := hit{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, Auth: r.Header.Get("Authorization"), APIKey: r.Header.Get("x-api-key"), Fields: map[string]string{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			_ = r.ParseMultipartForm(10 << 20)
			for k, v := range r.MultipartForm.Value {
				h.Fields[k] = v[0]
			}
			for _, fs := range r.MultipartForm.File {
				f, _ := fs[0].Open()
				h.File, _ = io.ReadAll(f)
				h.FileName = fs[0].Filename
				f.Close()
			}
		} else if r.Body != nil {
			h.Raw, _ = io.ReadAll(r.Body)
			_ = json.Unmarshal(h.Raw, &h.JSON)
		}
		mu.Lock()
		hits = append(hits, h)
		mu.Unlock()
		code, v := reply(h)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []hit {
		mu.Lock()
		defer mu.Unlock()
		return append([]hit(nil), hits...)
	}
}

func idPath(id int64) string { return strconv.FormatInt(id, 10) }

func TestSaveAttachmentToDocuments(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) {
		if h.Path == "/api/v1/documents" {
			return 201, map[string]any{"id": 7, "title": h.Fields["title"]}
		}
		return 201, map[string]any{"id": 1}
	})
	e.direct(t, e.alice.ID, "documents", srv.URL)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/save", map[string]any{"app": "documents", "attachment_ids": []int64{e.pdf.ID}})
	if code != 200 {
		t.Fatalf("save: %d %v", code, out)
	}
	hs := hits()
	if len(hs) != 2 {
		t.Fatalf("want upload + note, got %+v", hs)
	}
	up := hs[0]
	if up.Auth != "Bearer tok-documents" || up.FileName != "invoice.pdf" || string(up.File) != "%PDF-test" || up.Fields["title"] != "invoice" || up.Fields["created"] != "2026-10-20" {
		t.Fatalf("upload: %+v", up)
	}
	if hs[1].Path != "/api/v1/documents/7/notes" || !strings.Contains(hs[1].JSON["text"].(string), "Ana Novak") || !strings.Contains(hs[1].JSON["text"].(string), "https://mail.example.com/message/") {
		t.Fatalf("note: %+v", hs[1])
	}
	links, _ := e.s.store.AppLinksForThread(context.Background(), e.alice.ID, e.msg.ThreadKey, []int64{e.msg.ID})
	if len(links) != 1 || links[0].App != "documents" || links[0].ItemURL != "https://documents.example/documents/7" || links[0].ItemID != "7" {
		t.Fatalf("links: %+v", links)
	}
	// the whole email as a document
	if code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/save", map[string]any{"app": "documents", "whole": true}); code != 200 {
		t.Fatalf("save whole: %d %v", code, out)
	}
	last := hits()[2]
	if !strings.HasSuffix(last.FileName, ".eml") && !strings.HasSuffix(last.FileName, ".html") {
		t.Fatalf("whole email file: %q", last.FileName)
	}
}

func TestMakeTaskAndNote(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) {
		if h.Path == "/api/v1/tasks" {
			return 201, map[string]any{"id": 5, "title": h.JSON["title"]}
		}
		return 201, map[string]any{"id": 42, "title": h.JSON["title"]}
	})
	e.direct(t, e.alice.ID, "tasks", srv.URL)
	e.direct(t, e.alice.ID, "notes", srv.URL)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/task", map[string]any{"due": "2026-10-24"})
	if code != 200 {
		t.Fatalf("task: %d %v", code, out)
	}
	h := hits()[0]
	if h.JSON["title"] != "Your invoice for October" || h.JSON["due"] != "2026-10-24" || !strings.HasPrefix(h.JSON["url"].(string), "https://mail.example.com/message/") {
		t.Fatalf("task body: %+v", h.JSON)
	}
	if l := out["link"].(map[string]any); l["item_url"] != "https://tasks.example/inbox?task=5" {
		t.Fatalf("task link: %v", l)
	}
	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/note", nil)
	if code != 200 {
		t.Fatalf("note: %d %v", code, out)
	}
	n := hits()[1]
	if n.Path != "/api/v1/notes" || n.JSON["category"] != "Mail" || !strings.Contains(n.JSON["content"].(string), "Please pay by Friday") {
		t.Fatalf("note body: %+v", n.JSON)
	}
	if l := out["link"].(map[string]any); l["item_url"] != "https://notes.example/note/42" {
		t.Fatalf("note link: %v", l)
	}
}

func TestSavePhotosToImmich(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) { return 201, map[string]any{"id": "asset-1", "status": "created"} })
	ctx := context.Background()
	st, _ := e.s.store.GetSettings(ctx, e.alice.ID)
	st.ImmichMode, st.ImmichURL, st.ImmichKey = modeDirect, srv.URL, "imm-key"
	if err := e.s.store.SaveUserSettings(ctx, e.alice.ID, st); err != nil {
		t.Fatal(err)
	}
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/save", map[string]any{"app": "photos"})
	if code != 200 {
		t.Fatalf("photos: %d %v", code, out)
	}
	hs := hits()
	if len(hs) != 1 || hs[0].Path != "/api/assets" || hs[0].APIKey != "imm-key" || hs[0].FileName != "photo.png" || hs[0].Fields["fileCreatedAt"] == "" {
		t.Fatalf("immich upload: %+v", hs)
	}
	links := out["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["item_url"] != srv.URL+"/photos/asset-1" {
		t.Fatalf("photo links: %v", links)
	}
}

func TestRecipientsMergeContactsAndMail(t *testing.T) {
	e := newTestServer(t)
	srv, _ := fakeApp(t, func(h hit) (int, any) {
		return 200, []map[string]any{{"id": 1, "name": "Ana Novak", "email": "ANA@x.org", "photo_url": "/api/v1/contacts/1/photo"}, {"id": 2, "name": "Nina", "email": "nina@x.org"}, {"id": 3, "name": "Phone only", "email": ""}}
	})
	e.direct(t, e.alice.ID, "contacts", srv.URL)
	code, out := e.do(t, e.aliceTok, "GET", "/api/v1/recipients?q=a", nil)
	if code != 200 {
		t.Fatalf("recipients: %d %v", code, out)
	}
	people := out["people"].([]any)
	seen := map[string]int{}
	for _, p := range people {
		seen[strings.ToLower(p.(map[string]any)["addr"].(string))]++
	}
	first := people[0].(map[string]any)
	if first["source"] != "contacts" || first["photo_url"] != "https://contacts.example/api/v1/contacts/1/photo" || seen["ana@x.org"] != 1 || seen["nina@x.org"] != 1 || seen[""] != 0 {
		t.Fatalf("people: %v", people)
	}
	// without Contacts the mail itself still suggests
	e.direct(t, e.alice.ID, "contacts", "")
	st, _ := e.s.store.GetSettings(context.Background(), e.alice.ID)
	st.SetApp("contacts", store.AppConn{Mode: modeOff})
	_ = e.s.store.SaveUserSettings(context.Background(), e.alice.ID, st)
	_, out = e.do(t, e.aliceTok, "GET", "/api/v1/recipients?q=zor", nil)
	if p := out["people"].([]any); len(p) != 1 || p[0].(map[string]any)["addr"] != "zoran@y.org" || p[0].(map[string]any)["source"] != "mail" {
		t.Fatalf("mail-only people: %v", p)
	}
}

func TestAppActionsNotConnected(t *testing.T) {
	e := newTestServer(t)
	st, _ := e.s.store.GetSettings(context.Background(), e.alice.ID)
	st.SetApp("documents", store.AppConn{Mode: modeOff})
	_ = e.s.store.SaveUserSettings(context.Background(), e.alice.ID, st)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/save", map[string]any{"app": "documents"})
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "switched off") {
		t.Fatalf("off: %d %v", code, out)
	}
	code, out = e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/task", nil)
	if code != http.StatusConflict || !strings.Contains(out["error"].(string), "Pocket Tasks is not set up") {
		t.Fatalf("not set up: %d %v", code, out)
	}
	code, out = e.do(t, e.aliceTok, "GET", "/api/v1/apps", nil)
	apps := out["apps"].(map[string]any)
	if code != 200 || apps["tasks"].(map[string]any)["configured"] != false || apps["photos"] == nil || out["core"] != false {
		t.Fatalf("apps status: %d %v", code, out)
	}
}

func TestAppActionsCrossUser404(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) { return 201, map[string]any{"id": 1} })
	e.direct(t, e.bob.ID, "documents", srv.URL)
	e.direct(t, e.bob.ID, "tasks", srv.URL)
	for _, p := range []string{"/save", "/task", "/note", "/event", "/contact"} {
		code, _ := e.do(t, e.bobTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+p, map[string]any{"app": "documents"})
		if code != http.StatusNotFound {
			t.Fatalf("bob %s on alice's message: %d", p, code)
		}
	}
	if len(hits()) != 0 {
		t.Fatal("bob reached an app with alice's mail")
	}
	d := store.Draft{UserID: e.alice.ID, AccountID: e.acct.ID, Subject: "x"}
	if err := e.s.store.CreateDraft(context.Background(), &d); err != nil {
		t.Fatal(err)
	}
	e.direct(t, e.bob.ID, "files", srv.URL)
	if code, _ := e.do(t, e.bobTok, "POST", "/api/v1/drafts/"+idPath(d.ID)+"/files/from", map[string]any{"app": "files", "ref": "a.txt"}); code != http.StatusNotFound {
		t.Fatalf("bob attached to alice's draft: %d", code)
	}
}

func TestAttachFromFiles(t *testing.T) {
	e := newTestServer(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/fs/raw" || r.URL.Query().Get("path") != "Docs/plan.txt" || r.Header.Get("Authorization") != "Bearer tok-files" {
			http.Error(w, `{"error":"no"}`, 404)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("the plan"))
	}))
	defer srv.Close()
	e.direct(t, e.alice.ID, "files", srv.URL)
	d := store.Draft{UserID: e.alice.ID, AccountID: e.acct.ID, Subject: "x"}
	if err := e.s.store.CreateDraft(context.Background(), &d); err != nil {
		t.Fatal(err)
	}
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/drafts/"+idPath(d.ID)+"/files/from", map[string]any{"app": "files", "ref": "Docs/plan.txt"})
	if code != 200 || out["filename"] != "plan.txt" || out["size"].(float64) != 8 {
		t.Fatalf("attach: %d %v", code, out)
	}
	f, err := e.s.store.GetDraftFile(context.Background(), e.alice.ID, int64(out["id"].(float64)))
	if err != nil || string(f.Data) != "the plan" {
		t.Fatalf("draft file: %v %q", err, f.Data)
	}
}

func TestMakeExpenseInMinorUnits(t *testing.T) {
	e := newTestServer(t)
	srv, hits := fakeApp(t, func(h hit) (int, any) {
		if h.Method == "GET" {
			return 200, []map[string]any{{"id": 3, "name": "Card", "currency": "EUR"}, {"id": 4, "name": "KM cash", "currency": "BAM"}}
		}
		return 201, map[string]any{"id": 99}
	})
	e.direct(t, e.alice.ID, "money", srv.URL)
	code, out := e.do(t, e.aliceTok, "POST", "/api/v1/messages/"+idPath(e.msg.ID)+"/expense", map[string]any{"amount": 12.5, "currency": "bam"})
	if code != 200 {
		t.Fatalf("expense: %d %v", code, out)
	}
	tx := hits()[1].JSON
	if tx["amount"].(float64) != 1250 || tx["account_id"].(float64) != 4 || tx["currency"] != "BAM" || tx["kind"] != "expense" || tx["date"] != "2026-10-20" {
		t.Fatalf("transaction: %v", tx)
	}
	if l := out["link"].(map[string]any); l["item_url"] != "https://money.example/transactions/99" {
		t.Fatalf("expense link: %v", l)
	}
}

func TestQuietHours(t *testing.T) {
	at := func(h int) (st store.Settings, now timeOfDay) {
		return store.Settings{QuietFrom: 22, QuietTo: 7}, timeOfDay(h)
	}
	for h, want := range map[int]bool{23: true, 3: true, 7: false, 12: false, 22: true} {
		st, now := at(h)
		if got := appQuiet(st, now.t()); got != want {
			t.Fatalf("hour %d: quiet=%v", h, got)
		}
	}
	if appQuiet(store.Settings{QuietFrom: 5, QuietTo: 5}, timeOfDay(5).t()) {
		t.Fatal("equal hours = no quiet hours")
	}
}

type timeOfDay int

func (h timeOfDay) t() time.Time { return time.Date(2026, 1, 1, int(h), 0, 0, 0, time.Local) }
