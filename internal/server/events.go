package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- live updates ----
//
// Every open browser keeps one GET /api/v1/events stream (server-sent
// events). The engine tells the server when a person's mail changed, a sync
// state moved or a draft went out; the hub passes it on to that person's
// streams only. Nothing is queued for closed streams: a page that comes back
// simply reloads.

type liveEvent struct {
	uid  int64
	name string
	data any
}

type hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan liveEvent]bool
}

func newHub() *hub { return &hub{subs: map[int64]map[chan liveEvent]bool{}} }

func (h *hub) subscribe(uid int64) chan liveEvent {
	ch := make(chan liveEvent, 32)
	h.mu.Lock()
	if h.subs[uid] == nil {
		h.subs[uid] = map[chan liveEvent]bool{}
	}
	h.subs[uid][ch] = true
	h.mu.Unlock()
	return ch
}

func (h *hub) unsubscribe(uid int64, ch chan liveEvent) {
	h.mu.Lock()
	delete(h.subs[uid], ch)
	if len(h.subs[uid]) == 0 {
		delete(h.subs, uid)
	}
	h.mu.Unlock()
}

// send passes an event to a person's open streams (a full stream drops it).
func (h *hub) send(uid int64, name string, data any) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[uid] {
		select {
		case ch <- liveEvent{uid, name, data}:
		default:
		}
	}
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported")
		return
	}
	uid := actorID(r.Context())
	// the stream outlives the API's request timeout
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 5000\n\n")
	fl.Flush()
	ch := s.hub.subscribe(uid)
	defer s.hub.unsubscribe(uid, ch)
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	ctx := context.WithoutCancel(r.Context())
	_ = ctx
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case ev := <-ch:
			b, _ := json.Marshal(ev.data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, b)
			fl.Flush()
		}
	}
}

// mailChanged is the engine telling that a person's mail changed.
func (s *Server) mailChanged(uid, accountID int64, newIDs []int64) {
	if newIDs == nil {
		newIDs = []int64{}
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": accountID, "new": newIDs})
	if a, err := s.store.GetAccount(context.Background(), uid, accountID); err == nil {
		s.hub.send(uid, "sync", map[string]any{"type": "sync", "account_id": accountID, "state": a.SyncState, "error": a.SyncError})
	}
}

// mailSent is the engine telling that a draft went out (or failed for good).
func (s *Server) mailSent(d store.Draft, err error) {
	ev := map[string]any{"type": "sent", "draft_id": d.ID, "ok": err == nil, "error": ""}
	if err != nil {
		ev["error"] = err.Error()
	}
	s.hub.send(d.UserID, "sent", ev)
	if err != nil {
		return
	}
	s.saved.Add(1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	to := make([]string, 0, len(d.To))
	for _, a := range d.To {
		to = append(to, a.Addr)
	}
	s.publish(ctx, d.UserID, "mail.sent", d.Subject, map[string]any{"draft_id": d.ID, "account_id": d.AccountID, "subject": d.Subject, "to": to, "source": d.Source})
}

// mailArrived runs for each newly arrived message (after the engine applied
// the rules' own changes): the rules' app actions, the event on the bus and
// the push notification the person asked for.
func (s *Server) mailArrived(m store.Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	notified := false
	if rules, err := s.store.ListRules(ctx, m.UserID, true); err == nil {
		for _, r := range rules {
			if r.AccountID != nil && *r.AccountID != m.AccountID {
				continue
			}
			if !pmail.MatchRule(r, m) {
				continue
			}
			if r.Actions.Notify {
				notified = true
			}
			if len(r.Actions.SaveTo) > 0 || r.Actions.Task || r.Actions.Notify {
				s.ruleAppActions(ctx, m, r)
			}
		}
	}
	if m.FolderRole != "inbox" && m.FolderRole != "" {
		return
	}
	s.publish(ctx, m.UserID, "mail.received", m.Subject, map[string]any{
		"message_id": m.ID, "account_id": m.AccountID, "from": m.From.Addr, "from_name": m.From.Name,
		"subject": m.Subject, "category": m.Category, "date": m.Date, "has_attachments": m.HasAttachments,
	})
	if !notified {
		s.notifyNew(ctx, m)
	}
}

// notifyNew pushes a new message when the person wants that kind (people,
// VIPs or everything), outside quiet hours and not from a muted sender.
func (s *Server) notifyNew(ctx context.Context, m store.Message) {
	st, err := s.store.GetSettings(ctx, m.UserID)
	if err != nil || st.NotifyURL == "" || st.NotifyNew == "off" {
		return
	}
	sd := s.store.GetSender(ctx, m.UserID, m.From.Addr)
	if sd.Muted || m.Seen {
		return
	}
	switch st.NotifyNew {
	case "vip":
		if !sd.VIP {
			return
		}
	case "people":
		if m.Category != "people" && !sd.VIP {
			return
		}
	}
	if quietNow(st.QuietFrom, st.QuietTo, time.Now()) && !sd.VIP {
		return
	}
	who := m.From.Name
	if who == "" {
		who = m.From.Addr
	}
	body := m.Subject
	if body == "" {
		body = "(no subject)"
	}
	if m.Snippet != "" {
		body += "\n" + truncate(m.Snippet, 140)
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	msg := notify.Message{Title: who, Body: body, Priority: "info", Tags: []string{"envelope"}}
	if s.cfg.BaseURL != "" {
		msg.Link = fmt.Sprintf("%s/message/%d", s.cfg.BaseURL, m.ID)
	}
	if err := n.Send(ctx, msg); err != nil {
		s.log.Debug("notify new mail failed", "err", err)
	}
}

// quietNow reports whether the hour lies in the quiet hours (from == to = none).
func quietNow(from, to int, now time.Time) bool {
	if from == to {
		return false
	}
	h := now.Hour()
	if from < to {
		return h >= from && h < to
	}
	return h >= from || h < to
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}
