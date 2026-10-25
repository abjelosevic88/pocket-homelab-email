package mail

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ParseUnsubscribe reads a List-Unsubscribe header.
func ParseUnsubscribe(header string, oneClick bool) Unsubscribe {
	u := Unsubscribe{Method: "none"}
	for _, m := range strings.Split(header, ",") {
		v := strings.Trim(strings.TrimSpace(m), "<>")
		low := strings.ToLower(v)
		switch {
		case strings.HasPrefix(low, "mailto:") && u.Mailto == "":
			u.Mailto = v[7:]
		case (strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://")) && u.URL == "":
			u.URL = v
		}
	}
	switch {
	case oneClick && strings.HasPrefix(strings.ToLower(u.URL), "https://"):
		u.Method = "one_click"
	case u.Mailto != "":
		u.Method = "mailto"
	case u.URL != "":
		u.Method = "url"
	}
	return u
}

var unsubHTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 {
		return errors.New("too many redirects")
	}
	return nil
}}

// Unsubscribe leaves the mailing list a message came from: an RFC 8058
// one-click POST, or a mail to the list's unsubscribe address. For a plain
// web link it does nothing and returns it (Method "url") for the browser.
func (e *Engine) Unsubscribe(ctx context.Context, m store.Message) (Unsubscribe, error) {
	u := ParseUnsubscribe(m.ListUnsubscribe, m.UnsubscribePost)
	if u.Method == "one_click" {
		if err := e.oneClick(ctx, u.URL); err == nil {
			return u, nil
		} else if u.Mailto == "" {
			// the one-click address failed: the page itself may still work
			return Unsubscribe{Method: "url", URL: u.URL}, nil
		}
		u.Method = "mailto"
	}
	switch u.Method {
	case "mailto":
		if err := e.mailUnsubscribe(ctx, m, u.Mailto); err != nil {
			if u.URL != "" {
				return Unsubscribe{Method: "url", URL: u.URL, Mailto: u.Mailto}, nil
			}
			return u, err
		}
		return u, nil
	case "url":
		return u, nil
	}
	return u, errors.New("this sender doesn't say how to unsubscribe")
}

func (e *Engine) oneClick(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if e.cfg.UserAgent != "" {
		req.Header.Set("User-Agent", e.cfg.UserAgent)
	}
	res, err := unsubHTTP.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("the list answered %s", res.Status)
	}
	return nil
}

// mailUnsubscribe queues the unsubscribe mail from the account the message came to.
func (e *Engine) mailUnsubscribe(ctx context.Context, m store.Message, mailto string) error {
	addr, query, _ := strings.Cut(mailto, "?")
	addr, _ = url.PathUnescape(addr)
	q, _ := url.ParseQuery(query)
	subject := q.Get("subject")
	if subject == "" {
		subject = "unsubscribe"
	}
	body := q.Get("body")
	if body == "" {
		body = "unsubscribe"
	}
	a, err := e.st.GetAccount(ctx, m.UserID, m.AccountID)
	if err != nil {
		return err
	}
	d := &store.Draft{UserID: m.UserID, AccountID: a.ID, Kind: "new", To: []store.Address{{Addr: addr}}, Subject: subject,
		BodyHTML: "<p>" + htmlEscape(body) + "</p>", Status: "queued", SendAt: store.Now(), MessageID: NewMessageID(a.Email), Source: "unsubscribe"}
	if err := e.st.CreateDraft(ctx, d); err != nil {
		return err
	}
	e.KickOutbox()
	return nil
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
