package store

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// ---- messages ----

const messageCols = `m.id, m.user_id, m.account_id, m.folder_id, m.uid, m.message_id, m.in_reply_to, m.refs, m.thread_key, m.subject,
	m.from_name, m.from_addr, m.reply_to, m.to_json, m.cc_json, m.bcc_json, m.date, m.snippet, m.size,
	m.seen, m.flagged, m.answered, m.forwarded, m.draft, m.has_attachments, m.has_invite, m.category, m.list_id,
	m.list_unsubscribe, m.unsubscribe_post, m.text_part, m.html_part, m.body_text, m.body_html, m.body_fetched,
	m.snooze_until, m.pinned, m.created_at, f.role, f.name`

func addrs(s string) []Address {
	out := []Address{}
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

func addrsJSON(a []Address) string {
	if a == nil {
		a = []Address{}
	}
	b, _ := json.Marshal(a)
	return string(b)
}

func scanMessage(sc interface{ Scan(...any) error }, extra ...any) (Message, error) {
	var m Message
	var replyTo, to, cc, bcc string
	var seen, flagged, answered, forwarded, draft, hasAtt, hasInv, unsubPost, fetched, pinned int
	dst := []any{&m.ID, &m.UserID, &m.AccountID, &m.FolderID, &m.UID, &m.MessageID, &m.InReplyTo, &m.Refs, &m.ThreadKey, &m.Subject,
		&m.From.Name, &m.From.Addr, &replyTo, &to, &cc, &bcc, &m.Date, &m.Snippet, &m.Size,
		&seen, &flagged, &answered, &forwarded, &draft, &hasAtt, &hasInv, &m.Category, &m.ListID,
		&m.ListUnsubscribe, &unsubPost, &m.TextPart, &m.HTMLPart, &m.BodyText, &m.BodyHTML, &fetched,
		&m.SnoozeUntil, &pinned, &m.CreatedAt, &m.FolderRole, &m.FolderName}
	err := sc.Scan(append(dst, extra...)...)
	m.ReplyTo, m.To, m.Cc, m.Bcc = addrs(replyTo), addrs(to), addrs(cc), addrs(bcc)
	m.Seen, m.Flagged, m.Answered, m.Forwarded, m.Draft = seen == 1, flagged == 1, answered == 1, forwarded == 1, draft == 1
	m.HasAttachments, m.HasInvite, m.UnsubscribePost, m.BodyFetched, m.Pinned = hasAtt == 1, hasInv == 1, unsubPost == 1, fetched == 1, pinned == 1
	return m, err
}

// ThreadKeyFor finds the conversation a new message belongs to: the thread of
// any message it refers to (or that refers to it), else the root of its
// References, else its own Message-ID.
func (s *Store) ThreadKeyFor(ctx context.Context, uid, accountID int64, msgID, inReplyTo, refs string) string {
	ids := strings.Fields(refs)
	if inReplyTo != "" {
		ids = append(ids, inReplyTo)
	}
	if len(ids) > 0 {
		q := `SELECT thread_key FROM messages WHERE user_id = ? AND account_id = ? AND message_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) AND thread_key != '' LIMIT 1`
		args := []any{uid, accountID}
		for _, id := range ids {
			args = append(args, id)
		}
		var key string
		if err := s.DB.QueryRowContext(ctx, q, args...).Scan(&key); err == nil && key != "" {
			return key
		}
	}
	if msgID != "" {
		// a reply that arrived before this message already names it as its root
		var key string
		if err := s.DB.QueryRowContext(ctx, `SELECT thread_key FROM messages WHERE user_id = ? AND account_id = ? AND thread_key = ? LIMIT 1`, uid, accountID, msgID).Scan(&key); err == nil {
			return key
		}
	}
	switch {
	case len(strings.Fields(refs)) > 0:
		return strings.Fields(refs)[0]
	case inReplyTo != "":
		return inReplyTo
	case msgID != "":
		return msgID
	}
	return ""
}

// InsertMessage stores a message (or refreshes it when its folder/UID is
// already here) with its attachments. m.ID is set. The thread is worked out
// when m.ThreadKey is empty.
func (s *Store) InsertMessage(ctx context.Context, m *Message, atts []Attachment) error {
	if m.ThreadKey == "" {
		m.ThreadKey = s.ThreadKeyFor(ctx, m.UserID, m.AccountID, m.MessageID, m.InReplyTo, m.Refs)
		if m.ThreadKey == "" {
			m.ThreadKey = fmt.Sprintf("local:%d:%d", m.FolderID, m.UID)
		}
	}
	if m.Category == "" {
		m.Category = "people"
	}
	if m.Date == "" {
		m.Date = Now()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `INSERT INTO messages (user_id, account_id, folder_id, uid, message_id, in_reply_to, refs, thread_key, subject,
		from_name, from_addr, reply_to, to_json, cc_json, bcc_json, date, snippet, size, seen, flagged, answered, forwarded, draft,
		has_attachments, has_invite, category, list_id, list_unsubscribe, unsubscribe_post, text_part, html_part, body_text, body_html, body_fetched, snooze_until, pinned)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(folder_id, uid) DO UPDATE SET seen = excluded.seen, flagged = excluded.flagged, answered = excluded.answered,
			forwarded = excluded.forwarded, draft = excluded.draft
		RETURNING id`,
		m.UserID, m.AccountID, m.FolderID, m.UID, m.MessageID, m.InReplyTo, m.Refs, m.ThreadKey, m.Subject,
		m.From.Name, m.From.Addr, addrsJSON(m.ReplyTo), addrsJSON(m.To), addrsJSON(m.Cc), addrsJSON(m.Bcc), m.Date, m.Snippet, m.Size,
		boolInt(m.Seen), boolInt(m.Flagged), boolInt(m.Answered), boolInt(m.Forwarded), boolInt(m.Draft),
		boolInt(m.HasAttachments || len(nonInline(atts)) > 0), boolInt(m.HasInvite), m.Category, m.ListID, m.ListUnsubscribe, boolInt(m.UnsubscribePost),
		m.TextPart, m.HTMLPart, m.BodyText, m.BodyHTML, boolInt(m.BodyFetched), m.SnoozeUntil, boolInt(m.Pinned)).Scan(&m.ID)
	if err != nil {
		return err
	}
	var have int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM attachments WHERE message_id = ?`, m.ID).Scan(&have)
	if have == 0 {
		for i := range atts {
			a := &atts[i]
			a.UserID, a.MessageID = m.UserID, m.ID
			if a.MIME == "" {
				a.MIME = "application/octet-stream"
			}
			res, err := tx.ExecContext(ctx, `INSERT INTO attachments (user_id, message_id, part, filename, mime, size, encoding, content_id, inline, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				a.UserID, a.MessageID, a.Part, a.Filename, a.MIME, a.Size, a.Encoding, a.ContentID, boolInt(a.Inline), a.Data)
			if err != nil {
				return err
			}
			a.ID, _ = res.LastInsertId()
		}
	}
	return tx.Commit()
}

