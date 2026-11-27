package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|passes|send> from Pocket
// Hub and remembers the manifest Mail registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func capEnv(t *testing.T) (*testEnv, *fakeCapCore) {
	t.Helper()
	e := newTestServer(t)
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "mail:read", "passes": "mail:passes", "send": "mail:send"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "mail", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	e.s.cfg.CoreURL, e.s.cfg.CoreRegistrationKey = fc.URL, "key"
	e.s.core = core.New(fc.URL, core.Options{Token: "pca_mail", DB: e.s.store.DB})
	return e, fc
}

// folder makes one more folder in an account.
func (e *testEnv) folder(t *testing.T, a store.Account, name, role string) store.Folder {
	t.Helper()
	f := store.Folder{UserID: a.UserID, AccountID: a.ID, Name: name, Display: name, Role: role, Sync: true, Selectable: true}
	if err := e.s.store.UpsertFolder(context.Background(), &f); err != nil {
		t.Fatal(err)
	}
	return f
}

var testUID int64 = 100

// put stores a message in a folder, from Ana unless it says otherwise.
func (e *testEnv) put(t *testing.T, f store.Folder, m store.Message) store.Message {
	t.Helper()
	testUID++
	m.UserID, m.AccountID, m.FolderID, m.UID = f.UserID, f.AccountID, f.ID, testUID
	if m.MessageID == "" {
		m.MessageID = fmt.Sprintf("m%d@test.example", testUID)
	}
	if m.From.Addr == "" {
		m.From = store.Address{Name: "Ana Novak", Addr: "ana@x.org"}
	}
	if err := e.s.store.InsertMessage(context.Background(), &m, nil); err != nil {
		t.Fatal(err)
	}
	return m
}

// get calls a route as a person or a link token and expects 200.
func (e *testEnv) get(t *testing.T, tok, path string) map[string]any {
	t.Helper()
	code, out := e.do(t, tok, "GET", path, nil)
	if code != http.StatusOK {
		t.Fatalf("GET %s → %d %v", path, code, out)
	}
	return out
}

// items are the rows of a capability answer.
func items(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	l, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("no items list in %v", m)
	}
	out := []map[string]any{}
	for _, v := range l {
		out = append(out, v.(map[string]any))
	}
	return out
}

// untouched fails when reading left a mark: a message read, moved or
// starred, a change waiting for the mail server, a draft, or anything sent.
func (e *testEnv) untouched(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	m, err := e.s.store.GetMessage(ctx, e.alice.ID, e.msg.ID)
	if err != nil || m.Seen || m.Flagged || m.FolderID != e.inbox.ID {
		t.Fatalf("Alice's message changed: %v seen=%v starred=%v folder=%d", err, m.Seen, m.Flagged, m.FolderID)
	}
	for _, table := range []string{"pending_ops", "drafts"} {
		var n int
		if err := e.s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s holds %d rows after reading (%v)", table, n, err)
		}
	}
	if n := e.s.saved.Load(); n != 0 {
		t.Fatalf("%d messages were sent", n)
	}
}

