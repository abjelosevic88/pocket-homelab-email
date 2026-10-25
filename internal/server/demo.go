package server

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- demo data ----
//
// DEMO_DATA=true fills the owner's account with two mail accounts (provider
// "demo": nothing ever reaches a network) and four months of believable
// mail: conversations with friends and colleagues, newsletters that can be
// unsubscribed, receipts with PDFs, shipping and service updates, photos, a
// meeting invitation, a scheduled message and a draft. Same seed, same mail.

type demoPerson struct{ name, addr string }

var demoFriends = []demoPerson{
	{"Ana Kovač", "ana.kovac@example.org"}, {"Marko Petrović", "marko.p@example.net"}, {"Lena Horvat", "lena.horvat@example.com"},
	{"Jonas Berg", "jonas@example.de"}, {"Sara Ilić", "sara.ilic@example.org"}, {"Mom", "mama@example.net"},
	{"David Chen", "dchen@example.com"}, {"Nina Babić", "nina.b@example.org"},
}

var demoColleagues = []demoPerson{
	{"Iva Marić", "iva@studio.example"}, {"Tom Fischer", "tom@studio.example"}, {"Ema Novak", "ema.novak@client.example"},
	{"Luka Jurić", "luka@client.example"}, {"Priya Raman", "priya@agency.example"},
}

// a conversation: subject and turns ("them" / "me" with the text)
type demoTurn struct {
	me   bool
	text string
}

type demoThread struct {
	who     int // index into the account's people
	subject string
	turns   []demoTurn
	star    bool
}

var personalThreads = []demoThread{
	{0, "Dinner on Saturday?", []demoTurn{{false, "Hi!\n\nWe finally fixed the terrace, so we'd love to have you over for dinner on Saturday. Around 7? Bring nothing but yourselves.\n\nAna"}, {true, "That sounds lovely. We'll be there at 7 — I'll bring a dessert anyway, you can't stop me.\n\nSee you Saturday!"}, {false, "Ha, deal. Marko is making his famous peppers, so come hungry."}}, true},
	{1, "Hiking trip — Velebit in October", []demoTurn{{false, "Hey,\n\nI booked the hut for the 18th–20th. It's €35 a night per person, breakfast included. Are you still in? I need a yes by Friday.\n\nMarko"}, {true, "Yes, count me in! Should I bring the small stove?"}, {false, "Perfect. Bring the stove and a warm jacket, it gets cold up there at night."}, {true, "Packed already. Can't wait."}}, false},
	{2, "Photos from the lake", []demoTurn{{false, "Here are the photos from last weekend. The one with the swans came out really well!\n\nLena"}}, true},
	{3, "Your book", []demoTurn{{false, "Hi,\n\nI finished the book you lent me — thank you, I loved the last chapter. When can I bring it back?\n\nJonas"}, {true, "So glad you liked it! Keep it until we meet next week, no rush."}}, false},
	{4, "Birthday surprise for Nina", []demoTurn{{false, "Psst — Nina turns 30 on the 14th. A few of us are planning a surprise at the old place by the river. Are you in? Please don't reply-all :)\n\nSara"}, {true, "I'm in! Happy to help with decorations. What's the budget?"}, {false, "About €20 each. I'll make a little list and send it tomorrow."}}, false},
	{5, "Sunday lunch", []demoTurn{{false, "Are you coming on Sunday? I'm making sarma. Call me when you can.\n\nLove, Mom"}, {true, "Of course! I'll be there by one. Love you."}}, true},
	{6, "Recommendation for a dentist", []demoTurn{{false, "Do you know a good dentist in town? Mine retired and I need a check-up before the trip.\n\nDavid"}, {true, "Dr. Šimić on Ilica 42 — very gentle and always on time. Tell them I sent you."}}, false},
	{7, "Moving boxes", []demoTurn{{false, "Do you still have those moving boxes? We're finally moving on the 3rd!"}, {true, "I do, about fifteen. Come pick them up any evening this week."}, {false, "You're a star. Thursday after work?"}}, false},
	{0, "Recipe you asked for", []demoTurn{{false, "Here it is, the walnut cake:\n\n- 250 g ground walnuts\n- 200 g sugar\n- 6 eggs\n- a little rum\n\nBake at 180°C for 40 minutes. Don't open the oven!"}}, false},
	{1, "Concert tickets", []demoTurn{{false, "I got two extra tickets for the jazz night on Thursday. Want them?"}, {true, "Yes please! How much do I owe you?"}, {false, "Nothing, they were a gift. Enjoy!"}}, false},
}