func nonInline(atts []Attachment) []Attachment {
	var out []Attachment
	for _, a := range atts {
		if !a.Inline {
			out = append(out, a)
		}
	}
	return out
}

// FolderUIDs maps every UID here in a folder to its message id and flags (engine).
type UIDState struct {
	ID                                        int64
	Seen, Flagged, Answered, Forwarded, Draft bool
}

// FolderUIDs lists what is stored for a folder, by UID.
func (s *Store) FolderUIDs(ctx context.Context, folderID int64) (map[uint32]UIDState, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT uid, id, seen, flagged, answered, forwarded, draft FROM messages WHERE folder_id = ? AND uid > 0`, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uint32]UIDState{}
	for rows.Next() {
		var uid int64
		var st UIDState
		var a, b, c, d, e int
		if err := rows.Scan(&uid, &st.ID, &a, &b, &c, &d, &e); err != nil {
			return nil, err
		}
		st.Seen, st.Flagged, st.Answered, st.Forwarded, st.Draft = a == 1, b == 1, c == 1, d == 1, e == 1
		out[uint32(uid)] = st
	}
	return out, rows.Err()
}

// Flags are the IMAP system flags kept here.
type Flags struct {
	Seen      *bool `json:"seen,omitempty"`
	Flagged   *bool `json:"flagged,omitempty"`
	Answered  *bool `json:"answered,omitempty"`
	Forwarded *bool `json:"forwarded,omitempty"`
	Draft     *bool `json:"draft,omitempty"`
}

// SetFlags changes the given flags of one message (no person check; callers check).
func (s *Store) SetFlags(ctx context.Context, id int64, f Flags) error {
	sets, args := []string{}, []any{}
	add := func(col string, v *bool) {
		if v != nil {
			sets = append(sets, col+" = ?")
			args = append(args, boolInt(*v))
		}
	}
	add("seen", f.Seen)
	add("flagged", f.Flagged)
	add("answered", f.Answered)
	add("forwarded", f.Forwarded)
	add("draft", f.Draft)
	if len(sets) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.DB.ExecContext(ctx, `UPDATE messages SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

// SetBody stores the fetched text of a message.
func (s *Store) SetBody(ctx context.Context, id int64, text, html, snippet string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE messages SET body_text = ?, body_html = ?, snippet = CASE WHEN ? != '' THEN ? ELSE snippet END, body_fetched = 1 WHERE id = ?`, text, html, snippet, snippet, id)
	return err
}

// SetUID gives a moved message its new UID (or moves it locally to another folder).
func (s *Store) SetUID(ctx context.Context, id, folderID, uid int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE messages SET folder_id = ?, uid = ? WHERE id = ?`, folderID, uid, id)
	return conflict(err)
}

