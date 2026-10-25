package server

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-email/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- the Pocket apps, from a message ----
//
// Everything here acts in the person's own copy of a sibling app, through a
// Pocket Core link or a direct token (siblings.go), and records what it made
// as an app link on the message, so the reader can show "Saved to Pocket
// Documents · Open". A sibling that is not set up answers 409 with a plain
// sentence; one that fails answers 502 with what it said.

// appErr writes a sibling failure in plain words.
func (s *Server) appErr(w http.ResponseWriter, name string, err error) {
	if isNotConnected(err) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	var he *pocket.HTTPError
	if errors.As(err, &he) {
		if he.Msg != "" {
			writeErr(w, http.StatusBadGateway, name+" said: "+he.Msg)
			return
		}
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("%s answered with an error (%d)", name, he.Status))
		return
	}
	if errors.Is(err, pocket.ErrTooBig) {
		writeErr(w, http.StatusRequestEntityTooLarge, "that file is too big to attach")
		return
	}
	s.log.Warn("pocket app call failed", "app", name, "err", err)
	writeErr(w, http.StatusBadGateway, name+" did not answer: "+err.Error())
}

// appDecodeOpt decodes an optional JSON body (empty = zero value).
func appDecodeOpt(r *http.Request, v any) error {
	if err := decode(r, v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// appURL joins an app's public address and a page path ("" when the address is unknown).
func appURL(public, p string) string {
	if public == "" {
		return ""
	}
	return strings.TrimRight(public, "/") + p
}

// appBackLink is the message in Pocket Mail, for notes, tasks and events.
func (s *Server) appBackLink(m store.Message) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return s.cfg.BaseURL + "/message/" + strconv.FormatInt(m.ID, 10)
}

func appFromLine(m store.Message) string {
	if m.From.Name != "" && m.From.Addr != "" {
		return m.From.Name + " <" + m.From.Addr + ">"
	}
	return firstNonEmpty(m.From.Name, m.From.Addr)
}

func appMsgTime(m store.Message) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, m.Date); err == nil {
		return t
	}
	return time.Now()
}

// appMsgDay is the message's local date (YYYY-MM-DD).
func appMsgDay(m store.Message) string { return appMsgTime(m).Local().Format("2006-01-02") }

// appOwnMessage loads one of the caller's messages (404 for anyone else's).
func (s *Server) appOwnMessage(w http.ResponseWriter, r *http.Request) (store.Message, bool) {
	m, err := s.store.GetMessage(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "message")
		return m, false
	}
	return m, true
}

// appMessageText is the readable text of a message (fetched from the server when needed).
func (s *Server) appMessageText(ctx context.Context, m *store.Message) string {
	if !m.BodyFetched && s.mail != nil {
		_ = s.mail.EnsureBody(ctx, m)
	}
	t := strings.TrimSpace(m.BodyText)
	if t == "" && m.BodyHTML != "" {
		t = strings.TrimSpace(mail.HTMLToText(m.BodyHTML))
	}
	if t == "" {
		t = m.Snippet
	}
	return t
}

func (s *Server) appMaxFile() int64 {
	mb := s.cfg.MaxAttachmentMB
	if mb <= 0 {
		mb = 25
	}
	return int64(mb) << 20
}

// appAttachmentFiles loads the chosen attachments of a message with their bytes
// (ids empty = every file that is not an inline image; filter narrows).
func (s *Server) appAttachmentFiles(ctx context.Context, uid int64, m store.Message, ids []int64, filter func(store.Attachment) bool) ([]store.Attachment, error) {
	list, err := s.store.Attachments(ctx, uid, m.ID)
	if err != nil {
		return nil, err
	}
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []store.Attachment
	for _, a := range list {
		if len(ids) > 0 && !want[a.ID] {
			continue
		}
		if len(ids) == 0 && a.Inline && filter == nil {
			continue
		}
		if filter != nil && !filter(a) {
			continue
		}
		full, err := s.store.GetAttachment(ctx, uid, a.ID)
		if err != nil {
			return nil, err
		}
		if full.Size > s.appMaxFile() {
			return nil, fmt.Errorf("%s is too big (over %d MB)", firstNonEmpty(full.Filename, "the attachment"), s.appMaxFile()>>20)
		}
		if full.Data == nil {
			data, err := s.mail.AttachmentData(ctx, m, full)
			if err != nil {
				return nil, fmt.Errorf("could not fetch %s from the mail server: %w", firstNonEmpty(full.Filename, "the attachment"), err)
			}
			full.Data = data
		}
		if full.Filename == "" {
			full.Filename = "attachment" + appExtFor(full.MIME)
		}
		out = append(out, full)
	}
	if len(ids) > 0 && len(out) == 0 {
		return nil, store.ErrNotFound
	}
	return out, nil
}

func appExtFor(mime string) string {
	switch {
	case mime == "application/pdf":
		return ".pdf"
	case strings.HasPrefix(mime, "image/jpeg"):
		return ".jpg"
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "text/calendar"):
		return ".ics"
	case strings.HasPrefix(mime, "text/plain"):
		return ".txt"
	}
	return ""
}

