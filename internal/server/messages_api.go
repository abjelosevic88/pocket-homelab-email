package server

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- the sidebar and the numbers ----

func (s *Server) handleSidebar(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	accts, err := s.accountViews(ctx, uid)
	if err != nil {
		s.fail(w, err, "accounts")
		return
	}
	counts, err := s.store.BoxCounts(ctx, uid)
	if err != nil {
		s.fail(w, err, "counts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accts, "counts": counts})
}

func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) {
	counts, err := s.store.BoxCounts(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "counts")
		return
	}
	writeJSON(w, http.StatusOK, counts)
}

// handleSummary is one day of mail in numbers (for Pocket Day, dashboards
// and Pocket AI): received, still unread, sent, and who wrote the most.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	date := chi.URLParam(r, "date")
	if date == "" {
		date = r.URL.Query().Get("date")
	}
	if date == "" {
		date = store.Today()
	}
	day, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	from, to := day.UTC().Format(time.RFC3339), day.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	var received, unread, people, sent int
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1), COALESCE(SUM(1 - m.seen), 0), COALESCE(SUM(m.category = 'people'), 0)
		FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND f.role NOT IN ('sent','drafts','trash','junk') AND m.date >= ? AND m.date < ?`, uid, from, to).Scan(&received, &unread, &people)
	_ = s.store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND f.role = 'sent' AND m.date >= ? AND m.date < ?`, uid, from, to).Scan(&sent)
	type who struct {
		Name  string `json:"name"`
		Addr  string `json:"addr"`
		Count int    `json:"count"`
	}
	top := []who{}
	rows, err := s.store.DB.QueryContext(ctx, `SELECT MAX(m.from_name), lower(m.from_addr), COUNT(1) FROM messages m JOIN folders f ON f.id = m.folder_id
		WHERE m.user_id = ? AND m.category = 'people' AND f.role NOT IN ('sent','drafts','trash','junk') AND m.date >= ? AND m.date < ?
		GROUP BY lower(m.from_addr) ORDER BY COUNT(1) DESC LIMIT 5`, uid, from, to)
	if err == nil {
		for rows.Next() {
			var x who
			if rows.Scan(&x.Name, &x.Addr, &x.Count) == nil {
				top = append(top, x)
			}
		}
		rows.Close()
	}
	counts, _ := s.store.BoxCounts(ctx, uid)
	writeJSON(w, http.StatusOK, map[string]any{"date": date, "received": received, "from_people": people, "unread": unread, "sent": sent,
		"top_senders": top, "inbox_unread": counts["inbox"], "url": s.cfg.BaseURL})
}

// ---- lists ----

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	qv := r.URL.Query()
	q := store.Query{
		Box: qv.Get("box"), Category: qv.Get("category"), Search: strings.TrimSpace(qv.Get("q")), From: qv.Get("from"),
		Unread: queryBool(r, "unread"), Starred: queryBool(r, "starred"), HasAttachment: queryBool(r, "attachments"),
		Offset: queryInt(r, "offset"), Limit: queryInt(r, "limit"),
	}
	if q.Box != "" && !validBox(q.Box) {
		writeErr(w, http.StatusBadRequest, "unknown view")
		return
	}
	switch q.Category {
	case "", "people", "updates", "newsletters", "receipts":
	default:
		writeErr(w, http.StatusBadRequest, "unknown category")
		return
	}
	if id := queryInt(r, "account"); id > 0 {
		if _, err := s.store.GetAccount(ctx, uid, int64(id)); err != nil {
			s.fail(w, err, "account")
			return
		}
		q.AccountID = int64(id)
	}
	if id := queryInt(r, "folder"); id > 0 {
		f, err := s.store.GetFolder(ctx, uid, int64(id))
		if err != nil {
			s.fail(w, err, "folder")
			return
		}
		q.FolderID, q.AccountID = f.ID, f.AccountID
	}
	if v := qv.Get("conversations"); v != "" {
		q.Conversations = queryBool(r, "conversations")
	} else {
		st, _ := s.store.GetSettings(ctx, uid)
		q.Conversations = st.Conversations
	}
	if q.Search != "" || q.Box == "drafts" || q.Box == "snoozed" {
		q.Conversations = false
	}
	list, more, err := s.store.ListMessages(ctx, uid, q)
	if err != nil {
		s.fail(w, err, "messages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": list, "more": more})
}

func validBox(b string) bool {
	for _, x := range store.Boxes {
		if x == b {
			return true
		}
	}
	return false
}