// DeleteMessage removes one message here.
func (s *Store) DeleteMessage(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM messages WHERE id = ?`, id)
	return err
}

// DeleteUIDs removes the messages of a folder with these UIDs (gone on the server).
func (s *Store) DeleteUIDs(ctx context.Context, folderID int64, uids []uint32) error {
	for len(uids) > 0 {
		n := len(uids)
		if n > 500 {
			n = 500
		}
		q := `DELETE FROM messages WHERE folder_id = ? AND uid IN (` + strings.TrimSuffix(strings.Repeat("?,", n), ",") + `)`
		args := []any{folderID}
		for _, u := range uids[:n] {
			args = append(args, int64(u))
		}
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		uids = uids[n:]
	}
	return nil
}

// ClearFolder removes every message of a folder (its UIDVALIDITY changed).
func (s *Store) ClearFolder(ctx context.Context, folderID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM messages WHERE folder_id = ? AND uid > 0`, folderID)
	return err
}

// PruneOlder removes messages of a folder older than the account's window
// (not flagged, not pinned, not snoozed).
func (s *Store) PruneOlder(ctx context.Context, folderID int64, before string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM messages WHERE folder_id = ? AND date < ? AND flagged = 0 AND pinned = 0 AND snooze_until = ''`, folderID, before)
	return err
}

// GetMessage loads a person's message (uid 0 = any; engine only).
func (s *Store) GetMessage(ctx context.Context, uid, id int64) (Message, error) {
	q := `SELECT ` + messageCols + ` FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.id = ?`
	args := []any{id}
	if uid > 0 {
		q += ` AND m.user_id = ?`
		args = append(args, uid)
	}
	m, err := scanMessage(s.DB.QueryRowContext(ctx, q, args...))
	return m, notFound(err)
}

// MessagesByIDs loads several of a person's messages (missing ids are skipped).
func (s *Store) MessagesByIDs(ctx context.Context, uid int64, ids []int64) ([]Message, error) {
	if len(ids) == 0 {
		return []Message{}, nil
	}
	q := `SELECT ` + messageCols + ` FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.user_id = ? AND m.id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) ORDER BY m.date`
	args := []any{uid}
	for _, id := range ids {
		args = append(args, id)
	}
	return s.queryMessages(ctx, q, args...)
}

func (s *Store) queryMessages(ctx context.Context, q string, args ...any) ([]Message, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Thread lists the messages of one conversation, oldest first (trash and spam
// only when the conversation lives there).
func (s *Store) Thread(ctx context.Context, uid, accountID int64, threadKey string) ([]Message, error) {
	return s.queryMessages(ctx, `SELECT `+messageCols+` FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND m.account_id = ? AND m.thread_key = ? ORDER BY m.date, m.id`, uid, accountID, threadKey)
}

// SetSnooze hides a person's messages until a time ("" = wake now).
func (s *Store) SetSnooze(ctx context.Context, uid int64, ids []int64, until string) error {
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `UPDATE messages SET snooze_until = ? WHERE id = ? AND user_id = ?`, until, id, uid); err != nil {
			return err
		}
	}
	return nil
}

// WakeSnoozed ends every snooze that is due and returns the woken messages.
func (s *Store) WakeSnoozed(ctx context.Context) ([]Message, error) {
	now := Now()
	list, err := s.queryMessages(ctx, `SELECT `+messageCols+` FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.snooze_until != '' AND m.snooze_until <= ?`, now)
	if err != nil {
		return nil, err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE messages SET snooze_until = '', seen = 0 WHERE snooze_until != '' AND snooze_until <= ?`, now)
	return list, err
}

// SetPinned keeps a person's message on top of the inbox.
func (s *Store) SetPinned(ctx context.Context, uid, id int64, on bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE messages SET pinned = ? WHERE id = ? AND user_id = ?`, boolInt(on), id, uid)
	return err
}

// SetCategory moves a person's message to another inbox tab.
func (s *Store) SetCategory(ctx context.Context, uid, id int64, cat string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE messages SET category = ? WHERE id = ? AND user_id = ?`, cat, id, uid)
	return err
}

// ---- lists, boxes and search ----

// Boxes are the unified views across every account.
var Boxes = []string{"inbox", "unread", "starred", "snoozed", "drafts", "sent", "archive", "spam", "trash", "all", "attachments", "receipts", "newsletters", "updates", "people", "vip"}

// Query selects messages for a list.
type Query struct {
	Box           string // one of Boxes; "" with FolderID
	AccountID     int64
	FolderID      int64
	Category      string // people | updates | newsletters | receipts (inbox tabs)
	Unread        bool
	Starred       bool
	HasAttachment bool
	From          string // exact sender address
	Search        string // words and operators (see ParseSearch)
	Conversations bool
	Offset, Limit int
}

