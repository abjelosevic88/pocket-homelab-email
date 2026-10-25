package mail

import (
	"context"
	"errors"
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func (e *Engine) accountOf(ctx context.Context, m store.Message) (store.Account, error) {
	return e.st.GetAccount(ctx, 0, m.AccountID)
}

// EnsureBody fetches a message's text and HTML from the server when they are not here yet.
func (e *Engine) EnsureBody(ctx context.Context, m *store.Message) error {
	if m.BodyFetched || (m.TextPart == "" && m.HTMLPart == "") {
		return nil
	}
	a, err := e.accountOf(ctx, *m)
	if err != nil {
		return err
	}
	if a.Provider == "demo" {
		return nil
	}
	if m.UID <= 0 {
		return nil // still moving; the text comes with the next sync
	}
	return e.withWork(ctx, &a, m.FolderName, func(c *imapclient.Client) error {
		list := []store.Message{*m}
		e.fetchBodies(ctx, c, list)
		if list[0].BodyFetched {
			m.BodyText, m.BodyHTML, m.BodyFetched, m.Snippet = list[0].BodyText, list[0].BodyHTML, true, list[0].Snippet
			return nil
		}
		return errors.New("the mail server didn't send the text of this message")
	})
}

// fetchSection reads one part (or the whole message, sec "") of a message.
func (e *Engine) fetchSection(ctx context.Context, m store.Message, sec string, max int64) ([]byte, error) {
	a, err := e.accountOf(ctx, m)
	if err != nil {
		return nil, err
	}
	if a.Provider == "demo" {
		return nil, errors.New("this demo message has no original on a server")
	}
	if m.UID <= 0 {
		return nil, errors.New("this message is still being moved; try again in a moment")
	}
	var out []byte
	err = e.withWork(ctx, &a, m.FolderName, func(c *imapclient.Client) error {
		item := sectionOf(sec, 0)
		list, err := c.Fetch(imap.UIDSetNum(imap.UID(m.UID)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{item}}).Collect()
		if err != nil {
			return err
		}
		for _, fm := range list {
			if fm.UID == imap.UID(m.UID) {
				out = fm.FindBodySection(item)
			}
		}
		if out == nil {
			return errors.New("the mail server no longer has this message")
		}
		return nil
	})
	if err == nil && max > 0 && int64(len(out)) > max {
		return nil, fmt.Errorf("it is too big to open here (%s)", sizeText(int64(len(out))))
	}
	return out, err
}

// AttachmentData returns an attachment's bytes (from the server, or what is kept here).
func (e *Engine) AttachmentData(ctx context.Context, m store.Message, a store.Attachment) ([]byte, error) {
	if a.Data != nil {
		return a.Data, nil
	}
	if a.Size > 0 && a.Size > e.cfg.MaxMessageSize*4/3+4096 {
		return nil, fmt.Errorf("this attachment is too big to open here (%s)", sizeText(a.Size*3/4))
	}
	raw, err := e.fetchSection(ctx, m, a.Part, 0)
	if err != nil {
		return nil, err
	}
	data, err := decodeBytes(raw, a.Encoding)
	if err != nil {
		return raw, nil
	}
	return data, nil
}

// Raw returns the whole message as an .eml.
func (e *Engine) Raw(ctx context.Context, m store.Message) ([]byte, error) {
	return e.fetchSection(ctx, m, "", e.cfg.MaxMessageSize*2)
}
