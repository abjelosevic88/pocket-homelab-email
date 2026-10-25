package store

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestMessagesThreadsSearchAndIsolation(t *testing.T) {
	ctx := context.Background()
	st := testStore(t)
	alice := &User{Email: "alice@example.com", Role: "admin"}
	bob := &User{Email: "bob@example.com", Role: "member"}
	if err := st.CreateUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateUser(ctx, bob); err != nil {
		t.Fatal(err)
	}
	acct := &Account{UserID: alice.ID, Email: "alice@example.com", SyncEnabled: true}
	if err := st.CreateAccount(ctx, acct); err != nil {
		t.Fatal(err)
	}
	inbox := &Folder{UserID: alice.ID, AccountID: acct.ID, Name: "INBOX", Display: "Inbox", Role: "inbox", Sync: true, Selectable: true}
	if err := st.UpsertFolder(ctx, inbox); err != nil {
		t.Fatal(err)
	}
	m1 := &Message{UserID: alice.ID, AccountID: acct.ID, FolderID: inbox.ID, UID: 1, MessageID: "a@x", Subject: "Lunch on Friday", From: Address{"Ana", "ana@x.org"}, Date: "2026-10-01T10:00:00Z", BodyText: "shall we meet at the café"}
	m2 := &Message{UserID: alice.ID, AccountID: acct.ID, FolderID: inbox.ID, UID: 2, MessageID: "b@x", InReplyTo: "a@x", Refs: "a@x", Subject: "Re: Lunch on Friday", From: Address{"Ana", "ana@x.org"}, Date: "2026-10-02T10:00:00Z", Seen: true}
	m3 := &Message{UserID: alice.ID, AccountID: acct.ID, FolderID: inbox.ID, UID: 3, MessageID: "c@x", Subject: "Your invoice", From: Address{"Shop", "billing@shop.com"}, Date: "2026-10-03T10:00:00Z", Category: "receipts"}
	for _, m := range []*Message{m1, m2, m3} {
		if err := st.InsertMessage(ctx, m, []Attachment{{Filename: "a.pdf", MIME: "application/pdf", Part: "2"}}); err != nil {
			t.Fatal(err)
		}
	}
	if m2.ThreadKey != m1.ThreadKey {
		t.Fatalf("thread keys differ: %q %q", m1.ThreadKey, m2.ThreadKey)
	}
	rows, _, err := st.ListMessages(ctx, alice.ID, Query{Box: "inbox", Conversations: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].ThreadCount != 2 || rows[1].ThreadUnread != 1 {
		t.Fatalf("conversations: %+v", rows)
	}
	found, _, err := st.ListMessages(ctx, alice.ID, Query{Search: "cafe from:ana"})
	if err != nil || len(found) != 1 || found[0].ID != m1.ID {
		t.Fatalf("search: %v %+v", err, found)
	}
	if got, _, _ := st.ListMessages(ctx, bob.ID, Query{Box: "inbox"}); len(got) != 0 {
		t.Fatal("bob sees alice's mail")
	}
	if _, err := st.GetMessage(ctx, bob.ID, m1.ID); err != ErrNotFound {
		t.Fatal("bob opened alice's message")
	}
	c, err := st.BoxCounts(ctx, alice.ID)
	if err != nil || c["inbox"] != 2 || c["receipts"] != 1 {
		t.Fatalf("counts %v %v", c, err)
	}
	atts, _, err := st.ListAttachments(ctx, alice.ID, "documents", "", 0, 10)
	if err != nil || len(atts) != 3 {
		t.Fatalf("attachments %v %d", err, len(atts))
	}
	if err := st.DeleteUIDs(ctx, inbox.ID, []uint32{1}); err != nil {
		t.Fatal(err)
	}
	if found, _, _ := st.ListMessages(ctx, alice.ID, Query{Search: "cafe"}); len(found) != 0 {
		t.Fatal("search index kept a deleted message")
	}
}