// Search is a parsed search box.
type Search struct {
	Words      []string
	From, To   string
	Subject    string
	In         string // a box
	Before     string // YYYY-MM-DD
	After      string
	Unread     bool
	Read       bool
	Starred    bool
	Attachment bool
	Filename   string
	Account    string
}

var opRe = regexp.MustCompile(`(?i)\b(from|to|subject|in|before|after|is|has|filename|account|older_than|newer_than):("[^"]*"|\S+)`)

// ParseSearch reads Gmail-style operators: from: to: subject: in: before:
// after: is:unread|read|starred has:attachment filename: account:
// newer_than:7d older_than:1m. Everything else is searched as words.
func ParseSearch(q string) Search {
	var s Search
	rest := opRe.ReplaceAllStringFunc(q, func(m string) string {
		sub := opRe.FindStringSubmatch(m)
		k, v := strings.ToLower(sub[1]), strings.Trim(sub[2], `"`)
		switch k {
		case "from":
			s.From = v
		case "to":
			s.To = v
		case "subject":
			s.Subject = v
		case "in":
			s.In = strings.ToLower(v)
		case "before":
			s.Before = v
		case "after":
			s.After = v
		case "is":
			switch strings.ToLower(v) {
			case "unread":
				s.Unread = true
			case "read":
				s.Read = true
			case "starred", "flagged":
				s.Starred = true
			}
		case "has":
			if strings.HasPrefix(strings.ToLower(v), "attach") {
				s.Attachment = true
			}
		case "filename":
			s.Filename = v
		case "account":
			s.Account = v
		case "newer_than", "older_than":
			if d := relDate(v); d != "" {
				if k == "newer_than" {
					s.After = d
				} else {
					s.Before = d
				}
			}
		}
		return " "
	})
	for _, w := range strings.FieldsFunc(rest, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '@' && r != '.' && r != '-' && r != '_'
	}) {
		w = strings.Trim(w, ".-_")
		if w != "" {
			s.Words = append(s.Words, w)
		}
	}
	return s
}

func relDate(v string) string {
	if len(v) < 2 {
		return ""
	}
	var n int
	if _, err := fmt.Sscanf(v[:len(v)-1], "%d", &n); err != nil || n <= 0 {
		return ""
	}
	t := time.Now()
	switch v[len(v)-1] {
	case 'd':
		t = t.AddDate(0, 0, -n)
	case 'w':
		t = t.AddDate(0, 0, -7*n)
	case 'm':
		t = t.AddDate(0, -n, 0)
	case 'y':
		t = t.AddDate(-n, 0, 0)
	default:
		return ""
	}
	return t.Format("2006-01-02")
}

// ftsQuery turns words into an FTS5 query: every word, as a prefix.
func ftsQuery(words []string) string {
	var parts []string
	for _, w := range words {
		w = strings.ReplaceAll(w, `"`, "")
		if w != "" {
			parts = append(parts, `"`+w+`"*`)
		}
	}
	return strings.Join(parts, " ")
}

const hiddenRoles = `('trash','junk')`

// boxWhere is the filter of a unified view.
func boxWhere(box string) (string, []any) {
	now := Now()
	switch box {
	case "inbox":
		return `f.role = 'inbox' AND (m.snooze_until = '' OR m.snooze_until <= ?)`, []any{now}
	case "people", "updates", "newsletters", "receipts":
		return `f.role IN ('inbox','archive','all') AND m.category = ?`, []any{box}
	case "vip":
		return `f.role NOT IN ` + hiddenRoles + ` AND m.from_addr IN (SELECT addr FROM senders WHERE user_id = m.user_id AND vip = 1)`, nil
	case "unread":
		return `m.seen = 0 AND f.role NOT IN ('trash','junk','sent','drafts')`, nil
	case "starred":
		return `m.flagged = 1 AND f.role NOT IN ` + hiddenRoles, nil
	case "snoozed":
		return `m.snooze_until > ?`, []any{now}
	case "drafts":
		return `f.role = 'drafts'`, nil
	case "sent":
		return `f.role = 'sent'`, nil
	case "archive":
		return `f.role IN ('archive','all')`, nil
	case "spam":
		return `f.role = 'junk'`, nil
	case "trash":
		return `f.role = 'trash'`, nil
	case "attachments":
		return `m.has_attachments = 1 AND f.role NOT IN ` + hiddenRoles, nil
	case "all":
		return `f.role NOT IN ` + hiddenRoles, nil
	}
	return "", nil
}