func TestPocketSearch(t *testing.T) {
	e := newTestServer(t)
	id := idPath(e.msg.ID)
	got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=invoice"))
	if len(got) != 1 || got[0]["id"] != id || got[0]["title"] != "Your invoice for October" || got[0]["kind"] != "email" ||
		got[0]["date"] != "2026-10-20T09:30:00Z" || got[0]["url"] != "/message/"+id || got[0]["snippet"] != "Ana Novak" {
		t.Fatalf("by subject: %v", got)
	}
	// the text is searched too, and the line says who wrote and how it starts
	e.put(t, e.inbox, store.Message{Subject: "Lunch", Date: "2026-10-21T08:00:00Z", Snippet: "Shall we try the new place on Friday?", BodyText: "Shall we try the new place on Friday?"})
	got = items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=friday"))
	if len(got) != 2 || got[0]["title"] != "Lunch" || got[0]["snippet"] != "Ana Novak · Shall we try the new place on Friday?" || got[1]["id"] != id {
		t.Fatalf("by text, newest first: %v", got)
	}
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=friday&limit=1")); len(got) != 1 || got[0]["title"] != "Lunch" {
		t.Fatalf("limit 1: %v", got)
	}
	// what Alice wrote names who it went to
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=hello")); len(got) != 1 || got[0]["snippet"] != "To Ana N. and 1 other" {
		t.Fatalf("sent mail: %v", got)
	}
	// a second account is searched as well, and a message without a subject has a title
	work := store.Account{UserID: e.alice.ID, Name: "Alice", Email: "alice@work.example", Provider: "demo", SyncEnabled: true}
	if err := e.s.store.CreateAccount(context.Background(), &work); err != nil {
		t.Fatal(err)
	}
	e.put(t, e.folder(t, work, "INBOX", "inbox"), store.Message{From: store.Address{Addr: "boss@work.example"}, Date: "2026-10-22T08:00:00Z", BodyText: "quarterly numbers"})
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=quarterly")); len(got) != 1 || got[0]["title"] != "(no subject)" || got[0]["snippet"] != "boss@work.example" {
		t.Fatalf("second account: %v", got)
	}
	// the search box's operators work; trash and spam stay out unless asked for
	e.put(t, e.folder(t, e.acct, "Trash", "trash"), store.Message{Subject: "Old invoice", Date: "2026-10-01T08:00:00Z"})
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=invoice")); len(got) != 1 {
		t.Fatalf("trash is searched: %v", got)
	}
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=in:trash+invoice")); len(got) != 1 || got[0]["title"] != "Old invoice" {
		t.Fatalf("in:trash: %v", got)
	}
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q=from:boss")); len(got) != 1 || got[0]["title"] != "(no subject)" {
		t.Fatalf("from: %v", got)
	}
	// the same messages the app's own search finds
	_, own := e.do(t, e.aliceTok, "GET", "/api/v1/messages?q=friday", nil)
	if list, _ := own["messages"].([]any); len(list) != 2 || idOf(list[0].(map[string]any)["id"]) == e.msg.ID {
		t.Fatalf("GET /messages?q= differs: %v", own)
	}
	// no words: nothing, not the newest mail
	for _, q := range []string{"", "%21%21%21", "+"} {
		if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/search?q="+q)); len(got) != 0 {
			t.Fatalf("q=%q: %v", q, got)
		}
	}
	// another person's mail never appears
	for _, q := range []string{"invoice", "friday", "hello", "ana", "is:unread"} {
		if got := items(t, e.get(t, e.bobTok, "/api/v1/pocket/search?q="+q)); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	if code, _ := e.do(t, "", "GET", "/api/v1/pocket/search?q=invoice", nil); code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", code)
	}
	e.untouched(t)
}

