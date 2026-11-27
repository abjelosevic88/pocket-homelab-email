package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Mail (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. All three read the copy of the mail kept here, the one the lists, the
// search box and the numbers in the sidebar read. No mail server is asked,
// nothing is synced, no message text is fetched, and nothing is marked as
// read, moved or changed.

// dayRows is how much mail one day lists; the summary counts all of it.
const dayRows = 50

// widgetRows is how many unread emails the widget names.
const widgetRows = 8

// dots joins the parts of a line that are there: "Ana Novak · Hi Alice…".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// personName is someone as the lists show them: the name, else the address.
func personName(a store.Address) string {
	return firstNonEmpty(strings.TrimSpace(a.Name), strings.TrimSpace(a.Addr))
}

// subjectLine is a subject as the lists show it.
func subjectLine(m store.Message) string {
	return firstNonEmpty(strings.TrimSpace(m.Subject), "(no subject)")
}

// toWhom names who a message went to: "To Ana", "To Ana and 2 others".
func toWhom(m store.Message) string {
	if len(m.To) == 0 || personName(m.To[0]) == "" {
		return ""
	}
	switch n := len(m.To) - 1; n {
	case 0:
		return "To " + personName(m.To[0])
	case 1:
		return "To " + personName(m.To[0]) + " and 1 other"
	default:
		return fmt.Sprintf("To %s and %d others", personName(m.To[0]), n)
	}
}

// ownAddresses are the person's own mail addresses, lowercase.
func (s *Server) ownAddresses(ctx context.Context, uid int64) map[string]bool {
	mine := map[string]bool{}
	if accts, err := s.store.ListAccounts(ctx, uid); err == nil {
		for _, a := range accts {
			mine[strings.ToLower(a.Email)] = true
		}
	}
	return mine
}

// otherPerson is who a message is with, as a row of the list says it: the
// sender, or "To …" for what the person wrote themselves.
func otherPerson(m store.Message, mine map[string]bool) string {
	if m.FolderRole == "sent" || m.FolderRole == "drafts" || mine[strings.ToLower(m.From.Addr)] {
		return toWhom(m)
	}
	return personName(m.From)
}

// ---------- search ----------

// pocketSearch answers Pocket Search with what GET /messages?q= finds: the
// search box's own search (words in subjects, people and text, and its
// from:, is:unread, newer_than: … operators) over the mail kept here, in
// every account, newest first. Trash and spam only when asked for (in:trash).
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	if store.ParseSearch(q).Empty() {
		return nil, nil // only punctuation: the list would be all mail, not matches
	}
	list, _, err := s.store.ListMessages(ctx, uid, store.Query{Search: q, Limit: limit})
	if err != nil {
		return nil, err
	}
	mine := s.ownAddresses(ctx, uid)
	out := make([]core.SearchItem, 0, len(list))
	for _, m := range list {
		id := strconv.FormatInt(m.ID, 10)
		out = append(out, core.SearchItem{ID: id, Title: subjectLine(m), Snippet: dots(otherPerson(m, mine), m.Snippet), Kind: "email", Date: m.Date, URL: "/message/" + id})
	}
	return out, nil
}

// ---------- day ----------

// pocketDay answers Pocket Day: the mail that came in and the mail that was
// sent on a day, newest first, at most dayRows of it; the summary counts
// all. It is the day of GET /summary/{date} (the same folders: no drafts,
// trash or spam) with a message filed in two folders counted once. The day
// runs midnight to midnight in the caller's time zone, else this server's.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	if tz == nil {
		tz = time.Local
	}
	start, err := time.ParseInLocation("2006-01-02", date, tz)
	if err != nil {
		return nil, err
	}
	from, to := start.UTC().Format(time.RFC3339), start.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	got, received, err := s.store.DayMessages(ctx, uid, from, to, false, dayRows)
	if err != nil {
		return nil, err
	}
	wrote, sent, err := s.store.DayMessages(ctx, uid, from, to, true, dayRows)
	if err != nil {
		return nil, err
	}
	if received+sent == 0 {
		return nil, nil
	}
	type row struct {
		at   time.Time
		item core.DayItem
	}
	rows := make([]row, 0, len(got)+len(wrote))
	add := func(m store.Message, kind, sub string) {
		at := appMsgTime(m).In(tz)
		rows = append(rows, row{at, core.DayItem{ID: strconv.FormatInt(m.ID, 10), Time: at.Format("15:04"), Title: subjectLine(m), Sub: sub, Kind: kind,
			URL: "/message/" + strconv.FormatInt(m.ID, 10)}})
	}
	for _, m := range got {
		add(m, "received", personName(m.From))
	}
	for _, m := range wrote {
		add(m, "sent", toWhom(m))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.After(rows[j].at) })
	if len(rows) > dayRows {
		rows = rows[:dayRows]
	}
	day := &core.Day{Date: date}
	for _, x := range rows {
		day.Items = append(day.Items, x.item)
	}
	var parts []string
	if received > 0 {
		parts = append(parts, strconv.Itoa(received)+" received")
	}
	if sent > 0 {
		parts = append(parts, strconv.Itoa(sent)+" sent")
	}
	day.Summary = strings.Join(parts, ", ")
	return day, nil
}

// ---------- widget ----------

// pocketWidget answers Pocket Hub: the number next to Inbox in the sidebar
// (unread there, in every account, without what is snoozed), the unread
// numbers of the inbox tabs, and the unread rows on top of the inbox as its
// list shows them: pinned first, then the newest, a conversation as one row
// when the person reads mail that way. Someone without a mail account gets
// nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	accts, err := s.store.ListAccounts(ctx, uid)
	if err != nil {
		return nil, err
	}
	if len(accts) == 0 {
		return nil, nil
	}
	counts, err := s.store.BoxCounts(ctx, uid)
	if err != nil {
		return nil, err
	}
	w := &core.WidgetData{URL: "/m/inbox", Line: "No unread mail"}
	if n := counts["inbox"]; n > 0 {
		w.Line = strconv.Itoa(n) + " unread"
	}
	for _, tab := range []struct{ key, label string }{{"people", "People"}, {"updates", "Updates"}, {"newsletters", "Newsletters"}, {"receipts", "Receipts"}} {
		w.Metrics = append(w.Metrics, core.Metric{Label: tab.label, Value: strconv.Itoa(counts[tab.key])})
	}
	st, _ := s.store.GetSettings(ctx, uid)
	list, _, err := s.store.ListMessages(ctx, uid, store.Query{Box: "inbox", Unread: true, Conversations: st.Conversations, Limit: widgetRows})
	if err != nil {
		return nil, err
	}
	for _, m := range list {
		id := strconv.FormatInt(m.ID, 10)
		detail := personName(m.From)
		if m.ThreadUnread > 1 {
			detail = dots(detail, strconv.Itoa(m.ThreadUnread)+" unread")
		}
		w.Items = append(w.Items, core.WidgetItem{ID: id, Title: subjectLine(m), Detail: detail, URL: "/message/" + id})
	}
	return w, nil
}