// ListMessages lists one page of a person's messages for a view, newest
// first. With Conversations each conversation is one row (its newest
// message) with ThreadCount and ThreadUnread. The second value says whether
// there are more.
func (s *Store) ListMessages(ctx context.Context, uid int64, q Query) ([]Message, bool, error) {
	where := []string{"m.user_id = ?"}
	args := []any{uid}
	add := func(w string, a ...any) {
		where = append(where, w)
		args = append(args, a...)
	}
	box := q.Box
	var sr Search
	if q.Search != "" {
		sr = ParseSearch(q.Search)
		if sr.In != "" {
			box = sr.In
		}
	}
	if q.FolderID > 0 {
		add(`m.folder_id = ?`, q.FolderID)
	} else if w, a := boxWhere(box); w != "" {
		add(w, a...)
	} else if q.Search != "" {
		add(`f.role NOT IN ` + hiddenRoles)
	} else {
		add(`f.role = 'inbox'`)
	}
	if q.AccountID > 0 {
		add(`m.account_id = ?`, q.AccountID)
	}
	if q.Category != "" {
		add(`m.category = ?`, q.Category)
	}
	if q.Unread || sr.Unread {
		add(`m.seen = 0`)
	}
	if sr.Read {
		add(`m.seen = 1`)
	}
	if q.Starred || sr.Starred {
		add(`m.flagged = 1`)
	}
	if q.HasAttachment || sr.Attachment {
		add(`m.has_attachments = 1`)
	}
	if q.From != "" {
		add(`lower(m.from_addr) = lower(?)`, q.From)
	}
	if sr.From != "" {
		add(`(m.from_addr LIKE '%' || ? || '%' OR m.from_name LIKE '%' || ? || '%')`, sr.From, sr.From)
	}
	if sr.To != "" {
		add(`(m.to_json LIKE '%' || ? || '%' OR m.cc_json LIKE '%' || ? || '%')`, sr.To, sr.To)
	}
	if sr.Subject != "" {
		add(`m.subject LIKE '%' || ? || '%'`, sr.Subject)
	}
	if sr.Before != "" {
		add(`m.date < ?`, sr.Before)
	}
	if sr.After != "" {
		add(`m.date >= ?`, sr.After)
	}
	if sr.Filename != "" {
		add(`EXISTS (SELECT 1 FROM attachments a WHERE a.message_id = m.id AND a.filename LIKE '%' || ? || '%')`, sr.Filename)
	}
	if sr.Account != "" {
		add(`m.account_id IN (SELECT id FROM accounts WHERE user_id = m.user_id AND (email LIKE '%' || ? || '%' OR label LIKE '%' || ? || '%'))`, sr.Account, sr.Account)
	}
	if fq := ftsQuery(sr.Words); fq != "" {
		add(`m.id IN (SELECT rowid FROM messages_fts WHERE messages_fts MATCH ?)`, fq)
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	order := `m.date DESC, m.id DESC`
	if box == "inbox" && q.FolderID == 0 && q.Search == "" {
		order = `m.pinned DESC, ` + order
	}
	base := `FROM messages m JOIN folders f ON f.id = m.folder_id WHERE ` + strings.Join(where, " AND ")
	var sqlq string
	if q.Conversations {
		sqlq = `WITH sel AS (SELECT m.id, m.account_id, m.thread_key, m.seen, m.date, m.pinned,
				ROW_NUMBER() OVER (PARTITION BY m.account_id, m.thread_key ORDER BY m.date DESC, m.id DESC) AS rn,
				COUNT(*) OVER (PARTITION BY m.account_id, m.thread_key) AS n,
				SUM(1 - m.seen) OVER (PARTITION BY m.account_id, m.thread_key) AS unread,
				MAX(m.pinned) OVER (PARTITION BY m.account_id, m.thread_key) AS anypin ` + base + `)
			SELECT ` + messageCols + `, sel.n, sel.unread FROM sel JOIN messages m ON m.id = sel.id JOIN folders f ON f.id = m.folder_id
			WHERE sel.rn = 1 ORDER BY ` + strings.Replace(order, "m.pinned", "sel.anypin", 1) + ` LIMIT ? OFFSET ?`
	} else {
		sqlq = `SELECT ` + messageCols + `, 1, 1 - m.seen ` + base + ` ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	}
	args = append(args, limit+1, q.Offset)
	rows, err := s.DB.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []Message{}
	for rows.Next() {
		var n, unread int
		m, err := scanMessage(rows, &n, &unread)
		if err != nil {
			return nil, false, err
		}
		m.ThreadCount, m.ThreadUnread = n, unread
		m.BodyHTML, m.BodyText = "", ""
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	if q.Conversations && len(out) > 0 {
		s.threadTotals(ctx, uid, out)
	}
	return out, more, nil
}

// threadTotals sets each row's ThreadCount to the whole conversation (every
// folder but trash and spam, one copy per Message-ID), so a thread with your
// replies in Sent shows them in its number.
func (s *Store) threadTotals(ctx context.Context, uid int64, rows []Message) {
	keys := make([]any, 0, len(rows)+1)
	keys = append(keys, uid)
	for _, m := range rows {
		keys = append(keys, m.ThreadKey)
	}
	q := `SELECT m.account_id, m.thread_key, COUNT(DISTINCT CASE WHEN m.message_id != '' THEN m.message_id ELSE 'id' || m.id END)
		FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND f.role NOT IN ('trash','junk') AND m.thread_key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(rows)), ",") + `)
		GROUP BY m.account_id, m.thread_key`
	res, err := s.DB.QueryContext(ctx, q, keys...)
	if err != nil {
		return
	}
	total := map[string]int{}
	for res.Next() {
		var acct int64
		var key string
		var n int
		if res.Scan(&acct, &key, &n) == nil {
			total[fmt.Sprintf("%d\x00%s", acct, key)] = n
		}
	}
	res.Close()
	for i := range rows {
		if n, ok := total[fmt.Sprintf("%d\x00%s", rows[i].AccountID, rows[i].ThreadKey)]; ok && n > rows[i].ThreadCount {
			rows[i].ThreadCount = n
		}
	}
}