func TestPocketDay(t *testing.T) {
	e := newTestServer(t)
	id := idPath(e.msg.ID)
	day := e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-20?tz=UTC")
	got := items(t, day)
	if day["date"] != "2026-10-20" || day["summary"] != "1 received" || len(got) != 1 {
		t.Fatalf("one email: %v", day)
	}
	if it := got[0]; it["id"] != id || it["title"] != "Your invoice for October" || it["sub"] != "Ana Novak" || it["kind"] != "received" || it["time"] != "09:30" || it["url"] != "/message/"+id {
		t.Fatalf("received: %v", it)
	}
	if day := e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-19?tz=UTC"); day["summary"] != "1 sent" || items(t, day)[0]["kind"] != "sent" || items(t, day)[0]["sub"] != "To Ana N. and 1 other" {
		t.Fatalf("sent: %v", day)
	}
	// more of the 20th: an answer, a late one, the same message under a label, and what does not count
	e.put(t, e.sent, store.Message{Subject: "Re: Your invoice for October", From: store.Address{Addr: "alice@example.com"}, To: []store.Address{{Name: "Ana Novak", Addr: "ana@x.org"}}, Date: "2026-10-20T10:15:00Z", Seen: true})
	e.put(t, e.inbox, store.Message{Date: "2026-10-20T22:30:00Z", From: store.Address{Addr: "news@shop.example"}})
	e.put(t, e.folder(t, e.acct, "Bills", ""), store.Message{MessageID: e.msg.MessageID, Subject: e.msg.Subject, Date: e.msg.Date})
	for _, role := range []string{"drafts", "trash", "junk"} {
		e.put(t, e.folder(t, e.acct, role, role), store.Message{Subject: "not the day's mail", Date: "2026-10-20T12:00:00Z"})
	}
	day = e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-20?tz=UTC")
	got = items(t, day)
	if day["summary"] != "2 received, 1 sent" || len(got) != 3 {
		t.Fatalf("the 20th: %v", day)
	}
	if got[0]["title"] != "(no subject)" || got[0]["sub"] != "news@shop.example" || got[0]["time"] != "22:30" ||
		got[1]["kind"] != "sent" || got[1]["sub"] != "To Ana Novak" || got[1]["time"] != "10:15" || got[2]["id"] != id {
		t.Fatalf("newest first, the inbox copy of a message filed twice: %v", got)
	}
	// the day is the person's: half past midnight in Sarajevo belongs to the 21st
	day = e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-20?tz=Europe/Sarajevo")
	if got := items(t, day); day["summary"] != "1 received, 1 sent" || len(got) != 2 || got[0]["time"] != "12:15" || got[1]["time"] != "11:30" {
		t.Fatalf("the 20th in Sarajevo: %v", day)
	}
	if day := e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-21?tz=Europe/Sarajevo"); day["summary"] != "1 received" || items(t, day)[0]["time"] != "00:30" {
		t.Fatalf("the 21st in Sarajevo: %v", day)
	}
	// without a time zone: this server's day, as GET /summary/{date}
	local := appMsgDay(e.msg)
	_, sum := e.do(t, e.aliceTok, "GET", "/api/v1/summary/"+local, nil)
	if day := e.get(t, e.aliceTok, "/api/v1/pocket/day/"+local); len(items(t, day)) == 0 || sum["received"] == float64(0) {
		t.Fatalf("the server's own day: %v %v", day, sum)
	}
	// a busy day lists the newest 50 and counts them all
	for i := range 60 {
		e.put(t, e.inbox, store.Message{Subject: fmt.Sprintf("Note %02d", i), Date: fmt.Sprintf("2026-10-25T10:%02d:00Z", i)})
	}
	day = e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-25?tz=UTC")
	if got := items(t, day); day["summary"] != "60 received" || len(got) != 50 || got[0]["title"] != "Note 59" || got[49]["title"] != "Note 10" {
		t.Fatalf("a busy day: %v %d", day["summary"], len(items(t, day)))
	}
	// a day without mail says nothing; another person's mail never appears
	if day := e.get(t, e.aliceTok, "/api/v1/pocket/day/2026-10-01?tz=UTC"); day["summary"] != nil || len(items(t, day)) != 0 {
		t.Fatalf("an empty day: %v", day)
	}
	for _, d := range []string{"2026-10-19", "2026-10-20", "2026-10-25"} {
		if day := e.get(t, e.bobTok, "/api/v1/pocket/day/"+d+"?tz=UTC"); day["summary"] != nil || len(items(t, day)) != 0 {
			t.Fatalf("bob's %s holds alice's mail: %v", d, day)
		}
	}
	if code, _ := e.do(t, e.aliceTok, "GET", "/api/v1/pocket/day/today", nil); code != http.StatusBadRequest {
		t.Fatalf("a day that is no date: %d", code)
	}
	e.untouched(t)
}