var workThreads = []demoThread{
	{0, "Q4 roadmap draft", []demoTurn{{false, "Hi,\n\nI put the first draft of the Q4 roadmap in the shared folder. Could you look at the timeline for the mobile release before Thursday's meeting?\n\nThanks,\nIva"}, {true, "Looked through it — the timeline works if we move the beta two weeks later. I left comments on page 3."}, {false, "Good catch, updated. Thank you!"}}, true},
	{1, "Server costs this month", []demoTurn{{false, "Heads up: hosting went up 18% this month because of the image processing. I think we can cut it by moving thumbnails to the CDN. Thoughts?\n\nTom"}, {true, "Agreed, let's do it. Can you estimate the savings?"}}, false},
	{2, "Feedback on the new designs", []demoTurn{{false, "Dear team,\n\nThank you for the new designs. Overall we love the direction. Two points: the logo feels a little small on mobile, and could the green be slightly warmer?\n\nBest regards,\nEma Novak"}, {true, "Dear Ema,\n\nThank you — both are easy changes. We'll send an updated version by Wednesday.\n\nKind regards"}, {false, "Wonderful, looking forward to it."}}, true},
	{3, "Contract renewal", []demoTurn{{false, "Hello,\n\nAttached is the renewal for next year. Please have a look and let me know if anything should change before we sign.\n\nLuka"}}, false},
	{4, "Workshop next month", []demoTurn{{false, "Hi! We're running a design systems workshop on the 21st. Would you like to give a 20-minute talk about your component library?\n\nPriya"}, {true, "I'd be happy to. Can you send the details — audience, format, time?"}, {false, "Fantastic! About 40 designers and developers, in person, with Q&A. I'll send a calendar invite."}}, false},
	{1, "Deploy on Friday?", []demoTurn{{false, "Are we still deploying on Friday or should we wait for Monday? I'd rather not break anything before the weekend."}, {true, "Let's do Monday morning. Safer."}}, false},
}

type demoNewsletter struct {
	name, addr, title, color string
	topics                   []string
}

var demoNewsletters = []demoNewsletter{
	{"Weekend Reads", "hello@weekendreads.example", "Five long reads for a slow Sunday", "#b45309", []string{"Why we walk", "The last lighthouse keepers", "A short history of bread", "How cities learn", "The quiet joy of maps", "Letters that changed science"}},
	{"Kitchen Notes", "recipes@kitchennotes.example", "This week: autumn soups", "#15803d", []string{"Roasted pumpkin soup", "Mushroom barley stew", "Lentils with lemon", "Apple and walnut cake", "A better weeknight pasta", "Ten-minute flatbreads"}},
	{"Dev Digest", "digest@devdigest.example", "The week in software", "#4338ca", []string{"SQLite in production", "Go 1.26 release notes", "Faster CSS with layers", "Writing good error messages", "On-call without burnout", "What's new in WebAssembly"}},
	{"Trail & Summit", "news@trailsummit.example", "New routes and gear tips", "#0e7490", []string{"Three autumn ridge walks", "How to layer for cold mornings", "Hut-to-hut in the Alps", "Choosing trail shoes", "Night hiking safely", "The best viewpoints near you"}},
	{"Design Weekly", "team@designweekly.example", "Ideas worth stealing", "#be185d", []string{"Type that breathes", "Dark mode done right", "Icons with personality", "Motion that helps", "Forms people finish", "Colour for everyone"}},
}

type demoReceipt struct {
	name, addr, subject, item string
	amount                    float64
}

var demoReceipts = []demoReceipt{
	{"Nordlicht Café", "receipts@nordlicht.example", "Your receipt from Nordlicht Café", "2 × cappuccino, 1 × cheesecake", 9.40},
	{"Hostbay", "billing@hostbay.example", "Invoice INV-2026-%04d from Hostbay", "Cloud server S2, 1 month", 12.00},
	{"Bookshop Lumen", "orders@lumen.example", "Order #%d confirmed", "\"The Overstory\" (paperback)", 16.90},
	{"CityRail", "tickets@cityrail.example", "Your ticket: Zagreb → Split", "Train ticket, 2nd class", 24.50},
	{"Streamly", "billing@streamly.example", "Your Streamly receipt", "Monthly plan", 8.99},
	{"Green Grocer", "shop@greengrocer.example", "Thanks for your order", "Vegetable box, large", 27.30},
}

