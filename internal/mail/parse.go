package mail

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // decode every charset into UTF-8
	"github.com/emersion/go-message/textproto"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// headerFields are fetched with every message for threading, lists and categories.
var headerFields = []string{"References", "List-Id", "List-Unsubscribe", "List-Unsubscribe-Post", "Precedence", "Auto-Submitted", "X-Autoreply", "Reply-To"}

type partInfo struct {
	section  string // "1.2", or "TEXT" for a single-part message
	encoding string
	charset  string
	size     int64
}

// encode keeps the part's section with how to decode it ("1.2|base64|utf-8").
func (p partInfo) encode() string {
	if p.section == "" {
		return ""
	}
	return p.section + "|" + strings.ToLower(p.encoding) + "|" + strings.ToLower(p.charset)
}

func decodePart(s string) partInfo {
	f := strings.Split(s, "|")
	p := partInfo{section: f[0]}
	if len(f) > 1 {
		p.encoding = f[1]
	}
	if len(f) > 2 {
		p.charset = f[2]
	}
	return p
}

// sectionOf turns "1.2" into the fetch item for that part.
func sectionOf(sec string, max int64) *imap.FetchItemBodySection {
	item := &imap.FetchItemBodySection{Peek: true}
	if sec == "TEXT" {
		item.Specifier = imap.PartSpecifierText
	} else if sec != "" {
		for _, n := range strings.Split(sec, ".") {
			v, _ := strconv.Atoi(n)
			item.Part = append(item.Part, v)
		}
	}
	if max > 0 {
		item.Partial = &imap.SectionPartial{Offset: 0, Size: max}
	}
	return item
}

func pathString(path []int) string {
	s := make([]string, len(path))
	for i, n := range path {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ".")
}

type structure struct {
	text, html  partInfo
	attachments []store.Attachment
	invite      bool
}

// analyze picks the text and HTML parts of a message and its files.
func analyze(bs imap.BodyStructure) structure {
	var out structure
	if bs == nil {
		return out
	}
	single := false
	if _, ok := bs.(*imap.BodyStructureSinglePart); ok {
		single = true
	}
	bs.Walk(func(path []int, part imap.BodyStructure) bool {
		sp, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		sec := pathString(path)
		if single {
			sec = "TEXT"
		}
		mt := sp.MediaType()
		disp := ""
		if d := sp.Disposition(); d != nil {
			disp = strings.ToLower(d.Value)
		}
		name := sp.Filename()
		info := partInfo{section: sec, encoding: sp.Encoding, charset: sp.Params["charset"], size: int64(sp.Size)}
		if disp != "attachment" && name == "" {
			switch {
			case mt == "text/plain" && out.text.section == "":
				out.text = info
				return true
			case mt == "text/html" && out.html.section == "":
				out.html = info
				return true
			}
		}
		if mt == "text/calendar" || mt == "application/ics" {
			out.invite = true
			if name == "" {
				name = "invite.ics"
			}
		}
		if mt == "message/rfc822" && name == "" {
			name = "forwarded message.eml"
			if sp.MessageRFC822 != nil && sp.MessageRFC822.Envelope != nil && sp.MessageRFC822.Envelope.Subject != "" {
				name = cleanFilename(sp.MessageRFC822.Envelope.Subject) + ".eml"
			}
		}
		if (mt == "text/plain" || mt == "text/html") && name == "" && disp != "attachment" {
			return true // a second text part (signature, quoted) — not a file
		}
		cid := strings.Trim(sp.ID, "<> ")
		inline := strings.HasPrefix(mt, "image/") && (disp == "inline" || (disp == "" && cid != ""))
		if name == "" {
			if inline {
				name = "image" + extFor(mt)
			} else {
				name = "attachment" + extFor(mt)
			}
		}
		if single && sec == "TEXT" {
			sec = "1"
		}
		out.attachments = append(out.attachments, store.Attachment{Part: sec, Filename: name, MIME: mt, Size: int64(sp.Size), Encoding: strings.ToLower(sp.Encoding), ContentID: cid, Inline: inline})
		return true
	})
	return out
}

func extFor(mt string) string {
	switch mt {
	case "image/png":
		return ".png"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/calendar":
		return ".ics"
	}
	return ""
}

