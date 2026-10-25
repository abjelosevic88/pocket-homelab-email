package mail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/abjelosevic88/pocket-homelab-email/internal/secret"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- a mail server for the tests ----

type testServer struct {
	imapAddr, smtpAddr string
	user               *imapmemserver.User
	mu                 sync.Mutex
	sent               []sentMail
}

type sentMail struct {
	from string
	to   []string
	data []byte
}

type smtpBackend struct{ ts *testServer }

func (b smtpBackend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &smtpSession{ts: b.ts}, nil
}

type smtpSession struct {
	ts     *testServer
	authed bool
	from   string
	to     []string
}

func (s *smtpSession) AuthMechanisms() []string { return []string{sasl.Plain} }
func (s *smtpSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(identity, user, pass string) error {
		if user != "alice@example.com" || pass != "secret-pw" {
			return errors.New("invalid credentials")
		}
		s.authed = true
		return nil
	}), nil
}
func (s *smtpSession) Mail(from string, opts *smtp.MailOptions) error {
	if !s.authed {
		return smtp.ErrAuthRequired
	}
	s.from = from
	return nil
}
func (s *smtpSession) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}
func (s *smtpSession) Data(r io.Reader) error {
	b, _ := io.ReadAll(r)
	s.ts.mu.Lock()
	s.ts.sent = append(s.ts.sent, sentMail{from: s.from, to: s.to, data: b})
	s.ts.mu.Unlock()
	return nil
}
func (s *smtpSession) Reset()        {}
func (s *smtpSession) Logout() error { return nil }

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	ts := &testServer{}
	mem := imapmemserver.New()
	u := imapmemserver.NewUser("alice@example.com", "secret-pw")
	for _, name := range []string{"INBOX", "Sent", "Trash", "Archive", "Projects"} {
		if err := u.Create(name, nil); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(u)
	ts.user = u
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		Logger:       nopLogger{},
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close() })
	ts.imapAddr = ln.Addr().String()

	ss := smtp.NewServer(smtpBackend{ts})
	ss.Domain = "localhost"
	ss.AllowInsecureAuth = true
	sl, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ss.Serve(sl) }()
	t.Cleanup(func() { ss.Close() })
	ts.smtpAddr = sl.Addr().String()
	return ts
}

type nopLogger struct{}

func (nopLogger) Printf(string, ...interface{}) {}

