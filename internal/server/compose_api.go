package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/microcosm-cc/bluemonday"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- writing: drafts, replies, forwards and the outbox ----

// userHTML is what a person may write: formatting, links, lists, quotes,
// tables and images — no scripts, no forms, no event handlers.
var userHTML = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowStyling()
	p.AllowAttrs("style").Globally()
	p.AllowAttrs("class").Globally()
	p.AllowElements("font", "center", "span", "div", "u", "s", "strike", "small", "big")
	p.AllowAttrs("color", "face", "size").OnElements("font")
	p.AllowAttrs("align", "valign", "width", "height", "bgcolor", "border", "cellpadding", "cellspacing").OnElements("table", "tr", "td", "th", "img", "div", "p")
	p.AllowURLSchemes("http", "https", "mailto", "tel", "data", "cid")
	p.RequireNoReferrerOnLinks(true)
	return p
}()

func sanitizeUserHTML(h string) string { return strings.TrimSpace(userHTML.Sanitize(h)) }

// textToHTML shows plain text as HTML (line breaks kept).
func textToHTML(t string) string {
	return strings.ReplaceAll(html.EscapeString(strings.TrimRight(t, "\n")), "\n", "<br>")
}

func signatureHTML(a store.Account) string {
	if strings.TrimSpace(a.Signature) == "" {
		return ""
	}
	return `<div class="pm-signature">` + a.Signature + `</div>`
}

func addrList(as []store.Address) string {
	parts := make([]string, 0, len(as))
	for _, a := range as {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s &lt;%s&gt;", html.EscapeString(a.Name), html.EscapeString(a.Addr)))
		} else {
			parts = append(parts, html.EscapeString(a.Addr))
		}
	}
	return strings.Join(parts, ", ")
}

// quoteHTML is the original under a reply or a forward.
func (s *Server) quoteHTML(ctx context.Context, m store.Message, forward bool) string {
	if !m.BodyFetched {
		bctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		_ = s.mail.EnsureBody(bctx, &m)
		cancel()
	}
	body := ""
	if m.BodyHTML != "" {
		body, _ = pmail.Sanitize(m.BodyHTML, pmail.SanitizeOptions{AllowRemote: true})
	}
	if strings.TrimSpace(body) == "" {
		body = textToHTML(m.BodyText)
	}
	when := m.Date
	if t, err := time.Parse(time.RFC3339Nano, m.Date); err == nil {
		when = t.Local().Format("Mon, 2 Jan 2006 at 15:04")
	}
	who := m.From.Name
	if who == "" {
		who = m.From.Addr
	}
	if forward {
		head := fmt.Sprintf(`<div class="pm-fwd-head">---------- Forwarded message ----------<br>From: %s<br>Date: %s<br>Subject: %s<br>To: %s`,
			addrList([]store.Address{m.From}), html.EscapeString(when), html.EscapeString(m.Subject), addrList(m.To))
		if len(m.Cc) > 0 {
			head += "<br>Cc: " + addrList(m.Cc)
		}
		return head + `</div><br>` + body
	}
	return fmt.Sprintf(`<div class="pm-quote-head">On %s, %s wrote:</div><blockquote class="pm-quote" type="cite">%s</blockquote>`,
		html.EscapeString(when), html.EscapeString(who), body)
}

