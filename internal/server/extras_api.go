package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- senders: images, VIP, mute ----

func (s *Server) handleListSenders(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListSenders(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "senders")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handlePutSender(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	addr, _ := url.PathUnescape(chi.URLParam(r, "addr"))
	addr = strings.ToLower(strings.TrimSpace(addr))
	if !strings.Contains(addr, "@") || len(addr) > 320 {
		writeErr(w, http.StatusBadRequest, "not an email address")
		return
	}
	var in struct {
		Name        *string `json:"name"`
		AllowImages *bool   `json:"allow_images"`
		VIP         *bool   `json:"vip"`
		Muted       *bool   `json:"muted"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	sd := s.store.GetSender(ctx, uid, addr)
	if in.Name != nil {
		sd.Name = strings.TrimSpace(*in.Name)
	}
	if in.AllowImages != nil {
		sd.AllowImages = *in.AllowImages
	}
	if in.VIP != nil {
		sd.VIP = *in.VIP
		if sd.VIP {
			sd.Muted = false
		}
	}
	if in.Muted != nil {
		sd.Muted = *in.Muted
		if sd.Muted {
			sd.VIP = false
		}
	}
	if err := s.store.PutSender(ctx, sd); err != nil {
		s.fail(w, err, "sender")
		return
	}
	writeJSON(w, http.StatusOK, s.store.GetSender(ctx, uid, addr))
}

// ---- subscriptions: newsletters and lists ----

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Subscriptions(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "subscriptions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": list})
}

// handleUnsubscribe leaves a sender's list the way the list asks for: a
// one-click request or a mail sent for you; a web page is handed back to
// open. Their mail is muted either way.
func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Addr string `json:"addr"`
	}
	if err := decode(r, &in); err != nil || in.Addr == "" {
		writeErr(w, http.StatusBadRequest, "which sender?")
		return
	}
	m, err := s.store.LatestUnsubscribe(ctx, uid, in.Addr)
	if err != nil {
		writeErr(w, http.StatusNotFound, "this sender does not say how to unsubscribe — mute them or make a rule instead")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	u, err := s.mail.Unsubscribe(tctx, m)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	sd := s.store.GetSender(ctx, uid, in.Addr)
	if sd.Name == "" {
		sd.Name = m.From.Name
	}
	sd.UnsubscribedAt, sd.Muted, sd.VIP = store.Now(), true, false
	_ = s.store.PutSender(ctx, sd)
	name := firstNonEmpty(m.From.Name, m.From.Addr)
	res := map[string]any{"method": u.Method, "url": u.URL, "done": u.Method == "one_click" || u.Method == "mailto"}
	switch u.Method {
	case "one_click":
		res["message"] = "Unsubscribed from " + name + "."
	case "mailto":
		res["message"] = "An unsubscribe request was sent to " + name + "."
	case "url":
		res["message"] = name + " wants you to confirm on their website."
	default:
		res["message"] = name + " gives no way to unsubscribe; their mail is muted."
	}
	writeJSON(w, http.StatusOK, res)
}

// handleArchiveSender archives everything from a sender that is in the inbox.
func (s *Server) handleArchiveSender(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Addr string `json:"addr"`
	}
	if err := decode(r, &in); err != nil || in.Addr == "" {
		writeErr(w, http.StatusBadRequest, "which sender?")
		return
	}
	ids, err := s.store.MessageIDsFrom(ctx, uid, in.Addr, "inbox")
	if err != nil {
		s.fail(w, err, "messages")
		return
	}
	if len(ids) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": 0, "ops": []int64{}, "message": "Nothing from them in the inbox."})
		return
	}
	ops, err := s.mail.Apply(ctx, uid, ids, pmail.Action{Kind: "archive"})
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if ops == nil {
		ops = []int64{}
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": 0, "new": []int64{}})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "count": len(ids), "ops": ops, "message": fmt.Sprintf("Archived %d messages", len(ids))})
}

// ---- templates ----

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListTemplates(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "templates")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSaveTemplate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Template
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	t := store.Template{ID: idParam(r, "id"), UserID: uid, Name: strings.TrimSpace(in.Name), Subject: strings.TrimSpace(in.Subject), BodyHTML: sanitizeUserHTML(in.BodyHTML)}
	if t.Name == "" || len(t.Name) > 120 {
		writeErr(w, http.StatusBadRequest, "give the template a name")
		return
	}
	if err := s.store.SaveTemplate(ctx, &t); err != nil {
		s.fail(w, err, "template")
		return
	}
	status := http.StatusOK
	if chi.URLParam(r, "id") == "" {
		status = http.StatusCreated
	}
	list, _ := s.store.ListTemplates(ctx, uid)
	for _, x := range list {
		if x.ID == t.ID {
			writeJSON(w, status, x)
			return
		}
	}
	writeJSON(w, status, t)
}

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteTemplate(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "template")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- rules ----

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListRules(r.Context(), actorID(r.Context()), false)
	if err != nil {
		s.fail(w, err, "rules")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func validCategory(c string) bool {
	return c == "" || c == "people" || c == "updates" || c == "newsletters" || c == "receipts"
}

func (s *Server) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Rule
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.ID, in.UserID = idParam(r, "id"), uid
	if in.ID > 0 {
		old, err := s.store.GetRule(ctx, uid, in.ID)
		if err != nil {
			s.fail(w, err, "rule")
			return
		}
		in.Hits, in.LastHitAt = old.Hits, old.LastHitAt
	}
	in.Name = strings.TrimSpace(in.Name)
	m := &in.Match
	m.From, m.To, m.Subject = strings.TrimSpace(m.From), strings.TrimSpace(m.To), strings.TrimSpace(m.Subject)
	a := &in.Actions
	switch {
	case m.From == "" && m.To == "" && m.Subject == "" && !m.HasAttachment && m.Category == "":
		writeErr(w, http.StatusBadRequest, "say which mail the rule is for (from, to, subject, attachments or a tab)")
		return
	case !validCategory(m.Category) || !validCategory(a.Category):
		writeErr(w, http.StatusBadRequest, "unknown tab")
		return
	case !a.MarkRead && !a.Star && !a.Archive && a.MoveTo == "" && a.Category == "" && len(a.SaveTo) == 0 && !a.Task && !a.Notify:
		writeErr(w, http.StatusBadRequest, "pick at least one thing the rule does")
		return
	}
	for _, app := range a.SaveTo {
		if app != "documents" && app != "files" && app != "photos" {
			writeErr(w, http.StatusBadRequest, "attachments can go to documents, files or photos")
			return
		}
	}
	if in.AccountID != nil && *in.AccountID != 0 {
		if _, err := s.store.GetAccount(ctx, uid, *in.AccountID); err != nil {
			writeErr(w, http.StatusBadRequest, "unknown account")
			return
		}
	} else {
		in.AccountID = nil
	}
	if a.MoveTo != "" && in.AccountID == nil {
		writeErr(w, http.StatusBadRequest, "moving to a folder needs the rule to be for one account")
		return
	}
	if in.Name == "" {
		in.Name = ruleName(in)
	}
	if err := s.store.SaveRule(ctx, &in); err != nil {
		s.fail(w, err, "rule")
		return
	}
	saved, err := s.store.GetRule(ctx, uid, in.ID)
	if err != nil {
		s.fail(w, err, "rule")
		return
	}
	status := http.StatusOK
	if chi.URLParam(r, "id") == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, saved)
}

// ruleName names a rule after what it matches ("From newsletter@shop.com").
func ruleName(r store.Rule) string {
	switch m := r.Match; {
	case m.From != "":
		return "From " + m.From
	case m.Subject != "":
		return "Subject has “" + m.Subject + "”"
	case m.To != "":
		return "To " + m.To
	case m.Category != "":
		return strings.ToUpper(m.Category[:1]) + m.Category[1:]
	}
	return "With attachments"
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRule(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