var demoUpdates = []struct{ name, addr, subject, text string }{
	{"ParcelPoint", "noreply@parcelpoint.example", "Your package is on its way", "Good news! Your package left our warehouse and should arrive in 2–3 days. Track it any time in the ParcelPoint app."},
	{"ParcelPoint", "noreply@parcelpoint.example", "Delivered: your package", "Your package was delivered and left at the front door at 14:12."},
	{"Forge", "notifications@forge.example", "[pocket-homelab] New issue: Sync stalls after network change", "Iva opened a new issue: \"After switching from Wi-Fi to mobile data the sync stops until the app is restarted.\""},
	{"Forge", "notifications@forge.example", "[pocket-homelab] Pull request merged", "Tom merged \"Retry IMAP IDLE after the connection drops\" into main."},
	{"First Bank", "alerts@firstbank.example", "Your monthly statement is ready", "Your statement for last month is ready to view in online banking."},
	{"City Library", "no-reply@library.example", "Reminder: 2 books due on Friday", "\"Braiding Sweetgrass\" and \"The Dispossessed\" are due on Friday. Renew online if you need more time."},
	{"Sky Air", "noreply@skyair.example", "Check-in is open for your flight to Lisbon", "Online check-in is now open for flight SA 482 on Thursday at 09:40. Check in and get your boarding pass."},
	{"Electric Co.", "service@electric.example", "Your meter reading is due", "Please submit your meter reading by the end of the month so we can bill you accurately."},
}

func demoPDF(title, line string, amount float64) []byte {
	text := fmt.Sprintf("BT /F1 18 Tf 72 720 Td (%s) Tj 0 -32 Td /F1 12 Tf (%s) Tj 0 -20 Td (Total: EUR %.2f) Tj ET", pdfEsc(title), pdfEsc(line), amount)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(text), text),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}

