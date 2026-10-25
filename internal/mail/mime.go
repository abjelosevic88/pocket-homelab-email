package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// NewMessageID makes a Message-ID (without <>) for an address's domain.
func NewMessageID(fromAddr string) string {
	_, domain := splitEmail(fromAddr)
	if domain == "" {
		domain = "pocket-mail.local"
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s.%d@%s", hex.EncodeToString(b), time.Now().UnixNano()%1e9, domain)
}

func mailAddrs(list []store.Address) []*mail.Address {
	out := make([]*mail.Address, 0, len(list))
	for _, a := range list {
		if strings.TrimSpace(a.Addr) != "" {
			out = append(out, &mail.Address{Name: strings.TrimSpace(a.Name), Address: strings.TrimSpace(a.Addr)})
		}
	}
	return out
}

// htmlDocument wraps a body fragment in a minimal HTML document.
func htmlDocument(body string) string {
	if strings.Contains(strings.ToLower(body), "<html") {
		return body
	}
	return "<!DOCTYPE html>\r\n<html><head><meta charset=\"utf-8\"></head><body>" + body + "</body></html>"
}

// BuildMessage writes a draft as an RFC 5322 message (multipart/alternative
// text + HTML, attachments, In-Reply-To/References from orig).
func BuildMessage(a store.Account, d store.Draft, files []store.DraftFile, orig *store.Message) ([]byte, error) {
	if strings.TrimSpace(a.Email) == "" {
		return nil, errors.New("the account has no address")
	}
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{{Name: a.Name, Address: a.Email}})
	if to := mailAddrs(d.To); len(to) > 0 {
		h.SetAddressList("To", to)
	}
	if cc := mailAddrs(d.Cc); len(cc) > 0 {
		h.SetAddressList("Cc", cc)
	}
	h.SetSubject(d.Subject)
	id := d.MessageID
	if id == "" {
		id = NewMessageID(a.Email)
	}
	h.SetMessageID(id)
	if orig != nil && orig.MessageID != "" {
		h.SetMsgIDList("In-Reply-To", []string{orig.MessageID})
		refs := strings.Fields(orig.Refs)
		if len(refs) > 20 { // keep the root and the most recent
			refs = append(refs[:1], refs[len(refs)-19:]...)
		}
		h.SetMsgIDList("References", append(refs, orig.MessageID))
	}
	h.Set("User-Agent", "Pocket Mail")

	html := d.BodyHTML
	text := HTMLToText(html)
	var buf bytes.Buffer
	writeAlt := func(iw *mail.InlineWriter) error {
		var th mail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		w, err := iw.CreatePart(th)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, text); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		var hh mail.InlineHeader
		hh.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		w, err = iw.CreatePart(hh)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(w, htmlDocument(html)); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return iw.Close()
	}
	if len(files) == 0 {
		iw, err := mail.CreateInlineWriter(&buf, h)
		if err != nil {
			return nil, err
		}
		if err := writeAlt(iw); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}
	iw, err := mw.CreateInline()
	if err != nil {
		return nil, err
	}
	if err := writeAlt(iw); err != nil {
		return nil, err
	}
	for _, f := range files {
		var ah mail.AttachmentHeader
		mt := f.MIME
		if mt == "" {
			mt = mime.TypeByExtension(filepath.Ext(f.Filename))
		}
		if mt == "" {
			mt = "application/octet-stream"
		}
		ah.SetContentType(mt, nil)
		ah.SetFilename(f.Filename)
		w, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
		if err := w.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