func (ts *testServer) appendMsg(t *testing.T, mailbox, raw string, flags ...imap.Flag) {
	t.Helper()
	raw = strings.ReplaceAll(strings.ReplaceAll(raw, "\r\n", "\n"), "\n", "\r\n")
	if _, err := ts.user.Append(mailbox, bytes.NewReader([]byte(raw)), &imap.AppendOptions{Flags: flags, Time: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

// client is a second connection, playing another mail app changing the server.
func (ts *testServer) client(t *testing.T) *imapclient.Client {
	t.Helper()
	c, err := imapclient.DialInsecure(ts.imapAddr, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("alice@example.com", "secret-pw").Wait(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func hostPort(t *testing.T, addr string) (string, int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return h, n
}

const plainMsg = `From: Ana Kovač <ana@example.org>
To: Alice <alice@example.com>
Subject: Lunch on Friday?
Message-ID: <lunch-1@example.org>
Date: Mon, 19 Oct 2026 10:00:00 +0200
Content-Type: text/plain; charset=utf-8

Shall we meet at the café at noon?
`

const replyMsg = `From: Ana Kovač <ana@example.org>
To: Alice <alice@example.com>
Subject: Re: Lunch on Friday?
Message-ID: <lunch-2@example.org>
In-Reply-To: <lunch-1@example.org>
References: <lunch-1@example.org>
Date: Mon, 19 Oct 2026 12:00:00 +0200
Content-Type: text/plain; charset=iso-8859-1
Content-Transfer-Encoding: quoted-printable

Or at one? Gr=FC=DFe
`

const htmlMsg = `From: Shop <billing@shop.example>
To: alice@example.com
Subject: Your invoice 1234
Message-ID: <inv-1234@shop.example>
Date: Tue, 20 Oct 2026 09:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="mix"

--mix
Content-Type: multipart/related; boundary="rel"

--rel
Content-Type: text/html; charset=utf-8

<html><body><p>Thanks for your order</p><img src="cid:logo@shop"><img src="https://tracker.example/p.gif"><script>alert(1)</script></body></html>
--rel
Content-Type: image/png
Content-ID: <logo@shop>
Content-Disposition: inline
Content-Transfer-Encoding: base64

iVBORw0KGgo=
--rel--
--mix
Content-Type: application/pdf; name="invoice.pdf"
Content-Disposition: attachment; filename="invoice.pdf"
Content-Transfer-Encoding: base64

JVBERi0xLjQKJcOkw7zDtsOfCg==
--mix--
`

const newsMsg = `From: Weekly <news@letters.example>
To: alice@example.com
Subject: This week in gardening
Message-ID: <news-9@letters.example>
List-Id: Gardening <garden.letters.example>
List-Unsubscribe: <https://letters.example/u/9>, <mailto:unsub@letters.example?subject=stop>
List-Unsubscribe-Post: List-Unsubscribe=One-Click
Date: Wed, 21 Oct 2026 07:00:00 +0000
Content-Type: text/plain; charset=utf-8

Tomatoes, again.
`

type harness struct {
	ts  *testServer
	st  *store.Store
	e   *Engine
	a   store.Account
	uid int64

	mu      sync.Mutex
	newMsgs []store.Message
	changes [][]int64
	sentOK  []store.Draft
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	h := &harness{ts: newTestServer(t)}
	st, err := store.Open(filepath.Join(t.TempDir(), "m.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h.st = st
	u := &store.User{Email: "alice@example.com", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	h.uid = u.ID
	box := secret.New(bytes.Repeat([]byte{7}, 32))
	sealed, _ := box.Seal("secret-pw")
	ih, ip := hostPort(t, h.ts.imapAddr)
	sh, sp := hostPort(t, h.ts.smtpAddr)
	a := store.Account{UserID: u.ID, Name: "Alice", Email: "alice@example.com", Provider: "custom", Username: "alice@example.com",
		IMAPHost: ih, IMAPPort: ip, IMAPSecurity: "none", SMTPHost: sh, SMTPPort: sp, SMTPSecurity: "none", Secret: sealed, SyncEnabled: true, SyncDays: 30}
	if err := st.CreateAccount(ctx, &a); err != nil {
		t.Fatal(err)
	}
	h.a = a
	h.e = New(st, box, Config{
		OnNew:    func(m store.Message) { h.mu.Lock(); h.newMsgs = append(h.newMsgs, m); h.mu.Unlock() },
		OnChange: func(uid, acct int64, ids []int64) { h.mu.Lock(); h.changes = append(h.changes, ids); h.mu.Unlock() },
		OnSent: func(d store.Draft, err error) {
			if err == nil {
				h.mu.Lock()
				h.sentOK = append(h.sentOK, d)
				h.mu.Unlock()
			}
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return h
}

// sync runs one full sync of the account like the account loop does.
func (h *harness) sync(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	a, err := h.st.GetAccount(ctx, 0, h.a.ID)
	if err != nil {
		t.Fatal(err)
	}
	c, err := h.e.dialIMAP(ctx, &a, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := h.e.syncAccount(ctx, c, h.e.run(a.ID), &a); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) list(t *testing.T, q store.Query) []store.Message {
	t.Helper()
	ms, _, err := h.st.ListMessages(context.Background(), h.uid, q)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

func (h *harness) bySubject(t *testing.T, subject string) store.Message {
	t.Helper()
	for _, m := range h.list(t, store.Query{Box: "all", Limit: 200}) {
		if m.Subject == subject {
			full, err := h.st.GetMessage(context.Background(), h.uid, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			return full
		}
	}
	t.Fatalf("no message %q", subject)
	return store.Message{}
}

func TestSyncAndChanges(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.ts.appendMsg(t, "INBOX", plainMsg)
	h.ts.appendMsg(t, "INBOX", replyMsg, imap.FlagSeen)
	h.ts.appendMsg(t, "INBOX", htmlMsg)
	h.ts.appendMsg(t, "INBOX", newsMsg)

	h.sync(t)

	folders, _ := h.st.AccountFolders(ctx, h.a.ID)
	roles := map[string]string{}
	for _, f := range folders {
		roles[f.Name] = f.Role
	}
	if roles["INBOX"] != "inbox" || roles["Sent"] != "sent" || roles["Trash"] != "trash" || roles["Archive"] != "archive" || roles["Projects"] != "" {
		t.Fatalf("roles: %v", roles)
	}
	inbox := h.list(t, store.Query{Box: "inbox"})
	if len(inbox) != 4 {
		t.Fatalf("inbox has %d", len(inbox))
	}
	if len(h.newMsgs) != 0 {
		t.Fatal("the first sync announced old mail as new")
	}
	lunch := h.bySubject(t, "Lunch on Friday?")
	reply := h.bySubject(t, "Re: Lunch on Friday?")
	if lunch.ThreadKey != reply.ThreadKey || !reply.Seen || lunch.Seen {
		t.Fatalf("thread/flags: %+v %+v", lunch, reply)
	}
	if !strings.Contains(reply.BodyText, "Grüße") {
		t.Fatalf("charset decoding: %q", reply.BodyText)
	}
	if lunch.From.Name != "Ana Kovač" || !strings.Contains(lunch.Snippet, "café") {
		t.Fatalf("lunch: %+v", lunch)
	}
	inv := h.bySubject(t, "Your invoice 1234")
	if !inv.HasAttachments || inv.Category != "receipts" || !strings.Contains(inv.BodyHTML, "Thanks for your order") {
		t.Fatalf("invoice: att=%v cat=%s html=%q", inv.HasAttachments, inv.Category, inv.BodyHTML)
	}
	atts, _ := h.st.Attachments(ctx, h.uid, inv.ID)
	var pdf, logo store.Attachment
	for _, a := range atts {
		if a.Filename == "invoice.pdf" {
			pdf = a
		}
		if a.Inline && a.ContentID == "logo@shop" {
			logo = a
		}
	}
	if pdf.ID == 0 || logo.ID == 0 {
		t.Fatalf("attachments: %+v", atts)
	}
	data, err := h.e.AttachmentData(ctx, inv, pdf)
	if err != nil || !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("attachment data %q %v", data, err)
	}
	raw, err := h.e.Raw(ctx, inv)
	if err != nil || !bytes.Contains(raw, []byte("Your invoice 1234")) {
		t.Fatalf("raw: %v", err)
	}
	news := h.bySubject(t, "This week in gardening")
	if news.Category != "newsletters" || !news.UnsubscribePost || news.ListUnsubscribe == "" {
		t.Fatalf("newsletter: %+v", news)
	}

	// a new message arrives, another app reads Lunch and deletes the newsletter
	h.ts.appendMsg(t, "INBOX", strings.Replace(strings.Replace(plainMsg, "lunch-1", "dinner-1", 1), "Lunch on Friday?", "Dinner tonight", 1))
	c := h.ts.client(t)
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := c.Store(imap.UIDSetNum(imap.UID(lunch.UID)), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Store(imap.UIDSetNum(imap.UID(news.UID)), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Expunge().Close(); err != nil {
		t.Fatal(err)
	}
	h.sync(t)
	if len(h.newMsgs) != 1 || h.newMsgs[0].Subject != "Dinner tonight" {
		t.Fatalf("new mail: %+v", h.newMsgs)
	}
	if !h.bySubject(t, "Lunch on Friday?").Seen {
		t.Fatal("the flag change on the server was not picked up")
	}
	for _, m := range h.list(t, store.Query{Box: "all"}) {
		if m.Subject == "This week in gardening" {
			t.Fatal("the deletion on the server was not picked up")
		}
	}

	// archive + undo, then archive for real
	ops, err := h.e.Apply(ctx, h.uid, []int64{inv.ID}, Action{Kind: "archive"})
	if err != nil || len(ops) != 1 {
		t.Fatalf("apply: %v %v", ops, err)
	}
	moved, _ := h.st.GetMessage(ctx, h.uid, inv.ID)
	if moved.FolderRole != "archive" || moved.UID >= 0 {
		t.Fatalf("archived here: %+v", moved)
	}
	if err := h.e.Undo(ctx, h.uid, ops); err != nil {
		t.Fatal(err)
	}
	back, _ := h.st.GetMessage(ctx, h.uid, inv.ID)
	if back.FolderRole != "inbox" || back.UID != inv.UID {
		t.Fatalf("undo: %+v", back)
	}
	if _, err := h.e.apply(ctx, h.uid, []int64{inv.ID}, Action{Kind: "archive"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := h.e.apply(ctx, h.uid, []int64{lunch.ID}, Action{Kind: "star"}, 0); err != nil {
		t.Fatal(err)
	}
	h.e.applyOps(ctx, h.a.ID)
	done, _ := h.st.GetMessage(ctx, h.uid, inv.ID)
	if done.FolderRole != "archive" || done.UID <= 0 {
		t.Fatalf("after the server move: %+v", done)
	}
	if _, err := c.Select("Archive", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if sd, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait(); err != nil || len(sd.AllUIDs()) != 1 {
		t.Fatalf("server Archive: %v %v", sd, err)
	}
	if _, err := c.Select("INBOX", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if sd, err := c.UIDSearch(&imap.SearchCriteria{Flag: []imap.Flag{imap.FlagFlagged}}, nil).Wait(); err != nil || len(sd.AllUIDs()) != 1 {
		t.Fatalf("server star: %v %v", sd, err)
	}
	h.sync(t) // the archived message stays where it is, not twice
	n := 0
	for _, m := range h.list(t, store.Query{Box: "archive"}) {
		if m.Subject == "Your invoice 1234" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("archive has the invoice %d times", n)
	}

	// send a reply: SMTP gets it, Sent gets a copy, the original is answered
	orig := h.bySubject(t, "Dinner tonight")
	d := &store.Draft{UserID: h.uid, AccountID: h.a.ID, Kind: "reply", ReplyToID: &orig.ID, To: []store.Address{{Name: "Ana", Addr: "ana@example.org"}},
		Bcc: []store.Address{{Addr: "me@example.net"}}, Subject: "Re: Dinner tonight", BodyHTML: "<p>Yes, <b>gladly</b>.</p>"}
	if err := h.st.CreateDraft(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := h.st.AddDraftFile(ctx, &store.DraftFile{UserID: h.uid, DraftID: d.ID, Filename: "map.txt", MIME: "text/plain", Data: []byte("turn left")}); err != nil {
		t.Fatal(err)
	}
	if err := h.st.QueueDraft(ctx, h.uid, d.ID, store.Now(), NewMessageID("alice@example.com")); err != nil {
		t.Fatal(err)
	}
	h.e.sendDue(ctx)
	if len(h.ts.sent) != 1 || len(h.ts.sent[0].to) != 2 || len(h.sentOK) != 1 {
		t.Fatalf("smtp: %+v sentOK=%d", h.ts.sent, len(h.sentOK))
	}
	body := string(h.ts.sent[0].data)
	if strings.Contains(body, "me@example.net") || !strings.Contains(body, "In-Reply-To: <dinner-1@example.org>") || !strings.Contains(body, "map.txt") {
		t.Fatalf("sent message:\n%s", body)
	}
	if _, err := c.Select("Sent", nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if sd, _ := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait(); len(sd.AllUIDs()) != 1 {
		t.Fatal("no copy in Sent")
	}
	if got, _ := h.st.GetDraft(ctx, h.uid, d.ID); got.Status != "sent" {
		t.Fatalf("draft status %s %s", got.Status, got.Error)
	}
	if o, _ := h.st.GetMessage(ctx, h.uid, orig.ID); !o.Answered {
		t.Fatal("original not answered")
	}

	// permanent delete from Trash + undo
	if _, err := h.e.apply(ctx, h.uid, []int64{orig.ID}, Action{Kind: "trash"}, 0); err != nil {
		t.Fatal(err)
	}
	h.e.applyOps(ctx, h.a.ID)
	tr, _ := h.st.GetMessage(ctx, h.uid, orig.ID)
	if tr.FolderRole != "trash" || tr.UID <= 0 {
		t.Fatalf("trash: %+v", tr)
	}
	ops, err = h.e.Apply(ctx, h.uid, []int64{orig.ID}, Action{Kind: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.st.GetMessage(ctx, h.uid, orig.ID); err != store.ErrNotFound {
		t.Fatal("deleted message still here")
	}
	if err := h.e.Undo(ctx, h.uid, ops); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range h.list(t, store.Query{Box: "trash"}) {
		if m.Subject == "Dinner tonight" {
			found = true
		}
	}
	if !found {
		t.Fatal("undo of a permanent delete did not bring it back")
	}

	if err := h.e.Test(ctx, h.a, ""); err != nil {
		t.Fatalf("test: %v", err)
	}
	bad := h.a
	if err := h.e.Test(ctx, bad, "wrong"); err == nil || !isAuthError(err) {
		t.Fatalf("wrong password: %v", err)
	}
}

func TestEnsureBodyOnDemand(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	for i := 0; i < firstBodies+5; i++ {
		h.ts.appendMsg(t, "INBOX", strings.Replace(plainMsg, "lunch-1", "m"+strconv.Itoa(i), 1))
	}
	a, _ := h.st.GetAccount(ctx, 0, h.a.ID)
	c, err := h.e.dialIMAP(ctx, &a, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := h.e.syncFolders(ctx, c, &a); err != nil {
		t.Fatal(err)
	}
	inbox, _ := h.st.FolderByRole(ctx, a.ID, "inbox")
	if err := h.e.syncFolder(ctx, c, h.e.run(a.ID), &a, inbox, true); err != nil {
		t.Fatal(err)
	}
	need, _ := h.st.MessagesNeedingBody(ctx, a.ID, 100)
	if len(need) != 5 {
		t.Fatalf("%d without text, want 5", len(need))
	}
	m := need[0]
	if err := h.e.EnsureBody(ctx, &m); err != nil || !m.BodyFetched || !strings.Contains(m.BodyText, "café") {
		t.Fatalf("ensure body: %v %+v", err, m)
	}
}

func TestDemoAccountStaysLocal(t *testing.T) {
	ctx := context.Background()
	st, _ := store.Open(filepath.Join(t.TempDir(), "d.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer st.Close()
	u := &store.User{Email: "d@example.com", Role: "admin"}
	_ = st.CreateUser(ctx, u)
	a := store.Account{UserID: u.ID, Email: "d@example.com", Name: "Demo", Provider: "demo", SyncEnabled: true}
	_ = st.CreateAccount(ctx, &a)
	inbox := store.Folder{UserID: u.ID, AccountID: a.ID, Name: "INBOX", Role: "inbox", Sync: true, Selectable: true}
	_ = st.UpsertFolder(ctx, &inbox)
	m := store.Message{UserID: u.ID, AccountID: a.ID, FolderID: inbox.ID, UID: 1, Subject: "Hi", Date: store.Now()}
	_ = st.InsertMessage(ctx, &m, nil)
	e := New(st, secret.New(bytes.Repeat([]byte{1}, 32)), Config{}, nil)
	if _, err := e.apply(ctx, u.ID, []int64{m.ID}, Action{Kind: "archive"}, 0); err != nil {
		t.Fatal(err)
	}
	e.applyOps(ctx, a.ID)
	got, _ := st.GetMessage(ctx, u.ID, m.ID)
	if got.FolderRole != "archive" || got.UID <= 0 {
		t.Fatalf("demo archive: %+v", got)
	}
	d := &store.Draft{UserID: u.ID, AccountID: a.ID, To: []store.Address{{Addr: "x@example.org"}}, Subject: "Out", BodyHTML: "<p>hello</p>"}
	_ = st.CreateDraft(ctx, d)
	_ = st.QueueDraft(ctx, u.ID, d.ID, store.Now(), "")
	e.sendDue(ctx)
	sent, _, _ := st.ListMessages(ctx, u.ID, store.Query{Box: "sent"})
	if len(sent) != 1 || sent[0].Subject != "Out" {
		t.Fatalf("demo sent: %+v", sent)
	}
	if err := e.Test(ctx, a, ""); err != nil {
		t.Fatal(err)
	}
}

func uidSet(u int64) imap.UIDSet     { return imap.UIDSetNum(imap.UID(u)) }
func fetchFlags() *imap.FetchOptions { return &imap.FetchOptions{UID: true, Flags: true} }