func TestPocketWidget(t *testing.T) {
	e := newTestServer(t)
	ctx := context.Background()
	id := idPath(e.msg.ID)
	w := e.get(t, e.aliceTok, "/api/v1/pocket/widget")
	got := items(t, w)
	if w["line"] != "1 unread" || w["url"] != "/m/inbox" || len(got) != 1 {
		t.Fatalf("widget: %v", w)
	}
	if it := got[0]; it["id"] != id || it["title"] != "Your invoice for October" || it["detail"] != "Ana Novak" || it["url"] != "/message/"+id {
		t.Fatalf("row: %v", it)
	}
	if m := w["metrics"].([]any); len(m) != 4 || m[0].(map[string]any)["label"] != "People" || m[0].(map[string]any)["value"] != "1" || m[3].(map[string]any)["value"] != "0" {
		t.Fatalf("the inbox tabs: %v", m)
	}
	// the numbers and rows of the inbox: read, snoozed, sent and filed mail are not in them
	e.put(t, e.inbox, store.Message{Subject: "Read already", Date: "2026-10-22T08:00:00Z", Seen: true})
	e.put(t, e.inbox, store.Message{Subject: "For later", Date: "2026-10-22T09:00:00Z", SnoozeUntil: "2999-01-01T00:00:00Z"})
	e.put(t, e.folder(t, e.acct, "Bills", ""), store.Message{Subject: "Filed", Date: "2026-10-22T10:00:00Z"})
	e.put(t, e.folder(t, e.acct, "Junk", "junk"), store.Message{Subject: "Win a prize", Date: "2026-10-22T11:00:00Z"})
	// two unread answers in one conversation: one row, as the inbox shows it
	lunch := e.put(t, e.inbox, store.Message{Subject: "Lunch", Date: "2026-10-23T08:00:00Z", Category: "updates"})
	e.put(t, e.inbox, store.Message{Subject: "Re: Lunch", Date: "2026-10-23T09:00:00Z", From: store.Address{Name: "Zoran", Addr: "zoran@y.org"}, InReplyTo: lunch.MessageID, Category: "updates"})
	w = e.get(t, e.aliceTok, "/api/v1/pocket/widget")
	got = items(t, w)
	if w["line"] != "3 unread" || len(got) != 2 || got[0]["title"] != "Re: Lunch" || got[0]["detail"] != "Zoran · 2 unread" || got[1]["id"] != id {
		t.Fatalf("the inbox: %v", w)
	}
	if m := w["metrics"].([]any); m[0].(map[string]any)["value"] != "1" || m[1].(map[string]any)["label"] != "Updates" || m[1].(map[string]any)["value"] != "2" {
		t.Fatalf("the inbox tabs: %v", m)
	}
	// someone who reads message by message gets a row each
	st, _ := e.s.store.GetSettings(ctx, e.alice.ID)
	st.Conversations = false
	if err := e.s.store.SaveUserSettings(ctx, e.alice.ID, st); err != nil {
		t.Fatal(err)
	}
	if got := items(t, e.get(t, e.aliceTok, "/api/v1/pocket/widget")); len(got) != 3 || got[0]["detail"] != "Zoran" || got[1]["title"] != "Lunch" || got[1]["detail"] != "Ana Novak" {
		t.Fatalf("message by message: %v", got)
	}
	e.untouched(t)
	// at most eight rows; the line still counts them all
	for i := range 9 {
		e.put(t, e.inbox, store.Message{Subject: fmt.Sprintf("Note %d", i), Date: fmt.Sprintf("2026-10-24T10:%02d:00Z", i)})
	}
	if w := e.get(t, e.aliceTok, "/api/v1/pocket/widget"); w["line"] != "12 unread" || len(items(t, w)) != 8 || items(t, w)[0]["title"] != "Note 8" {
		t.Fatalf("a full inbox: %v", w)
	}
	// an account with nothing unread says so; no account, nothing to show
	if w := e.get(t, e.bobTok, "/api/v1/pocket/widget"); w["line"] != "No unread mail" || len(items(t, w)) != 0 {
		t.Fatalf("bob's widget: %v", w)
	}
	if err := e.s.store.DeleteAccount(ctx, e.bob.ID, e.bobAcct.ID); err != nil {
		t.Fatal(err)
	}
	if w := e.get(t, e.bobTok, "/api/v1/pocket/widget"); w["line"] != nil || w["metrics"] != nil || len(items(t, w)) != 0 {
		t.Fatalf("no account: %v", w)
	}
	if code, _ := e.do(t, "", "GET", "/api/v1/pocket/widget", nil); code != http.StatusUnauthorized {
		t.Fatalf("signed out: %d", code)
	}
}