func sameAddr(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

// pickAccount is the account a new message is written from.
func (s *Server) pickAccount(ctx context.Context, uid, want int64) (store.Account, error) {
	if want > 0 {
		return s.store.GetAccount(ctx, uid, want)
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if st.DefaultAccount > 0 {
		if a, err := s.store.GetAccount(ctx, uid, st.DefaultAccount); err == nil {
			return a, nil
		}
	}
	list, err := s.store.ListAccounts(ctx, uid)
	if err != nil {
		return store.Account{}, err
	}
	if len(list) == 0 {
		return store.Account{}, store.ErrNotFound
	}
	return list[0], nil
}

type composeRequest struct {
	Kind      string          `json:"kind"`
	MessageID int64           `json:"message_id"`
	AccountID int64           `json:"account_id"`
	To        []store.Address `json:"to"`
	Subject   string          `json:"subject"`
	BodyHTML  string          `json:"body_html"`
}

// handleCompose starts a draft: new, or a reply / reply all / forward of a
// message with the recipients, subject, quote (and for a forward the
// attachments) already in place.
func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in composeRequest
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Kind == "" {
		in.Kind = "new"
	}
	d := store.Draft{UserID: uid, Kind: in.Kind, To: cleanAddrs(in.To), Subject: strings.TrimSpace(in.Subject)}
	var orig store.Message
	if in.Kind != "new" {
		if in.Kind != "reply" && in.Kind != "reply_all" && in.Kind != "forward" {
			writeErr(w, http.StatusBadRequest, "kind is new, reply, reply_all or forward")
			return
		}
		var err error
		if orig, err = s.store.GetMessage(ctx, uid, in.MessageID); err != nil {
			s.fail(w, err, "message")
			return
		}
		id := orig.ID
		d.ReplyToID = &id
		if in.AccountID == 0 {
			in.AccountID = orig.AccountID
		}
	}
	a, err := s.pickAccount(ctx, uid, in.AccountID)
	if err != nil {
		if err == store.ErrNotFound {
			writeErr(w, http.StatusConflict, "add a mail account first (Settings → Accounts)")
			return
		}
		s.fail(w, err, "account")
		return
	}
	d.AccountID = a.ID
	sig := signatureHTML(a)
	switch in.Kind {
	case "new":
		body := sanitizeUserHTML(in.BodyHTML)
		if body == "" {
			body = "<p><br></p>"
		}
		if sig != "" {
			body += "<p><br></p>" + sig
		}
		d.BodyHTML = body
	case "reply", "reply_all":
		mine := s.myAddresses(ctx, uid)
		fromMe := mine[strings.ToLower(orig.From.Addr)]
		switch {
		case fromMe:
			d.To = append([]store.Address(nil), orig.To...)
		case len(orig.ReplyTo) > 0:
			d.To = append([]store.Address(nil), orig.ReplyTo...)
		default:
			d.To = []store.Address{orig.From}
		}
		if in.Kind == "reply_all" {
			seen := map[string]bool{}
			for _, x := range d.To {
				seen[strings.ToLower(x.Addr)] = true
			}
			for _, x := range append(append([]store.Address{}, orig.To...), orig.Cc...) {
				k := strings.ToLower(x.Addr)
				if seen[k] || mine[k] {
					continue
				}
				seen[k] = true
				d.Cc = append(d.Cc, x)
			}
		}
		d.Subject = "Re: " + baseSubject(orig.Subject)
		d.BodyHTML = "<p><br></p>" + nonEmpty(sig, "") + s.quoteHTML(ctx, orig, false)
	case "forward":
		d.Subject = "Fwd: " + baseSubject(orig.Subject)
		d.To = cleanAddrs(in.To)
		d.BodyHTML = "<p><br></p>" + nonEmpty(sig, "") + s.quoteHTML(ctx, orig, true)
	}
	if in.Subject != "" {
		d.Subject = in.Subject
	}
	if err := s.store.CreateDraft(ctx, &d); err != nil {
		s.fail(w, err, "draft")
		return
	}
	if in.Kind == "forward" {
		atts, _ := s.store.Attachments(ctx, uid, orig.ID)
		limit := int64(s.cfg.MaxAttachmentMB) << 20
		var total int64
		for _, at := range atts {
			if at.Inline && at.ContentID != "" && strings.Contains(orig.BodyHTML, strings.Trim(at.ContentID, "<>")) {
				continue
			}
			data, err := s.attachmentBytes(ctx, orig, at)
			if err != nil || total+int64(len(data)) > limit {
				continue
			}
			total += int64(len(data))
			f := store.DraftFile{UserID: uid, DraftID: d.ID, Filename: firstNonEmpty(at.Filename, "attachment"), MIME: at.MIME, Data: data}
			_ = s.store.AddDraftFile(ctx, &f)
		}
		d, _ = s.store.GetDraft(ctx, uid, d.ID)
	}
	writeJSON(w, http.StatusCreated, d)
}

func nonEmpty(s, sep string) string {
	if s == "" {
		return ""
	}
	return s + sep
}

// myAddresses are the person's own addresses (every account), lowercased.
func (s *Server) myAddresses(ctx context.Context, uid int64) map[string]bool {
	out := map[string]bool{}
	if list, err := s.store.ListAccounts(ctx, uid); err == nil {
		for _, a := range list {
			out[strings.ToLower(a.Email)] = true
		}
	}
	return out
}

// cleanAddrs trims addresses and drops empty ones.
func cleanAddrs(in []store.Address) []store.Address {
	out := []store.Address{}
	for _, a := range in {
		a.Addr, a.Name = strings.TrimSpace(a.Addr), strings.TrimSpace(a.Name)
		if a.Addr == "" {
			continue
		}
		if p, err := mail.ParseAddress(a.Addr); err == nil {
			a.Addr = p.Address
			if a.Name == "" {
				a.Name = p.Name
			}
		}
		out = append(out, a)
	}
	return out
}

func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var statuses []string
	for _, st := range strings.Split(r.URL.Query().Get("status"), ",") {
		switch st = strings.TrimSpace(st); st {
		case "draft", "queued", "sending", "sent", "failed":
			statuses = append(statuses, st)
		}
	}
	if len(statuses) == 0 {
		statuses = []string{"draft"}
	}
	list, err := s.store.ListDrafts(ctx, actorID(ctx), statuses...)
	if err != nil {
		s.fail(w, err, "drafts")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": list})
}

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.GetDraft(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "draft")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDraft(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "draft")
		return
	}
	var in struct {
		AccountID int64           `json:"account_id"`
		To        []store.Address `json:"to"`
		Cc        []store.Address `json:"cc"`
		Bcc       []store.Address `json:"bcc"`
		Subject   string          `json:"subject"`
		BodyHTML  string          `json:"body_html"`
	}
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["account_id"] && in.AccountID != d.AccountID {
		if _, err := s.store.GetAccount(ctx, uid, in.AccountID); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown account")
			return
		}
		d.AccountID = in.AccountID
	}
	if keys["to"] {
		d.To = cleanAddrs(in.To)
	}
	if keys["cc"] {
		d.Cc = cleanAddrs(in.Cc)
	}
	if keys["bcc"] {
		d.Bcc = cleanAddrs(in.Bcc)
	}
	if keys["subject"] {
		d.Subject = strings.TrimSpace(strings.ReplaceAll(in.Subject, "\n", " "))
	}
	if keys["body_html"] {
		d.BodyHTML = sanitizeUserHTML(in.BodyHTML)
	}
	if err := s.store.UpdateDraft(ctx, &d); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "this message is already on its way")
			return
		}
		s.fail(w, err, "save draft")
		return
	}
	d, _ = s.store.GetDraft(ctx, uid, d.ID)
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDraft(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDraft(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "delete draft")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) maxAttach() int64 { return int64(s.cfg.MaxAttachmentMB) << 20 }

