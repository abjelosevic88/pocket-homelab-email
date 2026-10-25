package mail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-message/mail"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func TestRunLoopPicksUpNewMailWithIdle(t *testing.T) {
	h := newHarness(t)
	h.ts.appendMsg(t, "INBOX", plainMsg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.e.cfg.SyncInterval = time.Hour
	go h.e.Run(ctx)
	waitFor(t, func() bool {
		a, _ := h.st.GetAccount(ctx, 0, h.a.ID)
		return a.SyncState == "ok" && h.e.Status(h.a.ID).Idle
	})
	h.ts.appendMsg(t, "INBOX", strings.Replace(strings.Replace(plainMsg, "lunch-1", "idle-1", 1), "Lunch on Friday?", "Pushed by IDLE", 1))
	waitFor(t, func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.newMsgs) == 1 && h.newMsgs[0].Subject == "Pushed by IDLE"
	})
	m := h.newMsgs[0]
	if _, err := h.e.Apply(ctx, h.uid, []int64{m.ID}, Action{Kind: "read"}); err != nil {
		t.Fatal(err)
	}
	c := h.ts.client(t)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		buf, err := c.Fetch(uidSet(m.UID), fetchFlags()).Collect()
		return err == nil && len(buf) == 1 && flagsOf(buf[0].Flags).seen
	})
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestBuildMessageRoundTrip(t *testing.T) {
	a := store.Account{Name: "Alice Ž", Email: "alice@example.com"}
	orig := &store.Message{MessageID: "root@x", Refs: "a@x b@x"}
	d := store.Draft{To: []store.Address{{Name: "Ana", Addr: "ana@example.org"}}, Cc: []store.Address{{Addr: "cc@example.org"}}, Bcc: []store.Address{{Addr: "hidden@example.org"}},
		Subject: "Re: Plans — čšž", BodyHTML: "<p>Hello <b>there</b></p>", MessageID: "fixed@example.com"}
	raw, err := BuildMessage(a, d, []store.DraftFile{{Filename: "notes ä.txt", MIME: "text/plain", Data: []byte("file body")}}, orig)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "hidden@example.org") {
		t.Fatal("Bcc leaked into the headers")
	}
	r, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	subj, _ := r.Header.Subject()
	id, _ := r.Header.MessageID()
	irt, _ := r.Header.MsgIDList("In-Reply-To")
	refs, _ := r.Header.MsgIDList("References")
	from, _ := r.Header.AddressList("From")
	if subj != d.Subject || id != "fixed@example.com" || len(irt) != 1 || irt[0] != "root@x" || len(refs) != 3 || from[0].Name != "Alice Ž" {
		t.Fatalf("headers: %q %q %v %v %v", subj, id, irt, refs, from)
	}
	var text, html, file string
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(p.Body)
		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := h.ContentType()
			if ct == "text/plain" {
				text = string(b)
			} else {
				html = string(b)
			}
		case *mail.AttachmentHeader:
			name, _ := h.Filename()
			if name == "notes ä.txt" {
				file = string(b)
			}
		}
	}
	if !strings.Contains(text, "Hello there") || !strings.Contains(html, "<b>there</b>") || file != "file body" {
		t.Fatalf("parts: %q %q %q", text, html, file)
	}
}

func TestSanitize(t *testing.T) {
	in := `<html><head><style>.x{color:red;background:url(https://t.example/a.png)} @import url(https://evil.example/x.css);</style></head>
<body onload="steal()"><p class="x" onclick="x()">Hi</p><script>alert(1)</script><form><input name=p></form>
<img src="https://t.example/pixel.gif" width="1"><img src="cid:logo@x"><a href="https://example.org">site</a>
<iframe src="https://evil.example"></iframe><div style="color:blue;position:fixed;background-image:url('https://t.example/b.png')">x</div></body></html>`
	out, blocked := Sanitize(in, SanitizeOptions{Inline: map[string]string{"logo@x": "data:image/png;base64,AAAA"}})
	for _, bad := range []string{"<script", "onload", "onclick", "<form", "<iframe", "@import", "pixel.gif", "position:fixed", "evil.example"} {
		if strings.Contains(out, bad) {
			t.Fatalf("%q survived:\n%s", bad, out)
		}
	}
	if blocked != 2 {
		t.Fatalf("blocked %d, want 2 (pixel, stylesheet url)\n%s", blocked, out)
	}
	if !strings.Contains(out, `src="data:image/png;base64,AAAA"`) || !strings.Contains(out, `target="_blank"`) || !strings.Contains(out, "noopener") || !strings.Contains(out, "color:red") {
		t.Fatalf("output:\n%s", out)
	}
	allowed, n := Sanitize(`<img src="https://t.example/pixel.gif">`, SanitizeOptions{AllowRemote: true})
	if n != 0 || !strings.Contains(allowed, "pixel.gif") {
		t.Fatalf("allowed: %s %d", allowed, n)
	}
}