// ---- one message, one conversation ----

// messageFull is one message as the reader shows it.
type messageFull struct {
	Message       store.Message      `json:"message"`
	HTML          string             `json:"html"`
	Text          string             `json:"text"`
	ImagesBlocked int                `json:"images_blocked"`
	Attachments   []store.Attachment `json:"attachments"`
	Invite        *pmail.Invite      `json:"invite"`
	Unsubscribe   pmail.Unsubscribe  `json:"unsubscribe"`
	Sender        store.Sender       `json:"sender"`
	Links         []store.AppLink    `json:"links"`
}

func (s *Server) messageFull(ctx context.Context, m store.Message, st store.Settings, showImages bool) messageFull {
	if !m.BodyFetched {
		bctx, cancel := context.WithTimeout(ctx, 25*time.Second)
		if err := s.mail.EnsureBody(bctx, &m); err != nil {
			s.log.Debug("fetch body", "message", m.ID, "err", err)
		}
		cancel()
	}
	out := messageFull{Message: m, Text: m.BodyText, Attachments: []store.Attachment{}, Links: []store.AppLink{}}
	out.Sender = s.store.GetSender(ctx, m.UserID, m.From.Addr)
	atts, _ := s.store.Attachments(ctx, m.UserID, m.ID)
	if m.BodyHTML != "" {
		allow := showImages || out.Sender.AllowImages || st.RemoteImages == "always"
		inline := map[string]string{}
		for _, a := range atts {
			if a.ContentID != "" {
				inline[strings.Trim(a.ContentID, "<>")] = fmt.Sprintf("/api/v1/attachments/%d?inline=1", a.ID)
			}
		}
		out.HTML, out.ImagesBlocked = pmail.Sanitize(m.BodyHTML, pmail.SanitizeOptions{AllowRemote: allow, Inline: inline})
		if out.Text == "" {
			out.Text = pmail.HTMLToText(m.BodyHTML)
		}
	}
	for _, a := range atts {
		cid := strings.Trim(a.ContentID, "<>")
		if a.Inline && cid != "" && strings.Contains(m.BodyHTML, cid) {
			continue // shown in the body
		}
		if a.MIME == "text/calendar" || a.MIME == "application/ics" {
			if out.Invite == nil {
				if data, err := s.attachmentBytes(ctx, m, a); err == nil {
					if inv, err := pmail.ParseInvite(data); err == nil {
						out.Invite = inv
					}
				}
			}
			if a.Filename == "" {
				continue
			}
		}
		out.Attachments = append(out.Attachments, a)
	}
	out.Unsubscribe = pmail.ParseUnsubscribe(m.ListUnsubscribe, m.UnsubscribePost)
	if links, err := s.store.AppLinksForThread(ctx, m.UserID, "\x00", []int64{m.ID}); err == nil {
		out.Links = links
	}
	return out
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMessage(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "message")
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	writeJSON(w, http.StatusOK, s.messageFull(ctx, m, st, queryBool(r, "images")))
}

func (s *Server) handleRawMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := s.store.GetMessage(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "message")
		return
	}
	b, err := s.mail.Raw(ctx, m)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	name := safeFilename(firstNonEmpty(m.Subject, "message")) + ".eml"
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", contentDisposition("attachment", name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

