package mail

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-smtp"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

const maxSendAttempts = 5

// outboxLoop sends what is due, every few seconds or when poked.
func (e *Engine) outboxLoop(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.rt.outbox:
		}
		e.sendDue(ctx)
	}
}

// sendDue sends every draft whose time has come.
func (e *Engine) sendDue(ctx context.Context) {
	list, err := e.st.ClaimDueDrafts(ctx, time.Now())
	if err != nil {
		return
	}
	for _, d := range list {
		err := e.sendDraft(ctx, d)
		switch {
		case err == nil:
			_ = e.st.FinishDraft(ctx, d.ID, "", "")
			if cur, gerr := e.st.GetDraft(ctx, 0, d.ID); gerr == nil {
				d = cur
			}
			if e.cfg.OnSent != nil {
				e.cfg.OnSent(d, nil)
			}
		case transient(err) && d.Attempts < maxSendAttempts:
			retry := time.Now().Add(time.Duration(d.Attempts) * time.Minute).UTC().Format(time.RFC3339Nano)
			_ = e.st.FinishDraft(ctx, d.ID, err.Error(), retry)
		default:
			_ = e.st.FinishDraft(ctx, d.ID, err.Error(), "")
			d.Status, d.Error = "failed", err.Error()
			if e.cfg.OnSent != nil {
				e.cfg.OnSent(d, err)
			}
		}
	}
}

// transient errors are worth trying again (the network, a busy server).
func transient(err error) bool {
	if isAuthError(err) {
		return false
	}
	var se *smtp.SMTPError
	if errors.As(err, &se) {
		return se.Code >= 400 && se.Code < 500
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "can't reach") || strings.Contains(msg, "connection") || strings.Contains(msg, "EOF") || strings.Contains(msg, "timeout")
}

func recipients(d store.Draft) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range [][]store.Address{d.To, d.Cc, d.Bcc} {
		for _, a := range list {
			k := strings.ToLower(strings.TrimSpace(a.Addr))
			if k != "" && !seen[k] {
				seen[k] = true
				out = append(out, strings.TrimSpace(a.Addr))
			}
		}
	}
	return out
}

// sendDraft sends one draft and files the copy.
func (e *Engine) sendDraft(ctx context.Context, d store.Draft) error {
	a, err := e.st.GetAccount(ctx, 0, d.AccountID)
	if err != nil {
		return errors.New("the account this was written from is gone")
	}
	rcpts := recipients(d)
	if len(rcpts) == 0 {
		return errors.New("there is nobody to send it to")
	}
	files, err := e.st.DraftFilesData(ctx, d.ID)
	if err != nil {
		return err
	}
	var orig *store.Message
	if d.ReplyToID != nil {
		if m, err := e.st.GetMessage(ctx, 0, *d.ReplyToID); err == nil {
			orig = &m
		}
	}
	if d.MessageID == "" {
		d.MessageID = NewMessageID(a.Email)
	}
	raw, err := BuildMessage(a, d, files, orig)
	if err != nil {
		return err
	}
	if a.Provider == "demo" {
		e.fileLocally(ctx, a, d, files, orig)
		return nil
	}
	sc, err := e.dialSMTP(ctx, &a, "")
	if err != nil {
		return err
	}
	if err := sc.SendMail(a.Email, rcpts, bytes.NewReader(raw)); err != nil {
		sc.Close()
		return sendError(err)
	}
	_ = sc.Quit()

	if !a.SavesSent {
		if sent, create, err := e.ensureRoleFolder(ctx, a, "sent"); err == nil {
			aerr := e.withWork(ctx, &a, "", func(c *imapclient.Client) error {
				if create {
					if err := c.Create(sent.Name, nil).Wait(); err != nil && !alreadyExists(err) {
						return err
					}
				}
				cmd := c.Append(sent.Name, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}, Time: time.Now()})
				if _, err := cmd.Write(raw); err != nil {
					return err
				}
				if err := cmd.Close(); err != nil {
					return err
				}
				_, err := cmd.Wait()
				return err
			})
			if aerr != nil {
				e.log.Warn("sent mail could not be filed in Sent", "account", a.ID, "err", aerr)
			}
		}
	}
	if orig != nil {
		flag := `\Answered`
		set := store.Flags{Answered: boolp(true)}
		if d.Kind == "forward" {
			flag, set = `$Forwarded`, store.Flags{Forwarded: boolp(true)}
		}
		_ = e.st.SetFlags(ctx, orig.ID, set)
		b := fmt.Sprintf(`{"add":[%q]}`, flag)
		mid := orig.ID
		_ = e.st.AddOp(ctx, &store.PendingOp{UserID: orig.UserID, AccountID: orig.AccountID, Op: "flags", Folder: orig.FolderName, UID: orig.UID, MessageID: &mid, Args: []byte(b)})
		poke(e.rt.ops)
	}
	e.Kick(a.ID) // pull the copy into Sent
	return nil
}

func sendError(err error) error {
	var se *smtp.SMTPError
	if errors.As(err, &se) {
		switch {
		case se.Code == 550 || se.Code == 553 || se.Code == 551:
			return fmt.Errorf("the mail server refused a recipient: %s", strings.TrimSpace(se.Message))
		case se.Code == 552:
			return errors.New("the message is too big for the mail server")
		case se.Code == 554:
			return fmt.Errorf("the mail server refused the message: %s", strings.TrimSpace(se.Message))
		}
		return se
	}
	return err
}

// fileLocally puts a sent message straight into Sent (demo accounts).
func (e *Engine) fileLocally(ctx context.Context, a store.Account, d store.Draft, files []store.DraftFile, orig *store.Message) {
	sent, _, err := e.ensureRoleFolder(ctx, a, "sent")
	if err != nil {
		return
	}
	text := HTMLToText(d.BodyHTML)
	m := store.Message{UserID: a.UserID, AccountID: a.ID, FolderID: sent.ID, UID: e.st.MaxUID(ctx, sent.ID) + 1, MessageID: d.MessageID,
		Subject: d.Subject, From: store.Address{Name: a.Name, Addr: a.Email}, To: d.To, Cc: d.Cc, Bcc: d.Bcc, Date: store.Now(),
		Seen: true, BodyText: text, BodyHTML: d.BodyHTML, BodyFetched: true, Snippet: Snippet(text), Category: "people"}
	if orig != nil {
		m.InReplyTo = orig.MessageID
		m.Refs = strings.TrimSpace(orig.Refs + " " + orig.MessageID)
		m.ThreadKey = orig.ThreadKey
		set := store.Flags{Answered: boolp(true)}
		if d.Kind == "forward" {
			set = store.Flags{Forwarded: boolp(true)}
		}
		_ = e.st.SetFlags(ctx, orig.ID, set)
	}
	var atts []store.Attachment
	for i, f := range files {
		atts = append(atts, store.Attachment{Part: fmt.Sprint(i + 2), Filename: f.Filename, MIME: f.MIME, Size: f.Size, Data: f.Data})
	}
	if err := e.st.InsertMessage(ctx, &m, atts); err == nil {
		_ = e.st.RefreshFolderCounts(ctx, sent.ID)
		e.changed(a.UserID, a.ID, nil)
	}
}