func TestHTMLToTextAndSnippet(t *testing.T) {
	txt := HTMLToText(`<html><head><title>T</title><style>p{}</style></head><body><h1>Hello</h1><p>See <a href="https://x.example/a">the plan</a>.</p><ul><li>one</li><li>two</li></ul>line<br>break</body></html>`)
	for _, want := range []string{"Hello", "the plan (https://x.example/a)", "- one", "- two", "line\nbreak"} {
		if !strings.Contains(txt, want) {
			t.Fatalf("%q missing in %q", want, txt)
		}
	}
	if strings.Contains(txt, "p{}") {
		t.Fatal("css in text")
	}
	s := Snippet("Thanks!\n\nSee you soon.\n\nOn Mon, 19 Oct 2026 Ana wrote:\n> old text")
	if s != "Thanks! See you soon." {
		t.Fatalf("snippet %q", s)
	}
	if long := Snippet(strings.Repeat("word ", 100)); len([]rune(long)) > 201 || !strings.HasSuffix(long, "…") {
		t.Fatalf("long snippet %q", long)
	}
}

func TestDiscover(t *testing.T) {
	oldMX, oldISP := lookupMX, ispdbURL
	defer func() { lookupMX, ispdbURL = oldMX, oldISP }()
	ispdbURL = "http://127.0.0.1:1/"
	lookupMX = func(ctx context.Context, d string) ([]*net.MX, error) {
		switch d {
		case "firma.rs":
			return []*net.MX{{Host: "aspmx.l.google.com."}}, nil
		case "work.example":
			return []*net.MX{{Host: "work-example.mail.protection.outlook.com."}}, nil
		}
		return nil, errors.New("no mx")
	}
	ctx := context.Background()
	cases := map[string]string{"me@gmail.com": "gmail", "me@icloud.com": "icloud", "me@hotmail.com": "outlook", "a@firma.rs": "gmail", "b@work.example": "outlook", "c@nowhere.example": "custom"}
	for email, want := range cases {
		p := Discover(ctx, email)
		if p.Provider != want {
			t.Fatalf("%s: got %s (%s)", email, p.Provider, p.Found)
		}
		if p.Username != email {
			t.Fatalf("%s: username %q", email, p.Username)
		}
	}
	if p := Discover(ctx, "me@gmail.com"); p.OAuth != "google" || !p.SavesSent || p.IMAPHost != "imap.gmail.com" || p.Auth[0] != "oauth" {
		t.Fatalf("gmail preset: %+v", p)
	}
	if p := Discover(ctx, "a@firma.rs"); p.Name != "Google Workspace" || p.Found != "mx" {
		t.Fatalf("workspace: %+v", p)
	}
	if p := Discover(ctx, "c@nowhere.example"); p.IMAPHost != "imap.nowhere.example" || p.Found != "guess" {
		t.Fatalf("guess: %+v", p)
	}
	if len(Providers()) < 10 {
		t.Fatal("providers list")
	}
}