// appFileName makes a subject safe as a file name.
func appFileName(subject, ext string) string {
	if strings.TrimSpace(subject) == "" {
		subject = "Email"
	}
	return safeFilename(subject) + ext
}

func (s *Server) appLink(ctx context.Context, uid int64, m store.Message, app, kind, label, itemID, itemURL string) store.AppLink {
	mid := m.ID
	l := store.AppLink{UserID: uid, MessageID: &mid, ThreadKey: m.ThreadKey, App: app, Kind: kind, Label: label, ItemID: itemID, ItemURL: itemURL}
	if err := s.store.AddAppLink(ctx, &l); err != nil {
		s.log.Warn("recording an app link failed", "err", err)
	}
	return l
}

// ---- status ----

func (s *Server) handleAppsStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(ctx)
	st, _ := s.store.GetSettings(ctx, u.ID)
	apps := map[string]any{}
	for _, a := range pocketApps {
		_, via, detail := s.connStatus(ctx, u, st, a)
		public := st.App(a.slug).PublicURL
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		if via == "" && detail == "" {
			detail = "not set up"
		}
		apps[a.slug] = map[string]any{"name": a.name, "configured": via != "", "detail": detail, "open_url": public}
	}
	photos := map[string]any{"name": "Photos (Immich)", "configured": false, "detail": "", "open_url": ""}
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	if im, err := s.immichFor(tctx, u.ID, "", ""); err == nil {
		photos["configured"], photos["open_url"] = true, im.public
	} else {
		photos["detail"] = err.Error()
	}
	cancel()
	apps["photos"] = photos
	cl, used, why := s.aiFor(ctx, u.ID, st)
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "ai": map[string]any{"ready": cl != nil, "provider": used, "detail": why}, "core": s.core.Configured()})
}

// ---- recipients ----

type appRecipient struct {
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	Source   string `json:"source"`
	PhotoURL string `json:"photo_url,omitempty"`
}