var badFilename = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]+`)

func cleanFilename(s string) string {
	s = strings.TrimSpace(badFilename.ReplaceAllString(s, " "))
	if len(s) > 80 {
		s = s[:80]
	}
	if s == "" {
		s = "message"
	}
	return s
}

// headerInfo are the extra header fields of a message.
type headerInfo struct {
	refs, listID, listUnsub, precedence, autoSubmitted string
	oneClick                                           bool
	autoReply                                          bool
}

func parseHeaderFields(b []byte) headerInfo {
	var hi headerInfo
	if len(b) == 0 {
		return hi
	}
	h, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(append(b, '\r', '\n'))))
	if err != nil {
		return hi
	}
	mh := message.Header{Header: h}
	refs := msgIDs(h.Get("References"))
	hi.refs = strings.Join(refs, " ")
	hi.listID = strings.Trim(strings.TrimSpace(mh.Get("List-Id")), "<>")
	if t, err := mh.Text("List-Id"); err == nil {
		hi.listID = strings.TrimSpace(t)
	}
	hi.listUnsub = strings.TrimSpace(h.Get("List-Unsubscribe"))
	hi.oneClick = strings.Contains(strings.ToLower(h.Get("List-Unsubscribe-Post")), "one-click")
	hi.precedence = strings.ToLower(strings.TrimSpace(h.Get("Precedence")))
	hi.autoSubmitted = strings.ToLower(strings.TrimSpace(h.Get("Auto-Submitted")))
	hi.autoReply = h.Get("X-Autoreply") != ""
	return hi
}

var msgIDRe = regexp.MustCompile(`<([^<>\s]+)>`)

func msgIDs(s string) []string {
	var out []string
	for _, m := range msgIDRe.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

var (
	receiptRe = regexp.MustCompile(`(?i)\b(receipt|invoice|order|payment|paid|purchase|booking|reservation|statement|subscription renewal|your bill|račun|racun|faktura|uplatnica|porudžbin|porudzbin|narudžb|narudzb|plaćanj|placanj|potvrda|rechnung|bestellung|zahlung|quittung|commande|facture|reçu)`)
	botRe     = regexp.MustCompile(`(?i)^(no[-_.]?reply|do[-_.]?not[-_.]?reply|notifications?|notify|alerts?|mailer-daemon|postmaster|bounce[s]?|info|news(letter)?|updates?|support|hello|team|marketing|billing|accounts?|service|automated|system|admin|digest|security)([-_.+].*)?@`)
)

// categorize sorts a message into an inbox tab.
func categorize(fromAddr, subject string, hi headerInfo) string {
	receipt := receiptRe.MatchString(subject)
	from := strings.ToLower(fromAddr)
	bot := botRe.MatchString(from)
	if receipt && (bot || hi.listUnsub != "" || hi.precedence != "" || hi.autoSubmitted != "") {
		return "receipts"
	}
	if hi.listUnsub != "" || hi.listID != "" {
		if hi.precedence == "list" && !bot && hi.listUnsub == "" {
			return "people" // a discussion list
		}
		return "newsletters"
	}
	if (hi.autoSubmitted != "" && hi.autoSubmitted != "no") || hi.precedence == "bulk" || hi.precedence == "junk" || bot {
		if receipt {
			return "receipts"
		}
		return "updates"
	}
	return "people"
}

// decodeBody decodes a fetched part (transfer encoding, then charset → UTF-8).
func decodeBody(raw []byte, p partInfo, mediaType string) string {
	h := message.Header{}
	ct := mediaType
	if p.charset != "" {
		ct += "; charset=" + p.charset
	}
	h.Set("Content-Type", ct)
	if p.encoding != "" {
		h.Set("Content-Transfer-Encoding", p.encoding)
	}
	e, err := message.New(h, bytes.NewReader(raw))
	if err != nil && e == nil {
		return string(raw)
	}
	b, err := io.ReadAll(e.Body)
	if err != nil && len(b) == 0 {
		return string(raw)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// decodeBytes undoes a transfer encoding (attachments).
func decodeBytes(raw []byte, encoding string) ([]byte, error) {
	h := message.Header{}
	h.Set("Content-Type", "application/octet-stream")
	if encoding != "" {
		h.Set("Content-Transfer-Encoding", encoding)
	}
	e, err := message.New(h, bytes.NewReader(raw))
	if err != nil && e == nil {
		return nil, err
	}
	return io.ReadAll(e.Body)
}

func toAddrs(list []imap.Address) []store.Address {
	out := []store.Address{}
	for _, a := range list {
		if a.IsGroupStart() || a.IsGroupEnd() {
			continue
		}
		if addr := a.Addr(); addr != "" {
			out = append(out, store.Address{Name: strings.TrimSpace(a.Name), Addr: addr})
		}
	}
	return out
}

func sizeText(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