var reSubjectPrefix = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|aw|wg|sv|odg|vs)\s*(\[\d+\])?\s*:\s*)+`)

// baseSubject is a subject without its Re:/Fwd: prefixes.
func baseSubject(s string) string { return strings.TrimSpace(reSubjectPrefix.ReplaceAllString(s, "")) }

// handleThread is the conversation of a message: its messages oldest first,
// one copy of each (a message filed in two folders shows once), trash and
// spam only when the opened message is there.
func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	m, err := s.store.GetMessage(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "message")
		return
	}
	list, err := s.store.Thread(ctx, uid, m.AccountID, m.ThreadKey)
	if err != nil {
		s.fail(w, err, "conversation")
		return
	}
	hidden := m.FolderRole == "trash" || m.FolderRole == "junk"
	seen := map[string]int{}
	var keep []store.Message
	for _, x := range list {
		if !hidden && (x.FolderRole == "trash" || x.FolderRole == "junk") && x.ID != m.ID {
			continue
		}
		if x.MessageID != "" {
			if i, ok := seen[x.MessageID]; ok {
				// prefer the copy in the folder that was opened, else the inbox
				if x.ID == m.ID || (keep[i].ID != m.ID && x.FolderRole == "inbox") {
					keep[i] = x
				}
				continue
			}
			seen[x.MessageID] = len(keep)
		}
		keep = append(keep, x)
	}
	if len(keep) == 0 {
		keep = []store.Message{m}
	}
	st, _ := s.store.GetSettings(ctx, uid)
	images := queryBool(r, "images")
	out := make([]messageFull, 0, len(keep))
	ids := make([]int64, 0, len(keep))
	for _, x := range keep {
		out = append(out, s.messageFull(ctx, x, st, images))
		ids = append(ids, x.ID)
	}
	links, _ := s.store.AppLinksForThread(ctx, uid, m.ThreadKey, ids)
	if links == nil {
		links = []store.AppLink{}
	}
	subject := baseSubject(keep[0].Subject)
	if subject == "" {
		subject = baseSubject(m.Subject)
	}
	writeJSON(w, http.StatusOK, map[string]any{"subject": subject, "account_id": m.AccountID, "thread_key": m.ThreadKey, "messages": out, "links": links})
}

// ---- changes ----

type actionRequest struct {
	IDs      []int64 `json:"ids"`
	Action   string  `json:"action"`
	Thread   bool    `json:"thread"`
	FolderID int64   `json:"folder_id"`
	Until    string  `json:"until"`
	Category string  `json:"category"`
}

var actionWords = map[string][2]string{
	"read": {"Marked as read", ""}, "unread": {"Marked as unread", ""}, "star": {"Starred", ""}, "unstar": {"Star removed", ""},
	"archive": {"Archived", ""}, "trash": {"Moved to Trash", ""}, "spam": {"Marked as spam", ""}, "notspam": {"Moved to the inbox", ""},
	"inbox": {"Moved to the inbox", ""}, "delete": {"Deleted for good", ""}, "move": {"Moved", ""},
	"snooze": {"Snoozed", ""}, "unsnooze": {"Back in the inbox", ""}, "pin": {"Pinned", ""}, "unpin": {"Unpinned", ""}, "category": {"Moved to another tab", ""},
}

// expand turns message ids into the messages they stand for: with thread,
// every message of their conversations (for moves only those in the same
// folder as the given message, so a reply in Sent stays there).
func (s *Server) expand(ctx context.Context, uid int64, ids []int64, thread, sameFolder bool) ([]int64, error) {
	msgs, err := s.store.MessagesByIDs(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	seen := map[int64]bool{}
	add := func(id int64) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, m := range msgs {
		add(m.ID)
		if !thread || m.ThreadKey == "" {
			continue
		}
		list, err := s.store.Thread(ctx, uid, m.AccountID, m.ThreadKey)
		if err != nil {
			return nil, err
		}
		for _, x := range list {
			if sameFolder && x.FolderID != m.FolderID {
				continue
			}
			if !sameFolder && (x.FolderRole == "trash" || x.FolderRole == "junk") && m.FolderRole != x.FolderRole {
				continue
			}
			add(x.ID)
		}
	}
	return out, nil
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in actionRequest
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	words, ok := actionWords[in.Action]
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown action")
		return
	}
	if len(in.IDs) == 0 || len(in.IDs) > 1000 {
		writeErr(w, http.StatusBadRequest, "pick 1 to 1000 messages")
		return
	}
	moving := map[string]bool{"archive": true, "trash": true, "spam": true, "notspam": true, "inbox": true, "delete": true, "move": true, "snooze": true, "unsnooze": true}[in.Action]
	ids, err := s.expand(ctx, uid, in.IDs, in.Thread, moving)
	if err != nil {
		s.fail(w, err, "messages")
		return
	}
	if len(ids) == 0 {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	ops := []int64{}
	switch in.Action {
	case "snooze":
		t, err := time.Parse(time.RFC3339, in.Until)
		if err != nil || t.Before(time.Now()) {
			writeErr(w, http.StatusBadRequest, "pick a time in the future")
			return
		}
		err = s.store.SetSnooze(ctx, uid, ids, t.UTC().Format(time.RFC3339Nano))
		if err != nil {
			s.fail(w, err, "snooze")
			return
		}
		words[0] = "Snoozed until " + t.Local().Format("Mon 2 Jan, 15:04")
	case "unsnooze":
		if err := s.store.SetSnooze(ctx, uid, ids, ""); err != nil {
			s.fail(w, err, "snooze")
			return
		}
	case "pin", "unpin":
		for _, id := range ids {
			if err := s.store.SetPinned(ctx, uid, id, in.Action == "pin"); err != nil {
				s.fail(w, err, "pin")
				return
			}
		}
	case "category":
		switch in.Category {
		case "people", "updates", "newsletters", "receipts":
		default:
			writeErr(w, http.StatusBadRequest, "unknown category")
			return
		}
		for _, id := range ids {
			if err := s.store.SetCategory(ctx, uid, id, in.Category); err != nil {
				s.fail(w, err, "category")
				return
			}
		}
	default:
		act := pmail.Action{Kind: in.Action}
		if in.Action == "move" {
			f, err := s.store.GetFolder(ctx, uid, in.FolderID)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "pick a folder")
				return
			}
			act.FolderID = f.ID
			// a folder belongs to one account: only that account's messages move
			msgs, _ := s.store.MessagesByIDs(ctx, uid, ids)
			ids = ids[:0]
			for _, m := range msgs {
				if m.AccountID == f.AccountID && m.FolderID != f.ID {
					ids = append(ids, m.ID)
				}
			}
			if len(ids) == 0 {
				writeErr(w, http.StatusBadRequest, "these messages are in another account or already in that folder")
				return
			}
			words[0] = "Moved to " + firstNonEmpty(f.Display, f.Name)
		}
		ops, err = s.mail.Apply(ctx, uid, ids, act)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		if ops == nil {
			ops = []int64{}
		}
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": 0, "new": []int64{}})
	n := len(in.IDs)
	what := "message"
	if in.Thread {
		what = "conversation"
	}
	if n != 1 {
		what += "s"
	}
	msg := words[0]
	if n > 1 {
		msg = fmt.Sprintf("%s · %d %s", words[0], n, what)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "ops": ops, "affected": ids, "message": msg})
}

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		Ops []int64 `json:"ops"`
	}
	if err := decode(r, &in); err != nil || len(in.Ops) == 0 {
		writeErr(w, http.StatusBadRequest, "nothing to undo")
		return
	}
	uid := actorID(ctx)
	if err := s.mail.Undo(ctx, uid, in.Ops); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": 0, "new": []int64{}})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- attachments ----

// attachmentBytes is an attachment's content: what is kept here (demo mail,
// small inline parts) or else fetched from the mail server.
func (s *Server) attachmentBytes(ctx context.Context, m store.Message, a store.Attachment) ([]byte, error) {
	if a.Data == nil {
		if full, err := s.store.GetAttachment(ctx, m.UserID, a.ID); err == nil {
			a = full
		}
	}
	return s.mail.AttachmentData(ctx, m, a)
}

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kind := r.URL.Query().Get("kind")
	if kind != "" && kind != "images" && kind != "documents" {
		writeErr(w, http.StatusBadRequest, "kind is images or documents")
		return
	}
	list, more, err := s.store.ListAttachments(ctx, actorID(ctx), kind, strings.TrimSpace(r.URL.Query().Get("q")), queryInt(r, "offset"), queryInt(r, "limit"))
	if err != nil {
		s.fail(w, err, "attachments")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attachments": list, "more": more})
}

// inlineSafe are the types shown in the browser; everything else downloads
// (an HTML or SVG attachment must never run on this origin).
var inlineSafe = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/bmp": true,
	"application/pdf": true, "text/plain": true, "audio/mpeg": true, "audio/mp4": true, "video/mp4": true}

func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetAttachment(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "attachment")
		return
	}
	m, err := s.store.GetMessage(ctx, uid, a.MessageID)
	if err != nil {
		s.fail(w, err, "message")
		return
	}
	data, err := s.mail.AttachmentData(ctx, m, a)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	mime := strings.ToLower(a.MIME)
	disp := "attachment"
	if !queryBool(r, "download") && inlineSafe[mime] {
		disp = "inline"
	}
	if !inlineSafe[mime] {
		mime = "application/octet-stream"
		if a.MIME != "" {
			mime = a.MIME
		}
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	name := a.Filename
	if name == "" {
		name = "attachment-" + strconv.FormatInt(a.ID, 10)
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", contentDisposition(disp, name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

var unsafeName = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func safeFilename(s string) string {
	s = strings.TrimSpace(unsafeName.ReplaceAllString(s, " "))
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	if s == "" {
		s = "file"
	}
	return s
}

// contentDisposition writes the header with an ASCII fallback and the UTF-8 name.
func contentDisposition(kind, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, kind, ascii, pathEscape(name))
}

func pathEscape(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.IndexByte("-._~", c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