func pdfEsc(s string) string {
	s = strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`).Replace(s)
	return strings.Map(func(r rune) rune {
		if r > 126 {
			return '?'
		}
		return r
	}, s)
}

// demoPhoto is a small landscape: sky, a lake, hills — in the given colours.
func demoPhoto(sky, hill, water color.RGBA, seed int64) []byte {
	const w, h = 480, 320
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rnd := rand.New(rand.NewSource(seed))
	phase := rnd.Float64() * 6
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := sky
			t := float64(y) / h
			c = color.RGBA{uint8(float64(sky.R) * (1 - t*0.3)), uint8(float64(sky.G) * (1 - t*0.2)), uint8(sky.B), 255}
			ridge := 150 + 30*sinApprox(float64(x)/70+phase) + 12*sinApprox(float64(x)/23+phase*2)
			if float64(y) > ridge {
				c = hill
			}
			if y > 220 {
				k := float64(y-220) / 100
				c = color.RGBA{uint8(float64(water.R) * (1 - k*0.3)), uint8(float64(water.G) * (1 - k*0.2)), water.B, 255}
				if (x+y*3)%37 == 0 {
					c = color.RGBA{230, 240, 250, 255}
				}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func sinApprox(x float64) float64 {
	// a smooth periodic curve, good enough for hills
	for x > 3.14159 {
		x -= 6.28318
	}
	for x < -3.14159 {
		x += 6.28318
	}
	return 1.27323954*x - 0.405284735*x*abs(x)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func demoICS(uid, summary, location string, start, end time.Time, org demoPerson, attendee string) []byte {
	f := "20060102T150405Z"
	return []byte(strings.Join([]string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Pocket Mail demo//EN", "METHOD:REQUEST", "BEGIN:VEVENT",
		"UID:" + uid, "DTSTAMP:" + time.Now().UTC().Format(f), "DTSTART:" + start.UTC().Format(f), "DTEND:" + end.UTC().Format(f),
		"SUMMARY:" + summary, "LOCATION:" + location,
		"ORGANIZER;CN=" + org.name + ":mailto:" + org.addr,
		"ATTENDEE;CN=You;RSVP=TRUE:mailto:" + attendee,
		"END:VEVENT", "END:VCALENDAR", "",
	}, "\r\n"))
}

func newsletterHTML(n demoNewsletter, issue int, topics []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<div style="background:#f4f1ec;padding:24px 0;font-family:Georgia,serif"><table width="100%%" cellpadding="0" cellspacing="0"><tr><td align="center">`)
	fmt.Fprintf(&b, `<table width="560" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:10px">`)
	fmt.Fprintf(&b, `<tr><td style="background:%s;color:#ffffff;padding:28px 32px;border-radius:10px 10px 0 0"><div style="font-size:13px;letter-spacing:2px;text-transform:uppercase;opacity:.85">%s · Issue %d</div><div style="font-size:26px;font-weight:bold;margin-top:6px">%s</div></td></tr>`, n.color, html.EscapeString(n.name), issue, html.EscapeString(n.title))
	fmt.Fprintf(&b, `<tr><td style="padding:8px 32px 0"><img src="https://images.example.com/%s/%d.jpg" width="496" alt="" style="display:block;border-radius:6px;margin-top:20px"></td></tr>`, strings.ToLower(strings.ReplaceAll(n.name, " ", "-")), issue)
	for i, t := range topics {
		fmt.Fprintf(&b, `<tr><td style="padding:20px 32px 0"><div style="font-size:19px;font-weight:bold;color:#1f2937">%d. %s</div><p style="font-size:15px;line-height:1.6;color:#374151;margin:6px 0 0">A short, thoughtful take on %s — what changed, why it matters and one thing you can try this week. <a href="https://example.com/read/%d" style="color:%s">Read more →</a></p></td></tr>`, i+1, html.EscapeString(t), html.EscapeString(strings.ToLower(t)), issue*10+i, n.color)
	}
	fmt.Fprintf(&b, `<tr><td style="padding:28px 32px;font-size:12px;color:#6b7280;font-family:Arial,sans-serif">You get this because you subscribed to %s. <a href="https://example.com/unsubscribe" style="color:#6b7280">Unsubscribe</a></td></tr>`, html.EscapeString(n.name))
	b.WriteString(`</table></td></tr></table><img src="https://track.example.com/open.gif" width="1" height="1" alt=""></div>`)
	return b.String()
}

func receiptHTML(r demoReceipt, ref string, amount float64, when time.Time) string {
	return fmt.Sprintf(`<div style="font-family:Arial,sans-serif;max-width:520px;margin:0 auto;color:#111827">
<h2 style="font-weight:600;margin:0 0 4px">%s</h2><p style="color:#6b7280;margin:0 0 20px">Receipt %s · %s</p>
<table width="100%%" cellpadding="8" cellspacing="0" style="border-collapse:collapse;border-top:1px solid #e5e7eb;border-bottom:1px solid #e5e7eb">
<tr><td>%s</td><td align="right">€%.2f</td></tr><tr><td><b>Total</b></td><td align="right"><b>€%.2f</b></td></tr></table>
<p style="color:#6b7280;font-size:13px;margin-top:20px">Paid by card ending in 4417. The PDF receipt is attached. Thank you!</p></div>`,
		html.EscapeString(r.name), html.EscapeString(ref), when.Format("2 Jan 2006"), html.EscapeString(r.item), amount, amount)
}

func demoSnippet(t string) string {
	t = strings.Join(strings.Fields(t), " ")
	if r := []rune(t); len(r) > 180 {
		return string(r[:179]) + "…"
	}
	return t
}