// A link token with one pocket:* scope reaches its one route and nothing
// else, and nothing it reads is marked as read or changed.
func TestPocketLinkTokens(t *testing.T) {
	e, _ := capEnv(t)
	id := idPath(e.msg.ID)
	line := map[string]any{"request_id": "req-0000000000000001", "text": "Hello"}
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=invoice", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/2026-10-20", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/2026-10-20", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=invoice", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=invoice", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/2026-10-20", nil, 403},
		// Mail keeps no quick line: there is no such route, for a link or for a person
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", line, 404},
		{"pcx_alice_send", "POST", "/api/v1/pocket/capture", line, 404},
		{e.aliceTok, "POST", "/api/v1/pocket/capture", line, 404},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		// the rest of the app: no reading of messages, no change, no sending
		{"pcx_alice_search", "GET", "/api/v1/messages?q=invoice", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/messages/" + id, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/threads/" + id, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/messages/" + id + "/raw", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/attachments/" + idPath(e.pdf.ID), nil, 403},
		{"pcx_alice_search", "POST", "/api/v1/pocket/search?q=invoice", nil, 405},
		{"pcx_alice_day", "GET", "/api/v1/summary/2026-10-20", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/messages", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/counts", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/sidebar", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/accounts", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/settings", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/accounts/" + idPath(e.acct.ID) + "/sync", nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "read"}, 403},
		{"pcx_alice_search", "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "trash"}, 403},
		{"pcx_alice_day", "POST", "/api/v1/actions", map[string]any{"ids": []int64{e.msg.ID}, "action": "star"}, 403},
		{"pcx_alice_search", "POST", "/api/v1/send", map[string]any{"to": "ana@x.org", "subject": "x"}, 403},
		{"pcx_alice_widget", "POST", "/api/v1/compose", map[string]any{"kind": "new"}, 403},
		{"pcx_alice_day", "GET", "/api/v1/passes", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/export.json", nil, 403},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=invoice", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/2026-10-20", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_passes", "GET", "/api/v1/pocket/search?q=invoice", nil, 403},
		{"pcx_alice_send", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/messages?q=invoice", nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m := e.do(t, c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only
	if got := items(t, e.get(t, "pcx_alice_search", "/api/v1/pocket/search?q=invoice")); len(got) != 1 || got[0]["id"] != id {
		t.Fatalf("alice through the link: %v", got)
	}
	if got := items(t, e.get(t, "pcx_bob_search", "/api/v1/pocket/search?q=invoice")); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's mail: %v", got)
	}
	if d := e.get(t, "pcx_alice_day", "/api/v1/pocket/day/2026-10-20?tz=UTC"); d["summary"] != "1 received" {
		t.Fatalf("alice's day through the link: %v", d)
	}
	if d := e.get(t, "pcx_bob_day", "/api/v1/pocket/day/2026-10-20?tz=UTC"); len(items(t, d)) != 0 {
		t.Fatalf("bob's day through the link: %v", d)
	}
	if w := e.get(t, "pcx_alice_widget", "/api/v1/pocket/widget"); w["line"] != "1 unread" {
		t.Fatalf("alice's widget through the link: %v", w)
	}
	if w := e.get(t, "pcx_bob_widget", "/api/v1/pocket/widget"); len(items(t, w)) != 0 || w["line"] != "No unread mail" {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	e.untouched(t)
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeSearch, req("GET", "/api/v1/messages"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/summary/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/counts"), false},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), false},
		{core.ScopeCapture, req("POST", "/api/v1/send"), false},
		{"mail:read", req("GET", "/api/v1/pocket/search?q=x"), false},
		{"mail:read", req("GET", "/api/v1/pocket/day/2026-01-02"), false},
		{"mail:read", req("GET", "/api/v1/pocket/widget"), false},
		{"mail:send", req("POST", "/api/v1/pocket/capture"), false},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Mail tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	e, fc := capEnv(t)
	if err := e.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "mail" || m["category"] != "Everyday" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	if search == nil || fmt.Sprint(search["bangs"]) != "[mail email]" || fmt.Sprint(search["kinds"]) != "[email]" || day == nil || day["label"] != "Mail" ||
		day["one"] != "email" || widget == nil || widget["title"] != "Inbox" {
		t.Fatalf("capabilities: %v %v %v", search, day, widget)
	}
	if m["capture"] != nil {
		t.Fatalf("Mail keeps no quick line: %v", m["capture"])
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"mail:read", "mail:passes", "mail:send", core.ScopeSearch, core.ScopeDay, core.ScopeWidget} {
		if !offered[sc] {
			t.Errorf("%s is not offered", sc)
		}
	}
	if offered[core.ScopeCapture] {
		t.Error("pocket:capture is offered")
	}
}