// handleRecipients suggests addresses: Pocket Contacts first (when
// connected), then the people in the mail itself.
func (s *Server) handleRecipients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := []appRecipient{}
	seen := map[string]bool{}
	add := func(rc appRecipient) {
		k := strings.ToLower(strings.TrimSpace(rc.Addr))
		if k == "" || seen[k] || len(out) >= 10 {
			return
		}
		seen[k] = true
		out = append(out, rc)
	}
	if q != "" {
		if sib, _ := optional(s.clients(ctx, uid).conn(ctx, appContacts, "contacts:read")); sib != nil {
			tctx, cancel := context.WithTimeout(ctx, 4*time.Second)
			var hits []struct {
				Name     string `json:"name"`
				Email    string `json:"email"`
				PhotoURL string `json:"photo_url"`
			}
			if err := sib.client.JSON(tctx, "GET", "/api/v1/suggest", url.Values{"q": {q}, "limit": {"10"}}, nil, &hits); err == nil {
				for _, h := range hits {
					rc := appRecipient{Name: h.Name, Addr: h.Email, Source: "contacts"}
					if h.PhotoURL != "" {
						rc.PhotoURL = appURL(sib.public, h.PhotoURL)
					}
					add(rc)
				}
			}
			cancel()
		}
	}
	if list, err := s.store.Correspondents(ctx, uid, q, 10); err == nil {
		for _, c := range list {
			add(appRecipient{Name: c.Name, Addr: c.Addr, Source: "mail"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

// ---- save to an app ----

type appSaveInput struct {
	App           string  `json:"app"` // documents | files | photos | notes
	AttachmentIDs []int64 `json:"attachment_ids"`
	Whole         bool    `json:"whole"` // the email itself (documents)
	Path          string  `json:"path"`  // files: the folder
}

func (s *Server) handleSaveToApp(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	var in appSaveInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	cl := s.clients(ctx, uid)
	var links []store.AppLink
	var err error
	var name string
	switch in.App {
	case "documents":
		name = appDocuments.name
		links, err = s.appSaveDocuments(ctx, cl, m, in.AttachmentIDs, in.Whole)
	case "files":
		name = appFiles.name
		links, err = s.appSaveFiles(ctx, cl, m, in.AttachmentIDs, in.Path)
	case "photos":
		name = "Immich"
		links, err = s.appSavePhotos(ctx, uid, m, in.AttachmentIDs)
	case "notes":
		name = appNotes.name
		var l store.AppLink
		if l, err = s.appMakeNote(ctx, cl, m); err == nil {
			links = []store.AppLink{l}
		}
	default:
		writeErr(w, http.StatusBadRequest, "save to documents, files, photos or notes")
		return
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "no such attachment")
			return
		}
		s.appErr(w, name, err)
		return
	}
	if len(links) == 0 {
		writeErr(w, http.StatusBadRequest, "nothing to save: this message has no matching attachments")
		return
	}
	msg := "Saved to " + name
	if len(links) > 1 {
		msg = fmt.Sprintf("Saved %d files to %s", len(links), name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "message": msg})
}

func (s *Server) appSaveDocuments(ctx context.Context, cl *clients, m store.Message, ids []int64, whole bool) ([]store.AppLink, error) {
	sib, err := cl.conn(ctx, appDocuments, "documents:write")
	if err != nil {
		return nil, err
	}
	type file struct {
		name, title string
		data        []byte
		attID       int64
	}
	var files []file
	if whole {
		raw, err := s.mail.Raw(ctx, m)
		if err == nil && len(raw) > 0 {
			files = append(files, file{name: appFileName(m.Subject, ".eml"), title: firstNonEmpty(m.Subject, "Email"), data: raw})
		} else {
			files = append(files, file{name: appFileName(m.Subject, ".html"), title: firstNonEmpty(m.Subject, "Email"), data: []byte(s.appEmailHTML(ctx, m))})
		}
	} else {
		atts, err := s.appAttachmentFiles(ctx, cl.u.ID, m, ids, nil)
		if err != nil {
			return nil, err
		}
		for _, a := range atts {
			title := strings.TrimSuffix(a.Filename, path.Ext(a.Filename))
			files = append(files, file{name: a.Filename, title: firstNonEmpty(title, m.Subject), data: a.Data, attID: a.ID})
		}
	}
	note := "From " + appFromLine(m) + " · " + firstNonEmpty(m.Subject, "(no subject)")
	if bl := s.appBackLink(m); bl != "" {
		note += "\n" + bl
	}
	var links []store.AppLink
	for _, f := range files {
		var doc struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		}
		fields := map[string]string{"title": f.title, "created": appMsgDay(m), "external_id": fmt.Sprintf("mail:%d", m.ID)}
		if f.attID > 0 {
			fields["external_id"] = fmt.Sprintf("mail:%d:%d", m.ID, f.attID)
		}
		if err := sib.client.Upload(ctx, "POST", "/api/v1/documents", nil, "document", f.name, f.data, fields, &doc); err != nil {
			return links, err
		}
		id := ""
		if doc.ID > 0 {
			id = strconv.FormatInt(doc.ID, 10)
			_ = sib.client.JSON(ctx, "POST", "/api/v1/documents/"+id+"/notes", nil, map[string]string{"text": note}, nil)
		}
		kind := "attachment"
		if f.attID == 0 {
			kind = "message"
		}
		links = append(links, s.appLink(ctx, cl.u.ID, m, "documents", kind, firstNonEmpty(doc.Title, f.title), id, appURL(sib.public, "/documents/"+id)))
	}
	return links, nil
}

// appEmailHTML is a stand-alone page of a message (for apps that keep it as a file).
func (s *Server) appEmailHTML(ctx context.Context, m store.Message) string {
	if !m.BodyFetched && s.mail != nil {
		_ = s.mail.EnsureBody(ctx, &m)
	}
	body := ""
	if m.BodyHTML != "" {
		body, _ = mail.Sanitize(m.BodyHTML, mail.SanitizeOptions{})
	}
	if strings.TrimSpace(body) == "" {
		body = `<pre style="white-space:pre-wrap;font:inherit">` + html.EscapeString(firstNonEmpty(m.BodyText, m.Snippet)) + "</pre>"
	}
	var to []string
	for _, a := range m.To {
		to = append(to, firstNonEmpty(a.Name, a.Addr))
	}
	return `<!doctype html><html><head><meta charset="utf-8"><title>` + html.EscapeString(m.Subject) + `</title></head><body style="font-family:sans-serif">` +
		`<h1 style="font-size:1.3em">` + html.EscapeString(firstNonEmpty(m.Subject, "(no subject)")) + "</h1>" +
		`<p style="color:#555"><b>From:</b> ` + html.EscapeString(appFromLine(m)) + "<br><b>To:</b> " + html.EscapeString(strings.Join(to, ", ")) +
		"<br><b>Date:</b> " + html.EscapeString(appMsgTime(m).Local().Format("2 Jan 2006 15:04")) + "</p><hr>" + body + "</body></html>"
}

func (s *Server) appSaveFiles(ctx context.Context, cl *clients, m store.Message, ids []int64, dir string) ([]store.AppLink, error) {
	sib, err := cl.conn(ctx, appFiles, "files:write")
	if err != nil {
		return nil, err
	}
	atts, err := s.appAttachmentFiles(ctx, cl.u.ID, m, ids, nil)
	if err != nil {
		return nil, err
	}
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" {
		dir = "Mail attachments/" + appMsgDay(m)[:7]
	}
	var links []store.AppLink
	for _, a := range atts {
		var e struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Skipped bool   `json:"skipped"`
		}
		q := url.Values{"space": {"home"}, "path": {dir}, "name": {a.Filename}, "mtime": {strconv.FormatInt(appMsgTime(m).UnixMilli(), 10)}}
		if err := sib.client.PutRaw(ctx, "/api/v1/fs/upload", q, a.Data, a.MIME, &e); err != nil {
			return links, err
		}
		p := strings.TrimPrefix(firstNonEmpty(e.Path, dir+"/"+a.Filename), "/")
		links = append(links, s.appLink(ctx, cl.u.ID, m, "files", "attachment", firstNonEmpty(e.Name, a.Filename), p, appURL(sib.public, "/f/home/"+appEscapePath(path.Dir(p)))))
	}
	return links, nil
}

