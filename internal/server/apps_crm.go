package server

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- Pocket CRM: an email from a possible client becomes a deal ----
//
// crm:write makes a lead (POST /deals) that remembers the message it came
// from (source_ref "message:<id>", kept by CRM as "mail:message:<id>"), so
// pressing it twice gives the same deal. crm:read shows, in the reader, the
// deals you already have with the sender (GET /lookup?email=).

// crmDeal is the part of a Pocket CRM deal Mail shows.
type crmDeal struct {
	ID        any     `json:"id"`
	Title     string  `json:"title"`
	StageName string  `json:"stage_name"`
	Status    string  `json:"status"`
	Amount    float64 `json:"amount"`
	Currency  string  `json:"currency"`
	URL       string  `json:"url"`
}

// appSnippet is the start of a message's text on one line, at most n characters.
func appSnippet(text string, n int) string {
	t := strings.Join(strings.Fields(text), " ")
	if r := []rune(t); len(r) > n {
		t = strings.TrimSpace(string(r[:n])) + "…"
	}
	return t
}

func (s *Server) appMakeDeal(ctx context.Context, cl *clients, m store.Message) (store.AppLink, bool, error) {
	sib, err := cl.conn(ctx, appCRM, "crm:write")
	if err != nil {
		return store.AppLink{}, false, err
	}
	title := firstNonEmpty(strings.TrimSpace(m.Subject), "Email from "+firstNonEmpty(m.From.Name, m.From.Addr))
	var b strings.Builder
	if sn := appSnippet(s.appMessageText(ctx, &m), 400); sn != "" {
		b.WriteString(sn + "\n\n")
	}
	b.WriteString("From " + appFromLine(m) + ", " + appMsgTime(m).Local().Format("2 Jan 2006"))
	if bl := s.appBackLink(m); bl != "" {
		b.WriteString("\nOpen the email: " + bl)
	}
	body := map[string]any{
		"title":         title,
		"contact_name":  strings.TrimSpace(m.From.Name),
		"contact_email": strings.TrimSpace(m.From.Addr),
		"description":   b.String(),
		"source":        "email",
		"source_ref":    "message:" + strconv.FormatInt(m.ID, 10),
		"note":          "From an email",
	}
	var d crmDeal
	if err := sib.client.JSON(ctx, "POST", "/api/v1/deals", nil, body, &d); err != nil {
		return store.AppLink{}, false, err
	}
	id := appIDString(d.ID)
	// CRM answers with the same deal for a message it has seen before: keep one link.
	links, _ := s.store.AppLinksForThread(ctx, cl.u.ID, m.ThreadKey, []int64{m.ID})
	for _, l := range links {
		if l.App == "crm" && l.ItemID == id && id != "" {
			return l, true, nil
		}
	}
	return s.appLink(ctx, cl.u.ID, m, "crm", "deal", firstNonEmpty(d.Title, title), id, appURL(sib.public, "/deals/"+id)), false, nil
}

func (s *Server) handleMakeDeal(w http.ResponseWriter, r *http.Request) {
	m, ok := s.appOwnMessage(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	l, again, err := s.appMakeDeal(ctx, s.clients(ctx, actorID(ctx)), m)
	if err != nil {
		s.appErr(w, appCRM.name, err)
		return
	}
	msg := "Deal made in " + appCRM.name
	if again {
		msg = "Already a deal in " + appCRM.name
	}
	writeJSON(w, http.StatusOK, map[string]any{"link": l, "message": msg})
}

// handleCRMDeals is the deals you already have with a sender, from Pocket CRM.
func (s *Server) handleCRMDeals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	addr := strings.TrimSpace(r.URL.Query().Get("addr"))
	out := map[string]any{"deals": []crmDeal{}, "open": 0, "url": ""}
	if addr == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	sib, err := s.clients(ctx, actorID(ctx)).conn(ctx, appCRM, "crm:read")
	if err != nil {
		s.appErr(w, appCRM.name, err)
		return
	}
	var got struct {
		Deals []crmDeal `json:"deals"`
		URL   string    `json:"url"`
	}
	if err := sib.client.JSON(ctx, "GET", "/api/v1/lookup", url.Values{"email": {addr}}, nil, &got); err != nil {
		s.appErr(w, appCRM.name, err)
		return
	}
	open := 0
	deals := make([]crmDeal, 0, len(got.Deals))
	for _, d := range got.Deals {
		d.URL = appURL(sib.public, "/deals/"+appIDString(d.ID))
		if d.Status == "" || d.Status == "open" {
			open++
		}
		deals = append(deals, d)
	}
	out["deals"], out["open"], out["url"] = deals, open, firstNonEmpty(sib.public, got.URL)
	writeJSON(w, http.StatusOK, out)
}