// draftSize is how many bytes a draft's files hold.
func (s *Server) draftSize(d store.Draft) int64 {
	var n int64
	for _, f := range d.Files {
		n += f.Size
	}
	return n
}

func (s *Server) handleAddDraftFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDraft(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "draft")
		return
	}
	limit := s.maxAttach()
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("attachments can be up to %d MB together", s.cfg.MaxAttachmentMB))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "could not read the file")
		return
	}
	if s.draftSize(d)+int64(len(data)) > limit {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("attachments can be up to %d MB together — share a link from Pocket Files instead", s.cfg.MaxAttachmentMB))
		return
	}
	mime := hdr.Header.Get("Content-Type")
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	f := store.DraftFile{UserID: uid, DraftID: d.ID, Filename: safeFilename(hdr.Filename), MIME: mime, Data: data}
	if err := s.store.AddDraftFile(ctx, &f); err != nil {
		s.fail(w, err, "attach")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleGetDraftFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	f, err := s.store.GetDraftFile(ctx, actorID(ctx), idParam(r, "fid"))
	if err != nil || f.DraftID != idParam(r, "id") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	mime := strings.ToLower(f.MIME)
	disp := "attachment"
	if inlineSafe[mime] && !queryBool(r, "download") {
		disp = "inline"
	} else {
		w.Header().Set("Content-Security-Policy", "sandbox")
	}
	w.Header().Set("Content-Type", firstNonEmpty(f.MIME, "application/octet-stream"))
	w.Header().Set("Content-Disposition", contentDisposition(disp, f.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(f.Data)
}

func (s *Server) handleDeleteDraftFile(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteDraftFile(r.Context(), actorID(r.Context()), idParam(r, "id"), idParam(r, "fid")); err != nil {
		s.fail(w, err, "remove attachment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkSendable says in plain words why a draft cannot go yet ("" = it can).
func checkSendable(d store.Draft) string {
	if len(d.To)+len(d.Cc)+len(d.Bcc) == 0 {
		return "add at least one recipient"
	}
	if len(d.To)+len(d.Cc)+len(d.Bcc) > 500 {
		return "up to 500 recipients at once"
	}
	for _, list := range [][]store.Address{d.To, d.Cc, d.Bcc} {
		for _, a := range list {
			if !validEmail(a.Addr) {
				return fmt.Sprintf("%q is not an email address", a.Addr)
			}
		}
	}
	return ""
}

// queue puts a draft in the outbox: at sendAt, or now plus the undo window.
func (s *Server) queue(ctx context.Context, uid int64, d store.Draft, sendAt time.Time) (store.Draft, time.Time, error) {
	a, err := s.store.GetAccount(ctx, uid, d.AccountID)
	if err != nil {
		return d, sendAt, err
	}
	if sendAt.IsZero() {
		st, _ := s.store.GetSettings(ctx, uid)
		sendAt = time.Now().Add(time.Duration(st.UndoSendSeconds) * time.Second)
	}
	if err := s.store.QueueDraft(ctx, uid, d.ID, sendAt.UTC().Format(time.RFC3339Nano), pmail.NewMessageID(a.Email)); err != nil {
		return d, sendAt, err
	}
	if time.Until(sendAt) <= time.Second {
		s.mail.KickOutbox()
	} else if time.Until(sendAt) < 2*time.Minute {
		go func(at time.Time) {
			time.Sleep(time.Until(at) + 200*time.Millisecond)
			s.mail.KickOutbox()
		}(sendAt)
	}
	d, err = s.store.GetDraft(ctx, uid, d.ID)
	return d, sendAt, err
}

func (s *Server) handleSendDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	d, err := s.store.GetDraft(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "draft")
		return
	}
	var in struct {
		SendAt string `json:"send_at"`
	}
	if r.ContentLength != 0 {
		_ = decode(r, &in)
	}
	if msg := checkSendable(d); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	var at time.Time
	if in.SendAt != "" {
		if at, err = time.Parse(time.RFC3339, in.SendAt); err != nil || at.Before(time.Now().Add(-time.Minute)) {
			writeErr(w, http.StatusBadRequest, "pick a time in the future")
			return
		}
	}
	d, at, err = s.queue(ctx, uid, d, at)
	if err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "this message is already on its way")
			return
		}
		s.fail(w, err, "send")
		return
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": d.AccountID, "new": []int64{}})
	writeJSON(w, http.StatusOK, map[string]any{"draft": d, "undo_until": at.UTC().Format(time.RFC3339)})
}

func (s *Server) handleCancelDraft(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id := idParam(r, "id")
	if err := s.store.UnqueueDraft(ctx, uid, id); err != nil {
		if err == store.ErrConflict {
			writeErr(w, http.StatusConflict, "too late — it has already gone out")
			return
		}
		s.fail(w, err, "cancel")
		return
	}
	d, err := s.store.GetDraft(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "draft")
		return
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": d.AccountID, "new": []int64{}})
	writeJSON(w, http.StatusOK, d)
}

// handleSendDirect sends a message in one call — for another Pocket app with
// mail:send (an invoice, a book to a Kindle) or a script with an API token.
// It goes out right away (no undo window) and is filed in Sent.
func (s *Server) handleSendDirect(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		AccountID   int64           `json:"account_id"`
		From        string          `json:"from"` // pick the account by address
		To          []store.Address `json:"to"`
		Cc          []store.Address `json:"cc"`
		Bcc         []store.Address `json:"bcc"`
		Subject     string          `json:"subject"`
		BodyHTML    string          `json:"body_html"`
		BodyText    string          `json:"body_text"`
		SendAt      string          `json:"send_at"`
		Attachments []struct {
			Filename string `json:"filename"`
			MIME     string `json:"mime"`
			Data     string `json:"data"` // base64
		} `json:"attachments"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var a store.Account
	var err error
	if in.From != "" && in.AccountID == 0 {
		a, err = s.store.AccountByEmail(ctx, uid, in.From)
	} else {
		a, err = s.pickAccount(ctx, uid, in.AccountID)
	}
	if err != nil {
		writeErr(w, http.StatusConflict, "no mail account to send from (add one in Pocket Mail → Settings → Accounts)")
		return
	}
	body := sanitizeUserHTML(in.BodyHTML)
	if body == "" {
		body = "<p>" + textToHTML(in.BodyText) + "</p>"
	}
	src := linkApp(ctx)
	if src == "" {
		src = "api"
	}
	d := store.Draft{UserID: uid, AccountID: a.ID, Kind: "new", To: cleanAddrs(in.To), Cc: cleanAddrs(in.Cc), Bcc: cleanAddrs(in.Bcc),
		Subject: strings.TrimSpace(in.Subject), BodyHTML: body, Source: src}
	if msg := checkSendable(d); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	var total int64
	type file struct {
		name, mime string
		data       []byte
	}
	var files []file
	for _, f := range in.Attachments {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "attachment data must be base64")
			return
		}
		total += int64(len(data))
		if total > s.maxAttach() {
			writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("attachments can be up to %d MB together", s.cfg.MaxAttachmentMB))
			return
		}
		files = append(files, file{safeFilename(f.Filename), f.MIME, data})
	}
	if err := s.store.CreateDraft(ctx, &d); err != nil {
		s.fail(w, err, "send")
		return
	}
	for _, f := range files {
		df := store.DraftFile{UserID: uid, DraftID: d.ID, Filename: f.name, MIME: firstNonEmpty(f.mime, http.DetectContentType(f.data)), Data: f.data}
		if err := s.store.AddDraftFile(ctx, &df); err != nil {
			s.fail(w, err, "attach")
			return
		}
	}
	at := time.Now()
	if in.SendAt != "" {
		if at, err = time.Parse(time.RFC3339, in.SendAt); err != nil {
			writeErr(w, http.StatusBadRequest, "send_at is RFC 3339")
			return
		}
	}
	d, _ = s.store.GetDraft(ctx, uid, d.ID)
	if d, at, err = s.queue(ctx, uid, d, at); err != nil {
		s.fail(w, err, "send")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"draft_id": d.ID, "status": d.Status, "send_at": at.UTC().Format(time.RFC3339), "message_id": d.MessageID, "from": a.Email})
}