func TestDiscoverISPDB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/example.net") {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `<clientConfig version="1.1"><emailProvider id="example.net"><displayName>Example Mail</displayName>
<incomingServer type="imap"><hostname>mail.example.net</hostname><port>143</port><socketType>STARTTLS</socketType><username>%EMAILLOCALPART%</username></incomingServer>
<outgoingServer type="smtp"><hostname>smtp.example.net</hostname><port>587</port><socketType>STARTTLS</socketType></outgoingServer></emailProvider></clientConfig>`)
	}))
	defer srv.Close()
	old := ispdbURL
	defer func() { ispdbURL = old }()
	ispdbURL = srv.URL + "/v1.1/"
	p := Discover(context.Background(), "joe@example.net")
	if p.Found != "ispdb" || p.IMAPHost != "mail.example.net" || p.IMAPPort != 143 || p.IMAPSecurity != "starttls" || p.Username != "joe" || p.SMTPPort != 587 {
		t.Fatalf("ispdb: %+v", p)
	}
}

func TestCategorize(t *testing.T) {
	cases := []struct {
		from, subject string
		hi            headerInfo
		want          string
	}{
		{"ana@example.org", "Lunch?", headerInfo{}, "people"},
		{"news@shop.example", "Autumn sale", headerInfo{listUnsub: "<https://x>"}, "newsletters"},
		{"noreply@shop.example", "Your order has shipped", headerInfo{}, "receipts"},
		{"billing@telekom.rs", "Vaš račun za oktobar", headerInfo{}, "receipts"},
		{"notifications@github.com", "[repo] New issue", headerInfo{}, "updates"},
		{"alerts@bank.example", "Login from new device", headerInfo{autoSubmitted: "auto-generated"}, "updates"},
		{"bob@lists.example", "Re: meeting notes", headerInfo{listID: "team.lists.example", precedence: "list"}, "people"},
	}
	for _, c := range cases {
		if got := categorize(c.from, c.subject, c.hi); got != c.want {
			t.Fatalf("%s / %s: %s, want %s", c.from, c.subject, got, c.want)
		}
	}
}

func TestParseInvite(t *testing.T) {
	ics := "BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nBEGIN:VTIMEZONE\r\nTZID:Europe/Belgrade\r\nBEGIN:STANDARD\r\nDTSTART:19701025T030000\r\nEND:STANDARD\r\nEND:VTIMEZONE\r\n" +
		"BEGIN:VEVENT\r\nUID:abc-123\r\nSUMMARY:Project review with a very long title that the cale\r\n ndar folded\r\nLOCATION:Room 4\\, second floor\r\n" +
		"DTSTART;TZID=Europe/Belgrade:20261030T100000\r\nDTEND;TZID=Europe/Belgrade:20261030T113000\r\n" +
		"ORGANIZER;CN=\"Ana K\":mailto:ana@example.org\r\nATTENDEE;CN=Alice:mailto:alice@example.com\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	inv, err := ParseInvite([]byte(ics))
	if err != nil {
		t.Fatal(err)
	}
	if inv.Summary != "Project review with a very long title that the calendar folded" || inv.Location != "Room 4, second floor" || inv.Method != "REQUEST" || inv.UID != "abc-123" {
		t.Fatalf("invite: %+v", inv)
	}
	if inv.Start != "2026-10-30T10:00:00+01:00" || inv.End != "2026-10-30T11:30:00+01:00" || inv.AllDay {
		t.Fatalf("times: %s %s", inv.Start, inv.End)
	}
	if inv.Organizer.Name != "Ana K" || inv.Organizer.Addr != "ana@example.org" || len(inv.Attendees) != 1 {
		t.Fatalf("people: %+v", inv)
	}
	day, err := ParseInvite([]byte("BEGIN:VCALENDAR\nBEGIN:VEVENT\nSUMMARY:Holiday\nDTSTART;VALUE=DATE:20261102\nEND:VEVENT\nEND:VCALENDAR\n"))
	if err != nil || !day.AllDay || day.Start != "2026-11-02" || day.End != "2026-11-03" {
		t.Fatalf("all day: %+v %v", day, err)
	}
	utc, _ := ParseInvite([]byte("BEGIN:VEVENT\nDTSTART:20261030T080000Z\nDURATION:PT45M\nEND:VEVENT\n"))
	if utc == nil || utc.End != "2026-10-30T08:45:00Z" {
		t.Fatalf("utc+duration: %+v", utc)
	}
	if _, err := ParseInvite([]byte("BEGIN:VCALENDAR\nEND:VCALENDAR")); err == nil {
		t.Fatal("empty calendar parsed")
	}
}

func TestParseUnsubscribe(t *testing.T) {
	u := ParseUnsubscribe("<mailto:unsub@x.example?subject=stop>, <https://x.example/u/1>", true)
	if u.Method != "one_click" || u.URL != "https://x.example/u/1" || u.Mailto != "unsub@x.example?subject=stop" {
		t.Fatalf("%+v", u)
	}
	if u := ParseUnsubscribe("<mailto:unsub@x.example>", false); u.Method != "mailto" {
		t.Fatalf("%+v", u)
	}
	if u := ParseUnsubscribe("<https://x.example/u>", false); u.Method != "url" {
		t.Fatalf("%+v", u)
	}
	if u := ParseUnsubscribe("", false); u.Method != "none" {
		t.Fatalf("%+v", u)
	}
}

func TestMatchRule(t *testing.T) {
	m := store.Message{AccountID: 1, From: store.Address{Name: "Shop", Addr: "billing@shop.example"}, Subject: "Your Invoice", HasAttachments: true,
		To: []store.Address{{Addr: "me@example.com"}}, Category: "receipts"}
	acct2 := int64(2)
	cases := []struct {
		r    store.Rule
		want bool
	}{
		{store.Rule{Enabled: true, Match: store.RuleMatch{From: "SHOP.example", HasAttachment: true}}, true},
		{store.Rule{Enabled: true, Match: store.RuleMatch{Subject: "invoice", Category: "receipts", To: "me@"}}, true},
		{store.Rule{Enabled: false, Match: store.RuleMatch{From: "shop"}}, false},
		{store.Rule{Enabled: true, Match: store.RuleMatch{From: "shop"}, AccountID: &acct2}, false},
		{store.Rule{Enabled: true, Match: store.RuleMatch{Subject: "receipt"}}, false},
		{store.Rule{Enabled: true}, false},
	}
	for i, c := range cases {
		if MatchRule(c.r, m) != c.want {
			t.Fatalf("case %d: want %v", i, c.want)
		}
	}
}

func TestOAuth(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		claims, _ := json.Marshal(map[string]any{"email": "alice@gmail.com", "name": "Alice"})
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt", "expires_in": 3600,
			"id_token": "x." + base64.RawURLEncoding.EncodeToString(claims) + ".y"})
	}))
	defer srv.Close()
	old := googleTokenURL
	defer func() { googleTokenURL = old }()
	googleTokenURL = srv.URL
	c := OAuthClients{GoogleID: "gid", GoogleSecret: "gs"}
	u, err := OAuthAuthURL(c, "google", "https://mail.example/cb", "st", "verifier", "alice@gmail.com")
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(u)
	q := pu.Query()
	if q.Get("code_challenge") != pkce("verifier") || q.Get("access_type") != "offline" || !strings.Contains(q.Get("scope"), "https://mail.google.com/") || q.Get("login_hint") != "alice@gmail.com" {
		t.Fatalf("auth url %s", u)
	}
	tok, err := OAuthExchange(context.Background(), c, "google", "https://mail.example/cb", "code", "verifier")
	if err != nil || tok.Email != "alice@gmail.com" || tok.RefreshToken != "rt" || tok.AccessToken != "at" {
		t.Fatalf("exchange %+v %v", tok, err)
	}
	if gotForm.Get("code_verifier") != "verifier" || gotForm.Get("client_secret") != "gs" {
		t.Fatalf("form %v", gotForm)
	}
	if _, err := OAuthAuthURL(OAuthClients{}, "microsoft", "x", "s", "v", ""); err == nil || !strings.Contains(err.Error(), "not set up") {
		t.Fatalf("missing client: %v", err)
	}
	mu, err := OAuthAuthURL(OAuthClients{MicrosoftID: "mid"}, "microsoft", "https://mail.example/cb", "s", "v", "")
	if err != nil || !strings.Contains(mu, "login.microsoftonline.com/common/") || !strings.Contains(mu, "IMAP.AccessAsUser.All") {
		t.Fatalf("ms url %s %v", mu, err)
	}
}

func TestOneClickUnsubscribe(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = r.Method + " " + string(b)
	}))
	defer srv.Close()
	e := New(nil, nil, Config{UserAgent: "test"}, nil)
	// the test server is http; one-click is only offered for https, so call it directly
	if err := e.oneClick(context.Background(), srv.URL); err != nil || body != "POST List-Unsubscribe=One-Click" {
		t.Fatalf("one click: %q %v", body, err)
	}
}