// BoxCounts are the numbers next to the views in the sidebar.
func (s *Store) BoxCounts(ctx context.Context, uid int64) (map[string]int, error) {
	now := Now()
	out := map[string]int{}
	row := s.DB.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN f.role = 'inbox' AND m.seen = 0 AND (m.snooze_until = '' OR m.snooze_until <= ?) THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN m.seen = 0 AND f.role NOT IN ('trash','junk','sent','drafts') THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN m.flagged = 1 AND f.role NOT IN ('trash','junk') THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN m.snooze_until > ? THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'junk' AND m.seen = 0 THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'drafts' THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'inbox' AND m.seen = 0 AND m.category = 'people' AND (m.snooze_until = '' OR m.snooze_until <= ?) THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'inbox' AND m.seen = 0 AND m.category = 'updates' AND (m.snooze_until = '' OR m.snooze_until <= ?) THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'inbox' AND m.seen = 0 AND m.category = 'newsletters' AND (m.snooze_until = '' OR m.snooze_until <= ?) THEN 1 END), 0),
		COALESCE(SUM(CASE WHEN f.role = 'inbox' AND m.seen = 0 AND m.category = 'receipts' AND (m.snooze_until = '' OR m.snooze_until <= ?) THEN 1 END), 0)
		FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.user_id = ?`, now, now, now, now, now, now, uid)
	var inbox, unread, starred, snoozed, spam, srvDrafts, people, updates, news, receipts int
	if err := row.Scan(&inbox, &unread, &starred, &snoozed, &spam, &srvDrafts, &people, &updates, &news, &receipts); err != nil {
		return nil, err
	}
	var drafts, scheduled, failed int
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(SUM(status = 'draft'), 0), COALESCE(SUM(status IN ('queued','sending')), 0), COALESCE(SUM(status = 'failed'), 0) FROM drafts WHERE user_id = ?`, uid).Scan(&drafts, &scheduled, &failed)
	out["inbox"], out["unread"], out["starred"], out["snoozed"], out["spam"] = inbox, unread, starred, snoozed, spam
	out["drafts"], out["scheduled"], out["failed"] = drafts+srvDrafts, scheduled, failed
	out["people"], out["updates"], out["newsletters"], out["receipts"] = people, updates, news, receipts
	return out, nil
}

// Correspondent is someone you write with, for autocompleting addresses.
type Correspondent struct {
	Name  string `json:"name"`
	Addr  string `json:"addr"`
	Count int    `json:"count"`
	Last  string `json:"last"`
}