func (s *Server) seedDemo(ctx context.Context, uid int64) error {
	have, err := s.store.ListAccounts(ctx, uid)
	if err != nil {
		return err
	}
	for _, a := range have {
		if a.Provider == "demo" {
			return nil
		}
	}
	u, err := s.store.GetUser(ctx, uid)
	if err != nil {
		return err
	}
	name := firstNonEmpty(u.Name, "Maya Novak")
	if name == "Admin" {
		name = "Maya Novak"
	}
	first := strings.ToLower(strings.Fields(name)[0])
	rnd := rand.New(rand.NewSource(42))
	now := time.Now()

	mkAccount := func(email, label, color, sig string, sort int) (store.Account, map[string]store.Folder, error) {
		a := store.Account{UserID: uid, Name: name, Email: email, Label: label, Provider: "demo", Auth: "password", Username: email,
			IMAPHost: "demo", SMTPHost: "demo", Color: color, Signature: sig, Sort: sort, SyncEnabled: true, SyncDays: 180, SavesSent: false}
		if err := s.store.CreateAccount(ctx, &a); err != nil {
			return a, nil, err
		}
		_ = s.store.SetAccountSync(ctx, a.ID, "ok", "")
		fs := map[string]store.Folder{}
		for _, f := range []struct{ name, display, role string }{
			{"INBOX", "Inbox", "inbox"}, {"Sent", "Sent", "sent"}, {"Drafts", "Drafts", "drafts"}, {"Archive", "Archive", "archive"},
			{"Spam", "Spam", "junk"}, {"Trash", "Trash", "trash"},
		} {
			x := store.Folder{UserID: uid, AccountID: a.ID, Name: f.name, Display: f.display, Role: f.role, Sync: true, Selectable: true}
			if err := s.store.UpsertFolder(ctx, &x); err != nil {
				return a, nil, err
			}
			fs[f.role] = x
		}
		return a, fs, nil
	}
	personal, pf, err := mkAccount(first+"@example.com", "Personal", "#0b6bcb", "<p>"+html.EscapeString(strings.Fields(name)[0])+"</p>", 1)
	if err != nil {
		return err
	}
	work, wf, err := mkAccount(first+"@studio.example", "Work", "#16a34a", "<p>"+html.EscapeString(name)+"<br><span style=\"color:#6b7280\">Product designer · Studio North</span></p>", 2)
	if err != nil {
		return err
	}
	for _, extra := range []struct {
		acct store.Account
		set  map[string]store.Folder
		name string
	}{{personal, pf, "Travel"}, {personal, pf, "Family"}, {work, wf, "Clients"}} {
		x := store.Folder{UserID: uid, AccountID: extra.acct.ID, Name: extra.name, Display: extra.name, Sync: true, Selectable: true}
		if err := s.store.UpsertFolder(ctx, &x); err != nil {
			return err
		}
		extra.set[strings.ToLower(extra.name)] = x
	}

	uids := map[int64]int64{}
	nextUID := func(f store.Folder) int64 { uids[f.ID]++; return uids[f.ID] }
	msgSeq := 0
	newID := func(domain string) string {
		msgSeq++
		return fmt.Sprintf("demo-%d-%d@%s", msgSeq, rnd.Intn(1e6), domain)
	}
	at := func(daysAgo float64) time.Time {
		t := now.Add(-time.Duration(daysAgo * 24 * float64(time.Hour)))
		h := 8 + rnd.Intn(12)
		t = time.Date(t.Year(), t.Month(), t.Day(), h, rnd.Intn(60), rnd.Intn(60), 0, time.Local)
		if t.After(now) {
			t = now.Add(-time.Duration(rnd.Intn(50)+5) * time.Minute)
		}
		return t
	}
	insert := func(m *store.Message, atts []store.Attachment) error {
		m.UserID = uid
		m.UID = nextUID(store.Folder{ID: m.FolderID})
		m.BodyFetched = true
		if m.Snippet == "" {
			m.Snippet = demoSnippet(firstNonEmpty(m.BodyText, stripTags(m.BodyHTML)))
		}
		return s.store.InsertMessage(ctx, m, atts)
	}
	me := func(a store.Account) store.Address { return store.Address{Name: name, Addr: a.Email} }

	// conversations
	threads := func(a store.Account, fs map[string]store.Folder, people []demoPerson, list []demoThread, domain string) error {
		for i, th := range list {
			p := people[th.who%len(people)]
			start := float64(i)*rnd.Float64()*9 + rnd.Float64()*3 + float64(i)*2
			if i < 3 {
				start = float64(i)*0.6 + rnd.Float64()*0.4
			}
			var refs []string
			old := start > 6
			t0 := at(start + 0.4*float64(len(th.turns)))
			when := t0
			for j, turn := range th.turns {
				id := newID(domain)
				if j > 0 {
					when = when.Add(time.Duration(40+rnd.Intn(420)) * time.Minute)
				}
				if when.After(now) {
					when = now.Add(-time.Duration(len(th.turns)-j) * 7 * time.Minute)
				}
				m := &store.Message{AccountID: a.ID, MessageID: id, Subject: th.subject, Date: when.UTC().Format(time.RFC3339), BodyText: turn.text, Category: "people"}
				if j > 0 {
					m.Subject = "Re: " + th.subject
					m.InReplyTo = refs[len(refs)-1]
					m.Refs = strings.Join(refs, " ")
				}
				if turn.me {
					m.FolderID, m.From, m.To, m.Seen = fs["sent"].ID, me(a), []store.Address{{Name: p.name, Addr: p.addr}}, true
				} else {
					folder := fs["inbox"]
					if old {
						folder = fs["archive"]
					}
					last := j == len(th.turns)-1
					m.FolderID, m.From, m.To = folder.ID, store.Address{Name: p.name, Addr: p.addr}, []store.Address{me(a)}
					m.Seen = old || !last || rnd.Intn(3) == 0
					m.Flagged = th.star && last
					m.Answered = !last
				}
				var atts []store.Attachment
				if th.subject == "Photos from the lake" {
					m.HasAttachments = true
					cols := [][3]color.RGBA{
						{{135, 190, 235, 255}, {74, 120, 86, 255}, {60, 110, 160, 255}},
						{{250, 196, 140, 255}, {96, 84, 120, 255}, {80, 90, 140, 255}},
						{{200, 220, 240, 255}, {52, 96, 70, 255}, {40, 90, 120, 255}},
					}
					for k, c := range cols {
						data := demoPhoto(c[0], c[1], c[2], int64(k+1))
						atts = append(atts, store.Attachment{Part: fmt.Sprint(k + 2), Filename: fmt.Sprintf("IMG_%04d.png", 2210+k), MIME: "image/png", Size: int64(len(data)), Data: data})
					}
				}
				if th.subject == "Contract renewal" {
					data := demoPDF("Service agreement 2027", "Studio North x Client Co. — renewal", 24000)
					atts = append(atts, store.Attachment{Part: "2", Filename: "Renewal-2027.pdf", MIME: "application/pdf", Size: int64(len(data)), Data: data})
					m.HasAttachments = true
				}
				if err := insert(m, atts); err != nil {
					return err
				}
				refs = append(refs, id)
			}
		}
		return nil
	}
	if err := threads(personal, pf, demoFriends, personalThreads, "example.com"); err != nil {
		return err
	}
	if err := threads(work, wf, demoColleagues, workThreads, "studio.example"); err != nil {
		return err
	}

	// newsletters: weekly, the last two unread
	for ni, n := range demoNewsletters {
		for w := 0; w < 12; w++ {
			day := float64(w)*7 + float64(ni) + rnd.Float64()
			topics := []string{n.topics[(w)%len(n.topics)], n.topics[(w+1)%len(n.topics)], n.topics[(w+2)%len(n.topics)]}
			folder := pf["inbox"]
			if w > 3 {
				folder = pf["archive"]
			}
			m := &store.Message{AccountID: personal.ID, FolderID: folder.ID, MessageID: newID(strings.SplitN(n.addr, "@", 2)[1]),
				Subject: fmt.Sprintf("%s — %s", n.title, topics[0]), From: store.Address{Name: n.name, Addr: n.addr}, To: []store.Address{me(personal)},
				Date: at(day).UTC().Format(time.RFC3339), BodyHTML: newsletterHTML(n, 120-w, topics), Category: "newsletters",
				ListID: strings.ToLower(strings.ReplaceAll(n.name, " ", "-")) + ".example", ListUnsubscribe: "<https://" + strings.SplitN(n.addr, "@", 2)[1] + "/unsubscribe?u=demo>, <mailto:unsubscribe@" + strings.SplitN(n.addr, "@", 2)[1] + ">",
				UnsubscribePost: true, Seen: w > 1 || rnd.Intn(2) == 0}
			m.BodyText = stripTags(m.BodyHTML)
			if err := insert(m, nil); err != nil {
				return err
			}
		}
	}

	// receipts with PDFs
	for i := 0; i < 18; i++ {
		r := demoReceipts[i%len(demoReceipts)]
		day := float64(i)*6.5 + rnd.Float64()*3
		amount := r.amount * (0.9 + rnd.Float64()*0.3)
		amount = float64(int(amount*100)) / 100
		subject := r.subject
		ref := fmt.Sprintf("R-%05d", 10000+rnd.Intn(89999))
		if strings.Contains(subject, "%") {
			n := 100 + i*7
			subject = fmt.Sprintf(subject, n)
			ref = fmt.Sprint(n)
		}
		when := at(day)
		folder := pf["inbox"]
		if i > 4 {
			folder = pf["archive"]
		}
		pdf := demoPDF(r.name, r.item, amount)
		m := &store.Message{AccountID: personal.ID, FolderID: folder.ID, MessageID: newID(strings.SplitN(r.addr, "@", 2)[1]), Subject: subject,
			From: store.Address{Name: r.name, Addr: r.addr}, To: []store.Address{me(personal)}, Date: when.UTC().Format(time.RFC3339),
			BodyHTML: receiptHTML(r, ref, amount, when), Category: "receipts", Seen: i > 1, HasAttachments: true}
		m.BodyText = stripTags(m.BodyHTML)
		if err := insert(m, []store.Attachment{{Part: "2", Filename: fmt.Sprintf("%s-%s.pdf", strings.ReplaceAll(r.name, " ", "-"), ref), MIME: "application/pdf", Size: int64(len(pdf)), Data: pdf}}); err != nil {
			return err
		}
	}

	// updates
	for i := 0; i < 26; i++ {
		u := demoUpdates[i%len(demoUpdates)]
		day := float64(i)*4.3 + rnd.Float64()*2
		if i < 3 {
			day = float64(i)*0.3 + 0.1
		}
		a, fs := personal, pf
		if strings.Contains(u.addr, "forge") {
			a, fs = work, wf
		}
		folder := fs["inbox"]
		if i > 6 {
			folder = fs["archive"]
		}
		m := &store.Message{AccountID: a.ID, FolderID: folder.ID, MessageID: newID(strings.SplitN(u.addr, "@", 2)[1]), Subject: u.subject,
			From: store.Address{Name: u.name, Addr: u.addr}, To: []store.Address{me(a)}, Date: at(day).UTC().Format(time.RFC3339),
			BodyText: u.text + "\n\nThis is an automated message, please do not reply.", Category: "updates", Seen: i > 2}
		if err := insert(m, nil); err != nil {
			return err
		}
	}

	// a meeting invitation
	start := time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, time.Local).AddDate(0, 0, 3)
	ics := demoICS("kickoff-demo@studio.example", "Project kickoff — mobile app", "Studio North, room 2 / video call", start, start.Add(90*time.Minute), demoColleagues[0], work.Email)
	inv := &store.Message{AccountID: work.ID, FolderID: wf["inbox"].ID, MessageID: newID("studio.example"), Subject: "Invitation: Project kickoff — mobile app",
		From: store.Address{Name: demoColleagues[0].name, Addr: demoColleagues[0].addr}, To: []store.Address{me(work)}, Date: now.Add(-50 * time.Minute).UTC().Format(time.RFC3339),
		BodyText: "Hi,\n\nLet's kick off the mobile app project together. Agenda: goals, timeline, who does what.\n\nSee you there,\nIva", Category: "people", HasInvite: true}
	if err := insert(inv, []store.Attachment{{Part: "2", Filename: "invite.ics", MIME: "text/calendar", Size: int64(len(ics)), Data: ics}}); err != nil {
		return err
	}

	// spam and trash
	for i, sp := range []struct{ name, addr, subject, text string }{
		{"Prize Center", "winner@prizes.example", "You have WON a brand new phone!!!", "Click here to claim your prize before midnight."},
		{"Crypto Riches", "invest@fastmoney.example", "Turn €100 into €10,000", "Our secret method is guaranteed."},
	} {
		m := &store.Message{AccountID: personal.ID, FolderID: pf["junk"].ID, MessageID: newID("spam.example"), Subject: sp.subject,
			From: store.Address{Name: sp.name, Addr: sp.addr}, To: []store.Address{me(personal)}, Date: at(float64(i) + 1).UTC().Format(time.RFC3339), BodyText: sp.text, Category: "updates"}
		if err := insert(m, nil); err != nil {
			return err
		}
	}
	tr := &store.Message{AccountID: personal.ID, FolderID: pf["trash"].ID, MessageID: newID("example.org"), Subject: "Old meeting notes",
		From: store.Address{Name: "Ana Kovač", Addr: "ana.kovac@example.org"}, To: []store.Address{me(personal)}, Date: at(20).UTC().Format(time.RFC3339), BodyText: "Notes from the meeting, no longer needed.", Seen: true}
	if err := insert(tr, nil); err != nil {
		return err
	}

	// one snoozed, one pinned
	if list, _, err := s.store.ListMessages(ctx, uid, store.Query{Box: "inbox", Category: "people", Limit: 8}); err == nil && len(list) > 5 {
		_ = s.store.SetSnooze(ctx, uid, []int64{list[5].ID}, now.Add(46*time.Hour).UTC().Format(time.RFC3339Nano))
		_ = s.store.SetPinned(ctx, uid, list[1].ID, true)
	}
	for _, f := range append(mapFolders(pf), mapFolders(wf)...) {
		_ = s.store.RefreshFolderCounts(ctx, f.ID)
	}

	// senders, a rule, templates
	_ = s.store.PutSender(ctx, store.Sender{UserID: uid, Addr: "mama@example.net", Name: "Mom", VIP: true})
	_ = s.store.PutSender(ctx, store.Sender{UserID: uid, Addr: "hello@weekendreads.example", Name: "Weekend Reads", AllowImages: true})
	_ = s.store.SaveRule(ctx, &store.Rule{UserID: uid, Name: "Receipts to Documents", Enabled: true,
		Match: store.RuleMatch{Category: "receipts", HasAttachment: true}, Actions: store.RuleActions{SaveTo: []string{"documents"}, MarkRead: true}})
	_ = s.store.SaveRule(ctx, &store.Rule{UserID: uid, Name: "Forge notifications", Enabled: true, AccountID: &work.ID,
		Match: store.RuleMatch{From: "notifications@forge.example"}, Actions: store.RuleActions{Category: "updates"}})
	_ = s.store.SaveTemplate(ctx, &store.Template{UserID: uid, Name: "Thanks, will reply soon", Subject: "", BodyHTML: "<p>Thank you for your message! I'm away this week and will reply as soon as I'm back.</p>"})
	_ = s.store.SaveTemplate(ctx, &store.Template{UserID: uid, Name: "Invoice attached", Subject: "Invoice for this month", BodyHTML: "<p>Hello,</p><p>Please find this month's invoice attached. Let me know if you have any questions.</p><p>Best regards</p>"})

	// a draft in progress and a message scheduled for tomorrow morning
	d := store.Draft{UserID: uid, AccountID: personal.ID, Kind: "new", To: []store.Address{{Name: "Marko Petrović", Addr: "marko.p@example.net"}},
		Subject: "Packing list for Velebit", BodyHTML: "<p>Hey Marko,</p><p>Here's what I'm bringing:</p><ul><li>Stove and gas</li><li>Two headlamps</li><li>Coffee (obviously)</li></ul><p><br></p>"}
	_ = s.store.CreateDraft(ctx, &d)
	sch := store.Draft{UserID: uid, AccountID: work.ID, Kind: "new", To: []store.Address{{Name: "Ema Novak", Addr: "ema.novak@client.example"}},
		Subject: "Updated designs", BodyHTML: "<p>Dear Ema,</p><p>As promised, here are the updated designs with the larger logo and the warmer green.</p><p>Kind regards</p>"}
	if err := s.store.CreateDraft(ctx, &sch); err == nil {
		tomorrow := time.Date(now.Year(), now.Month(), now.Day(), 8, 0, 0, 0, time.Local).AddDate(0, 0, 1)
		_ = s.store.QueueDraft(ctx, uid, sch.ID, tomorrow.UTC().Format(time.RFC3339Nano), "demo-scheduled@studio.example")
	}
	st, _ := s.store.GetSettings(ctx, uid)
	if st.DefaultAccount == 0 {
		st.DefaultAccount = personal.ID
		_ = s.store.SaveUserSettings(ctx, uid, st)
	}
	s.log.Info("demo mail added", "accounts", 2)
	return nil
}

func mapFolders(m map[string]store.Folder) []store.Folder {
	out := make([]store.Folder, 0, len(m))
	for _, f := range m {
		out = append(out, f)
	}
	return out
}

// stripTags is a rough text of demo HTML (the engine has the real one).
func stripTags(h string) string {
	var b strings.Builder
	in := false
	for _, r := range h {
		switch {
		case r == '<':
			in = true
			b.WriteByte(' ')
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}