// appEscapePath escapes each element of a slash path for a URL.
func appEscapePath(p string) string {
	if p == "." || p == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func appIsPhoto(a store.Attachment) bool {
	return strings.HasPrefix(a.MIME, "image/") || strings.HasPrefix(a.MIME, "video/")
}

func (s *Server) appSavePhotos(ctx context.Context, uid int64, m store.Message, ids []int64) ([]store.AppLink, error) {
	im, err := s.immichFor(ctx, uid, "", "")
	if err != nil {
		return nil, err
	}
	filter := appIsPhoto
	if len(ids) == 0 {
		filter = func(a store.Attachment) bool { return !a.Inline && appIsPhoto(a) }
	}
	atts, err := s.appAttachmentFiles(ctx, uid, m, ids, filter)
	if err != nil {
		return nil, err
	}
	at := appMsgTime(m).UTC().Format(time.RFC3339)
	var links []store.AppLink
	for _, a := range atts {
		var res struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		fields := map[string]string{"fileCreatedAt": at, "fileModifiedAt": at,
			"deviceAssetId": fmt.Sprintf("pocket-mail-%d-%d", m.ID, a.ID), "deviceId": "pocket-mail"}
		if err := im.client.Upload(ctx, "POST", "/assets", nil, "assetData", a.Filename, a.Data, fields, &res); err != nil {
			return links, err
		}
		links = append(links, s.appLink(ctx, uid, m, "photos", "attachment", a.Filename, res.ID, appURL(im.public, "/photos/"+res.ID)))
	}
	return links, nil
}

// ---- make things from a message ----

func (s *Server) appMakeNote(ctx context.Context, cl *clients, m store.Message) (store.AppLink, error) {
	sib, err := cl.conn(ctx, appNotes, "notes:write")
	if err != nil {
		return store.AppLink{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**From:** %s  \n**Date:** %s  \n", appFromLine(m), appMsgTime(m).Local().Format("2 Jan 2006 15:04"))
	if bl := s.appBackLink(m); bl != "" {
		fmt.Fprintf(&b, "[Open in Pocket Mail](%s)\n", bl)
	}
	b.WriteString("\n" + s.appMessageText(ctx, &m) + "\n")
	title := firstNonEmpty(m.Subject, "Email from "+firstNonEmpty(m.From.Name, m.From.Addr))
	var note struct {
		ID    any    `json:"id"`
		Title string `json:"title"`
	}
	if err := sib.client.JSON(ctx, "POST", "/api/v1/notes", nil, map[string]any{"title": title, "category": "Mail", "content": b.String()}, &note); err != nil {
		return store.AppLink{}, err
	}
	id := appIDString(note.ID)
	return s.appLink(ctx, cl.u.ID, m, "notes", "note", firstNonEmpty(note.Title, title), id, appURL(sib.public, "/note/"+id)), nil
}

func appIDString(v any) string {
	switch x := v.(type) {
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case string:
		return x
	case nil:
		return ""
	}
	return fmt.Sprint(v)
}

func (s *Server) handleMakeNote(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	l, err := s.appMakeNote(ctx, s.clients(ctx, actorID(ctx)), m)
	if err != nil {
		s.appErr(w, appNotes.name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": "Saved to " + appNotes.name})
}

type appTaskInput struct {
	Title string `json:"title"`
	Due   string `json:"due"` // YYYY-MM-DD (or a date-time)
	Notes string `json:"notes"`
}

func (s *Server) appMakeTask(ctx context.Context, cl *clients, m store.Message, in appTaskInput) (store.AppLink, error) {
	sib, err := cl.conn(ctx, appTasks, "tasks:write")
	if err != nil {
		return store.AppLink{}, err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = firstNonEmpty(strings.TrimSpace(m.Subject), "Reply to "+firstNonEmpty(m.From.Name, m.From.Addr))
	}
	from := "From " + appFromLine(m) + ", " + appMsgTime(m).Local().Format("2 Jan 2006")
	bl := s.appBackLink(m)
	if bl != "" {
		from = "[" + from + "](" + bl + ")"
	}
	notes := strings.TrimSpace(in.Notes)
	if notes == "" {
		notes = from
	} else {
		notes += "\n\n" + from
	}
	body := map[string]any{"title": title, "notes": notes}
	if bl != "" {
		body["url"] = bl
	}
	if in.Due != "" {
		body["due"] = in.Due
	}
	var t struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := sib.client.JSON(ctx, "POST", "/api/v1/tasks", nil, body, &t); err != nil {
		return store.AppLink{}, err
	}
	id := strconv.FormatInt(t.ID, 10)
	return s.appLink(ctx, cl.u.ID, m, "tasks", "task", firstNonEmpty(t.Title, title), id, appURL(sib.public, "/inbox?task="+id)), nil
}

func (s *Server) handleMakeTask(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	var in appTaskInput
	if err := appDecodeOpt(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	l, err := s.appMakeTask(ctx, s.clients(ctx, actorID(ctx)), m, in)
	if err != nil {
		s.appErr(w, appTasks.name, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": "Task added to " + appTasks.name})
}

type appEventInput struct {
	UseInvite bool   `json:"use_invite"`
	Title     string `json:"title"`
	Start     string `json:"start"`
	End       string `json:"end"`
	AllDay    bool   `json:"all_day"`
	Location  string `json:"location"`
}

func appIsCalendarPart(a store.Attachment) bool {
	return strings.HasPrefix(strings.ToLower(a.MIME), "text/calendar") || strings.HasSuffix(strings.ToLower(a.Filename), ".ics")
}

// appInviteOf reads the invitation of a message (nil when there is none).
func (s *Server) appInviteOf(ctx context.Context, uid int64, m store.Message) *mail.Invite {
	atts, err := s.appAttachmentFiles(ctx, uid, m, nil, appIsCalendarPart)
	if err != nil {
		return nil
	}
	for _, a := range atts {
		if inv, err := mail.ParseInvite(a.Data); err == nil && inv != nil {
			return inv
		}
	}
	return nil
}

func (s *Server) handleMakeEvent(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	var in appEventInput
	if err := appDecodeOpt(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	if in.UseInvite || in.Start == "" {
		if inv := s.appInviteOf(ctx, uid, m); inv != nil {
			in.Title = firstNonEmpty(in.Title, inv.Summary)
			in.Start, in.End, in.AllDay = inv.Start, inv.End, inv.AllDay
			in.Location = firstNonEmpty(in.Location, inv.Location)
		} else if in.Start == "" {
			writeErr(w, http.StatusBadRequest, "this message has no invitation; give the event a start")
			return
		}
	}
	sib, err := s.clients(ctx, uid).conn(ctx, appCalendar, "calendar:write")
	if err != nil {
		s.appErr(w, appCalendar.name, err)
		return
	}
	title := firstNonEmpty(strings.TrimSpace(in.Title), m.Subject, "Event")
	desc := "From " + appFromLine(m)
	bl := s.appBackLink(m)
	if bl != "" {
		desc += "\n" + bl
	}
	start, end := in.Start, in.End
	if in.AllDay {
		if len(start) >= 10 {
			start = start[:10]
		}
		if len(end) >= 10 {
			end = end[:10]
		}
	}
	body := map[string]any{"title": title, "description": desc, "location": in.Location, "all_day": in.AllDay, "start": start}
	if end != "" {
		body["end"] = end
	}
	if bl != "" {
		body["url"] = bl
	}
	var ev struct {
		EventID int64  `json:"event_id"`
		ID      any    `json:"id"`
		Title   string `json:"title"`
	}
	if err := sib.client.JSON(ctx, "POST", "/api/v1/events", nil, body, &ev); err != nil {
		s.appErr(w, appCalendar.name, err)
		return
	}
	id := appIDString(ev.ID)
	if ev.EventID > 0 {
		id = strconv.FormatInt(ev.EventID, 10)
	}
	l := s.appLink(ctx, uid, m, "calendar", "event", firstNonEmpty(ev.Title, title), id, appURL(sib.public, "/event/"+id))
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": "Added to " + appCalendar.name})
}

func (s *Server) handleMakeContact(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	var in struct {
		Addr string `json:"addr"`
		Name string `json:"name"`
	}
	if err := appDecodeOpt(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	addr := firstNonEmpty(strings.TrimSpace(in.Addr), m.From.Addr)
	name := strings.TrimSpace(in.Name)
	if name == "" {
		for _, a := range append(append([]store.Address{m.From}, m.To...), m.Cc...) {
			if strings.EqualFold(a.Addr, addr) && a.Name != "" {
				name = a.Name
				break
			}
		}
	}
	if addr == "" {
		writeErr(w, http.StatusBadRequest, "no address to add")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	sib, err := s.clients(ctx, uid).conn(ctx, appContacts, "contacts:write")
	if err != nil {
		s.appErr(w, appContacts.name, err)
		return
	}
	card := map[string]any{"kind": "individual", "fn": firstNonEmpty(name, addr), "emails": []map[string]string{{"label": "other", "value": addr}}, "note": "Added from Pocket Mail"}
	if given, family, ok := strings.Cut(name, " "); ok {
		card["given"], card["family"] = given, family
	} else if name != "" {
		card["given"] = name
	}
	var out map[string]any
	if err := sib.client.JSON(ctx, "POST", "/api/v1/contacts", nil, map[string]any{"card": card}, &out); err != nil {
		s.appErr(w, appContacts.name, err)
		return
	}
	id := appIDString(out["id"])
	if c, ok := out["contact"].(map[string]any); ok && id == "" {
		id = appIDString(c["id"])
	}
	l := s.appLink(ctx, uid, m, "contacts", "contact", firstNonEmpty(name, addr), id, appURL(sib.public, "/contacts/"+id))
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": "Added to " + appContacts.name})
}

// appMinorUnits turns an amount into the currency's smallest unit.
func appMinorUnits(amount float64, currency string) int64 {
	switch strings.ToUpper(currency) {
	case "JPY", "KRW", "HUF", "ISK", "CLP", "VND":
		return int64(math.Round(amount))
	}
	return int64(math.Round(amount * 100))
}

type appMoneyAccount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
}

func (s *Server) handleMakeExpense(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	var in struct {
		Amount      float64 `json:"amount"`
		Currency    string  `json:"currency"`
		Date        string  `json:"date"`
		Description string  `json:"description"`
		AccountID   int64   `json:"account_id"`
		CategoryID  int64   `json:"category_id"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.Amount <= 0 {
		writeErr(w, http.StatusBadRequest, "the amount must be more than zero")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	sib, err := s.clients(ctx, uid).conn(ctx, appMoney, "transactions:write", "accounts:read")
	if err != nil {
		s.appErr(w, appMoney.name, err)
		return
	}
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.AccountID == 0 {
		var accts []appMoneyAccount
		if err := sib.client.JSON(ctx, "GET", "/api/v1/accounts", nil, nil, &accts); err != nil {
			s.appErr(w, appMoney.name, err)
			return
		}
		for _, a := range accts {
			if cur == "" || strings.EqualFold(a.Currency, cur) {
				in.AccountID = a.ID
				break
			}
		}
		if in.AccountID == 0 && len(accts) > 0 {
			in.AccountID = accts[0].ID
		}
		if in.AccountID == 0 {
			writeErr(w, http.StatusConflict, "Pocket Money has no account to book this on")
			return
		}
	}
	date := firstNonEmpty(in.Date, appMsgDay(m))
	if len(date) > 10 {
		date = date[:10]
	}
	desc := firstNonEmpty(strings.TrimSpace(in.Description), m.From.Name, m.Subject)
	notes := firstNonEmpty(m.Subject, "Receipt") + " — " + appFromLine(m)
	if bl := s.appBackLink(m); bl != "" {
		notes += "\n" + bl
	}
	body := map[string]any{"kind": "expense", "date": date, "account_id": in.AccountID, "amount": appMinorUnits(in.Amount, cur), "description": desc,
		"payee": firstNonEmpty(m.From.Name, m.From.Addr), "notes": notes, "external_id": fmt.Sprintf("msg-%d", m.ID)}
	if cur != "" {
		body["currency"] = cur
	}
	if in.CategoryID > 0 {
		body["category_id"] = in.CategoryID
	}
	var tx struct {
		ID int64 `json:"id"`
	}
	if err := sib.client.JSON(ctx, "POST", "/api/v1/transactions", nil, body, &tx); err != nil {
		s.appErr(w, appMoney.name, err)
		return
	}
	id := strconv.FormatInt(tx.ID, 10)
	label := strings.TrimSpace(fmt.Sprintf("%s · %.2f %s", desc, in.Amount, cur))
	l := s.appLink(ctx, uid, m, "money", "expense", label, id, appURL(sib.public, "/transactions/"+id))
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": "Expense recorded in " + appMoney.name})
}

func (s *Server) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAppLink(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "link")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- looking things up in the apps ----

// handleContactCard is who wrote, from Pocket Contacts. The photo URL points
// at Pocket Contacts itself (same site, the browser's own session there).
func (s *Server) handleContactCard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	addr := strings.TrimSpace(r.URL.Query().Get("addr"))
	card := map[string]any{"found": false, "name": "", "org": "", "title": "", "phone": "", "photo_url": "", "url": ""}
	if addr == "" {
		writeJSON(w, http.StatusOK, card)
		return
	}
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appContacts, "contacts:read")
	if err != nil {
		s.appErr(w, appContacts.name, err)
		return
	}
	var out struct {
		Contacts []struct {
			ID       int64    `json:"id"`
			FN       string   `json:"fn"`
			Org      string   `json:"org"`
			Title    string   `json:"title"`
			Phones   []string `json:"phones"`
			PhotoURL string   `json:"photo_url"`
		} `json:"contacts"`
	}
	if err := sib.client.JSON(ctx, "GET", "/api/v1/lookup", url.Values{"email": {addr}}, nil, &out); err != nil {
		s.appErr(w, appContacts.name, err)
		return
	}
	if len(out.Contacts) > 0 {
		c := out.Contacts[0]
		card["found"], card["name"], card["org"], card["title"] = true, c.FN, c.Org, c.Title
		if len(c.Phones) > 0 {
			card["phone"] = c.Phones[0]
		}
		if c.PhotoURL != "" {
			card["photo_url"] = appURL(sib.public, c.PhotoURL)
		}
		card["url"] = appURL(sib.public, "/contacts/"+strconv.FormatInt(c.ID, 10))
	}
	writeJSON(w, http.StatusOK, card)
}

// handleCalendarDay is what else is on a day (for an invitation).
func (s *Server) handleCalendarDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	day := r.URL.Query().Get("date")
	if _, err := time.Parse("2006-01-02", day); err != nil {
		writeErr(w, http.StatusBadRequest, "date is YYYY-MM-DD")
		return
	}
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appCalendar, "calendar:read")
	if err != nil {
		s.appErr(w, appCalendar.name, err)
		return
	}
	var out struct {
		Events []struct {
			Title  string `json:"title"`
			Start  string `json:"start"`
			End    string `json:"end"`
			AllDay bool   `json:"all_day"`
		} `json:"events"`
	}
	if err := sib.client.JSON(ctx, "GET", "/api/v1/events", url.Values{"from": {day}, "to": {store.AddDays(day, 1)}}, nil, &out); err != nil {
		s.appErr(w, appCalendar.name, err)
		return
	}
	events := []map[string]any{}
	for _, e := range out.Events {
		events = append(events, map[string]any{"title": e.Title, "start": e.Start, "end": e.End, "all_day": e.AllDay})
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleBrowseFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appFiles, "files:read")
	if err != nil {
		s.appErr(w, appFiles.name, err)
		return
	}
	var out struct {
		Entries []struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"is_dir"`
			Size  int64  `json:"size"`
			Mime  string `json:"mime"`
		} `json:"entries"`
	}
	if err := sib.client.JSON(ctx, "GET", "/api/v1/fs/list", url.Values{"space": {"home"}, "path": {r.URL.Query().Get("path")}}, nil, &out); err != nil {
		s.appErr(w, appFiles.name, err)
		return
	}
	entries := []map[string]any{}
	for _, e := range out.Entries {
		entries = append(entries, map[string]any{"name": e.Name, "path": e.Path, "dir": e.IsDir, "size": e.Size, "mime": e.Mime})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleSearchDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appDocuments, "documents:read")
	if err != nil {
		s.appErr(w, appDocuments.name, err)
		return
	}
	var out struct {
		Results []struct {
			ID      int64  `json:"id"`
			Title   string `json:"title"`
			Created string `json:"created"`
			Mime    string `json:"mime"`
		} `json:"results"`
	}
	if err := sib.client.JSON(ctx, "GET", "/api/v1/documents", url.Values{"q": {r.URL.Query().Get("q")}, "limit": {"25"}}, nil, &out); err != nil {
		s.appErr(w, appDocuments.name, err)
		return
	}
	docs := []map[string]any{}
	for _, d := range out.Results {
		docs = append(docs, map[string]any{"id": d.ID, "title": d.Title, "created": d.Created, "mime": d.Mime})
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": docs})
}

func (s *Server) handleMoneyAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appMoney, "accounts:read")
	if err != nil {
		s.appErr(w, appMoney.name, err)
		return
	}
	var accts []appMoneyAccount
	if err := sib.client.JSON(ctx, "GET", "/api/v1/accounts", nil, nil, &accts); err != nil {
		s.appErr(w, appMoney.name, err)
		return
	}
	if accts == nil {
		accts = []appMoneyAccount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accts})
}

// handleAttachFromApp attaches a file from Pocket Files (ref = its path) or
// Pocket Documents (ref = its id) to a draft.
func (s *Server) handleAttachFromApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	draftID := idParam(r, "id")
	if _, err := s.store.GetDraft(ctx, uid, draftID); err != nil {
		s.fail(w, err, "draft")
		return
	}
	var in struct {
		App string `json:"app"`
		Ref string `json:"ref"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Ref) == "" {
		writeErr(w, http.StatusBadRequest, "say which file (app and ref)")
		return
	}
	cl := s.clients(ctx, uid)
	var data []byte
	var mime, name string
	switch in.App {
	case "files":
		sib, err := cl.conn(ctx, appFiles, "files:read")
		if err != nil {
			s.appErr(w, appFiles.name, err)
			return
		}
		if data, mime, err = sib.client.Download(ctx, "/api/v1/fs/raw", url.Values{"space": {"home"}, "path": {in.Ref}}, s.appMaxFile()); err != nil {
			s.appErr(w, appFiles.name, err)
			return
		}
		name = path.Base(strings.TrimRight(in.Ref, "/"))
	case "documents":
		id, err := strconv.ParseInt(in.Ref, 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "a document is given by its id")
			return
		}
		sib, err := cl.conn(ctx, appDocuments, "documents:read")
		if err != nil {
			s.appErr(w, appDocuments.name, err)
			return
		}
		var doc struct {
			Title        string `json:"title"`
			OriginalName string `json:"original_name"`
			Mime         string `json:"mime"`
		}
		if err := sib.client.JSON(ctx, "GET", fmt.Sprintf("/api/v1/documents/%d", id), nil, nil, &doc); err != nil {
			s.appErr(w, appDocuments.name, err)
			return
		}
		if data, mime, err = sib.client.Download(ctx, fmt.Sprintf("/api/v1/documents/%d/download", id), url.Values{"original": {"true"}}, s.appMaxFile()); err != nil {
			s.appErr(w, appDocuments.name, err)
			return
		}
		name = firstNonEmpty(doc.OriginalName, appFileName(doc.Title, ""))
		mime = firstNonEmpty(doc.Mime, mime)
	default:
		writeErr(w, http.StatusBadRequest, "attach from files or documents")
		return
	}
	if i := strings.Index(mime, ";"); i > 0 && !strings.HasPrefix(mime, "text/") {
		mime = mime[:i]
	}
	f := store.DraftFile{UserID: uid, DraftID: draftID, Filename: firstNonEmpty(name, "file"), MIME: firstNonEmpty(mime, "application/octet-stream"), Data: data}
	if err := s.store.AddDraftFile(ctx, &f); err != nil {
		s.fail(w, err, "attach")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// ---- rules that reach other apps ----

// appQuiet reports whether now is inside the person's appQuiet hours.
func appQuiet(st store.Settings, now time.Time) bool {
	from, to, h := st.QuietFrom, st.QuietTo, now.Hour()
	switch {
	case from == to:
		return false
	case from < to:
		return h >= from && h < to
	}
	return h >= from || h < to
}

// ruleAppActions does the parts of a matching rule that need other apps
// (save attachments, make a task, notify). Failures are logged; mail keeps flowing.
func (s *Server) ruleAppActions(ctx context.Context, m store.Message, r store.Rule) {
	a := r.Actions
	if len(a.SaveTo) == 0 && !a.Task && !a.Notify {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cl := s.clients(ctx, m.UserID)
	if cl.u == nil {
		return
	}
	logErr := func(what string, err error) {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Warn("rule action failed", "rule", r.Name, "action", what, "message", m.ID, "err", err)
		}
	}
	for _, app := range a.SaveTo {
		switch app {
		case "documents":
			_, err := s.appSaveDocuments(ctx, cl, m, nil, !m.HasAttachments)
			logErr(app, err)
		case "files":
			if m.HasAttachments {
				_, err := s.appSaveFiles(ctx, cl, m, nil, "")
				logErr(app, err)
			}
		case "photos":
			if m.HasAttachments {
				_, err := s.appSavePhotos(ctx, m.UserID, m, nil)
				logErr(app, err)
			}
		}
	}
	if a.Task {
		_, err := s.appMakeTask(ctx, cl, m, appTaskInput{})
		logErr("task", err)
	}
	if a.Notify && !appQuiet(cl.st, time.Now()) {
		logErr("notify", cl.notify.Send(ctx, notify.Message{Title: "From " + firstNonEmpty(m.From.Name, m.From.Addr), Body: firstNonEmpty(m.Subject, "(no subject)"),
			Priority: "info", Tags: []string{"envelope"}, Link: s.appBackLink(m)}))
	}
}

// ---- Immich (photos) ----

// immichConn reaches a person's Immich (photos).
type immichConn struct {
	client *pocket.Client // BaseURL ends in /api; the key goes in x-api-key
	public string
	via    string
}

func appImmichAPI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", &notConnected{"the Immich address is not a web address"}
	}
	return strings.TrimSuffix(strings.TrimRight(raw, "/"), "/api") + "/api", nil
}

func appNewImmich(base, key, public, via string) *immichConn {
	c := pocket.New(base, key, 2*time.Minute)
	c.APIKeyHeader = "x-api-key"
	return &immichConn{client: c, public: firstNonEmpty(strings.TrimRight(public, "/"), strings.TrimSuffix(base, "/api")), via: via}
}

// immichFor connects to the person's Immich: directly with their own
// address and API key, or through Pocket Core's Immich service (typedKey
// non-empty = test what was typed in).
func (s *Server) immichFor(ctx context.Context, uid int64, typedURL, typedKey string) (*immichConn, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	direct := func(raw, key string) (*immichConn, error) {
		if raw == "" || key == "" {
			return nil, &notConnected{"Photos (Immich) is not set up: add your Immich address and API key in Settings → Connected apps"}
		}
		base, err := appImmichAPI(raw)
		if err != nil {
			return nil, err
		}
		return appNewImmich(base, key, "", modeDirect), nil
	}
	if typedKey != "" && !strings.Contains(typedKey, "••") {
		return direct(firstNonEmpty(typedURL, st.ImmichURL), typedKey)
	}
	switch s.connMode(st.ImmichMode, st.ImmichKey) {
	case modeOff:
		return nil, &notConnected{"Photos (Immich) is switched off in Settings → Connected apps"}
	case modeDirect:
		return direct(firstNonEmpty(typedURL, st.ImmichURL), st.ImmichKey)
	}
	if !s.core.Configured() {
		return nil, &notConnected{"Photos (Immich) through Pocket Core: there is no Pocket Core on this box; connect directly"}
	}
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	cred, err := s.core.ServiceFor(ctx, u.Email, "immich")
	if err != nil {
		return nil, &notConnected{"Photos (Immich) is not set up in Pocket Core: " + err.Error()}
	}
	if cred.URL == "" || cred.Key == "" {
		return nil, &notConnected{"add your Immich address and API key in Pocket Core"}
	}
	base, err := appImmichAPI(cred.URL)
	if err != nil {
		return nil, err
	}
	return appNewImmich(base, cred.Key, cred.PublicURL, modeCore), nil
}

func (im *immichConn) ping(ctx context.Context) (string, error) {
	var me struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := im.client.JSON(ctx, "GET", "/users/me", nil, nil, &me); err != nil {
		return "", err
	}
	how := "direct connection"
	if im.via == modeCore {
		how = "through Pocket Core"
	}
	return fmt.Sprintf("Immich, %s, signed in as %s", how, firstNonEmpty(me.Name, me.Email)), nil
}