// Correspondents suggests addresses from the mail itself: the people you
// wrote to and who wrote to you, most frequent first.
func (s *Store) Correspondents(ctx context.Context, uid int64, q string, limit int) ([]Correspondent, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT to_json, cc_json, date FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.user_id = ? AND f.role = 'sent' ORDER BY m.date DESC LIMIT 2000`, uid)
	if err != nil {
		return nil, err
	}
	agg := map[string]*Correspondent{}
	bump := func(a Address, date string, w int) {
		k := strings.ToLower(strings.TrimSpace(a.Addr))
		if k == "" {
			return
		}
		c := agg[k]
		if c == nil {
			c = &Correspondent{Addr: a.Addr}
			agg[k] = c
		}
		if c.Name == "" && a.Name != "" {
			c.Name = a.Name
		}
		c.Count += w
		if date > c.Last {
			c.Last = date
		}
	}
	for rows.Next() {
		var to, cc, date string
		if err := rows.Scan(&to, &cc, &date); err != nil {
			rows.Close()
			return nil, err
		}
		for _, a := range append(addrs(to), addrs(cc)...) {
			bump(a, date, 3)
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT from_name, from_addr, MAX(date), COUNT(1) FROM messages WHERE user_id = ? AND category = 'people' GROUP BY lower(from_addr) ORDER BY COUNT(1) DESC LIMIT 2000`, uid)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a Address
		var date string
		var n int
		if err := rows.Scan(&a.Name, &a.Addr, &date, &n); err != nil {
			rows.Close()
			return nil, err
		}
		bump(a, date, n)
	}
	rows.Close()
	mine := map[string]bool{}
	if accts, err := s.ListAccounts(ctx, uid); err == nil {
		for _, a := range accts {
			mine[strings.ToLower(a.Email)] = true
		}
	}
	ql := strings.ToLower(strings.TrimSpace(q))
	out := []Correspondent{}
	for k, c := range agg {
		if mine[k] || strings.Contains(k, "noreply") || strings.Contains(k, "no-reply") {
			continue
		}
		if ql != "" && !strings.Contains(k, ql) && !strings.Contains(strings.ToLower(c.Name), ql) {
			continue
		}
		out = append(out, *c)
	}
	sortCorrespondents(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func sortCorrespondents(c []Correspondent) {
	for i := 1; i < len(c); i++ {
		for j := i; j > 0 && (c[j].Count > c[j-1].Count || (c[j].Count == c[j-1].Count && c[j].Last > c[j-1].Last)); j-- {
			c[j], c[j-1] = c[j-1], c[j]
		}
	}
}

// Subscription is a mailing list or newsletter you get.
type Subscription struct {
	Addr           string `json:"addr"`
	Name           string `json:"name"`
	ListID         string `json:"list_id"`
	Count          int    `json:"count"`
	Unread         int    `json:"unread"`
	Last           string `json:"last"`
	LastSubject    string `json:"last_subject"`
	LastMessageID  int64  `json:"last_message_id"`
	CanUnsubscribe bool   `json:"can_unsubscribe"`
	OneClick       bool   `json:"one_click"`
	UnsubscribedAt string `json:"unsubscribed_at"`
	Muted          bool   `json:"muted"`
	Category       string `json:"category"`
}

// Subscriptions lists the senders of list mail (newsletters, updates) with how much they send.
func (s *Store) Subscriptions(ctx context.Context, uid int64) ([]Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT lower(m.from_addr), MAX(m.from_name), MAX(m.list_id), COUNT(1), SUM(1 - m.seen), MAX(m.date),
			MAX(m.list_unsubscribe != ''), MAX(m.unsubscribe_post), MAX(m.category),
			COALESCE(sd.unsubscribed_at, ''), COALESCE(sd.muted, 0)
		FROM messages m JOIN folders f ON f.id = m.folder_id
		LEFT JOIN senders sd ON sd.user_id = m.user_id AND sd.addr = lower(m.from_addr)
		WHERE m.user_id = ? AND f.role NOT IN ('sent','drafts','trash') AND (m.list_unsubscribe != '' OR m.list_id != '' OR m.category IN ('newsletters','updates'))
		GROUP BY lower(m.from_addr) ORDER BY COUNT(1) DESC LIMIT 500`, uid)
	if err != nil {
		return nil, err
	}
	out := []Subscription{}
	for rows.Next() {
		var sub Subscription
		var can, one, muted int
		if err := rows.Scan(&sub.Addr, &sub.Name, &sub.ListID, &sub.Count, &sub.Unread, &sub.Last, &can, &one, &sub.Category, &sub.UnsubscribedAt, &muted); err != nil {
			rows.Close()
			return nil, err
		}
		sub.CanUnsubscribe, sub.OneClick, sub.Muted = can == 1, one == 1, muted == 1
		out = append(out, sub)
	}
	rows.Close()
	for i := range out {
		_ = s.DB.QueryRowContext(ctx, `SELECT id, subject FROM messages WHERE user_id = ? AND lower(from_addr) = ? ORDER BY date DESC LIMIT 1`, uid, out[i].Addr).Scan(&out[i].LastMessageID, &out[i].LastSubject)
	}
	return out, nil
}

// LatestUnsubscribe is the newest message from a sender that says how to unsubscribe.
func (s *Store) LatestUnsubscribe(ctx context.Context, uid int64, addr string) (Message, error) {
	m, err := scanMessage(s.DB.QueryRowContext(ctx, `SELECT `+messageCols+` FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND lower(m.from_addr) = lower(?) AND m.list_unsubscribe != '' ORDER BY m.date DESC LIMIT 1`, uid, addr))
	return m, notFound(err)
}

// MessageIDsFrom lists a person's messages from a sender in the views that matter (for bulk actions).
func (s *Store) MessageIDsFrom(ctx context.Context, uid int64, addr string, roles ...string) ([]int64, error) {
	q := `SELECT m.id FROM messages m JOIN folders f ON f.id = m.folder_id WHERE m.user_id = ? AND lower(m.from_addr) = lower(?)`
	args := []any{uid, addr}
	if len(roles) > 0 {
		q += ` AND f.role IN (` + strings.TrimSuffix(strings.Repeat("?,", len(roles)), ",") + `)`
		for _, r := range roles {
			args = append(args, r)
		}
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MessagesNeedingBody lists messages of an account whose text is not fetched yet, newest first (engine).
func (s *Store) MessagesNeedingBody(ctx context.Context, accountID int64, limit int) ([]Message, error) {
	return s.queryMessages(ctx, `SELECT `+messageCols+` FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.account_id = ? AND m.body_fetched = 0 AND m.uid > 0 ORDER BY m.date DESC LIMIT ?`, accountID, limit)
}

// ---- attachments ----

func scanAttachment(sc interface{ Scan(...any) error }, withData bool) (Attachment, error) {
	var a Attachment
	var inline int
	dst := []any{&a.ID, &a.UserID, &a.MessageID, &a.Part, &a.Filename, &a.MIME, &a.Size, &a.Encoding, &a.ContentID, &inline}
	if withData {
		dst = append(dst, &a.Data)
	}
	err := sc.Scan(dst...)
	a.Inline = inline == 1
	return a, err
}

const attCols = `id, user_id, message_id, part, filename, mime, size, encoding, content_id, inline`

// Attachments lists a person's message's attachments (without the data).
func (s *Store) Attachments(ctx context.Context, uid, messageID int64) ([]Attachment, error) {
	q := `SELECT ` + attCols + ` FROM attachments WHERE message_id = ?`
	args := []any{messageID}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attachment{}
	for rows.Next() {
		a, err := scanAttachment(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAttachment loads one of a person's attachments with whatever data is kept here.
func (s *Store) GetAttachment(ctx context.Context, uid, id int64) (Attachment, error) {
	q := `SELECT ` + attCols + `, data FROM attachments WHERE id = ?`
	args := []any{id}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	a, err := scanAttachment(s.DB.QueryRowContext(ctx, q, args...), true)
	return a, notFound(err)
}

// SetAttachmentData keeps an attachment's bytes here (small inline images).
func (s *Store) SetAttachmentData(ctx context.Context, id int64, data []byte) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE attachments SET data = ?, size = CASE WHEN size = 0 THEN ? ELSE size END WHERE id = ?`, data, len(data), id)
	return err
}

// AttachmentRow is an attachment with the message it came with, for the attachments page.
type AttachmentRow struct {
	Attachment
	Subject   string  `json:"subject"`
	From      Address `json:"from"`
	Date      string  `json:"date"`
	AccountID int64   `json:"account_id"`
	ThreadKey string  `json:"thread_key"`
}

// ListAttachments lists a person's files across mail, newest first (kind: images | documents | "" all).
func (s *Store) ListAttachments(ctx context.Context, uid int64, kind, q string, offset, limit int) ([]AttachmentRow, bool, error) {
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	where := `a.user_id = ? AND a.inline = 0 AND f.role NOT IN ('trash','junk')`
	args := []any{uid}
	switch kind {
	case "images":
		where += ` AND a.mime LIKE 'image/%'`
	case "documents":
		where += ` AND (a.mime = 'application/pdf' OR a.mime LIKE 'application/vnd.%' OR a.mime LIKE 'application/msword%' OR a.mime LIKE 'text/%')`
	}
	if q != "" {
		where += ` AND (a.filename LIKE '%' || ? || '%' OR m.subject LIKE '%' || ? || '%' OR m.from_name LIKE '%' || ? || '%' OR m.from_addr LIKE '%' || ? || '%')`
		args = append(args, q, q, q, q)
	}
	args = append(args, limit+1, offset)
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.user_id, a.message_id, a.part, a.filename, a.mime, a.size, a.encoding, a.content_id, a.inline,
		m.subject, m.from_name, m.from_addr, m.date, m.account_id, m.thread_key
		FROM attachments a JOIN messages m ON m.id = a.message_id JOIN folders f ON f.id = m.folder_id
		WHERE `+where+` ORDER BY m.date DESC, a.id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []AttachmentRow{}
	for rows.Next() {
		var r AttachmentRow
		var inline int
		if err := rows.Scan(&r.ID, &r.UserID, &r.MessageID, &r.Part, &r.Filename, &r.MIME, &r.Size, &r.Encoding, &r.ContentID, &inline,
			&r.Subject, &r.From.Name, &r.From.Addr, &r.Date, &r.AccountID, &r.ThreadKey); err != nil {
			return nil, false, err
		}
		r.Inline = inline == 1
		out = append(out, r)
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, rows.Err()
}
