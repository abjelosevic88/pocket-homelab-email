package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// undoWindow is how long a change waits here before it goes to the server.
const undoWindow = 5 * time.Second

// Action is a change to messages made here.
type Action struct {
	Kind     string // read | unread | star | unstar | archive | trash | spam | notspam | inbox | delete | move
	FolderID int64  // for move
}

// opArgs is what a queued change needs to run and to be undone.
type opArgs struct {
	Add []string `json:"add,omitempty"` // flags
	Del []string `json:"del,omitempty"`
	// what the flags were (undo)
	PrevSeen    *bool `json:"prev_seen,omitempty"`
	PrevFlagged *bool `json:"prev_flagged,omitempty"`

	// move
	To         string `json:"to,omitempty"`
	ToID       int64  `json:"to_id,omitempty"`
	FromID     int64  `json:"from_id,omitempty"`
	PrevUID    int64  `json:"prev_uid,omitempty"`
	Create     bool   `json:"create,omitempty"`
	RoleOfDest string `json:"role,omitempty"`

	// permanent delete: the message, to put back on undo
	Snap *msgSnap `json:"snap,omitempty"`
}

// msgSnap is a stored message with its attachments (undo of a permanent delete).
type msgSnap struct {
	Raw  store.Message      `json:"raw"`
	Hid  hiddenFields       `json:"hid"`
	Atts []store.Attachment `json:"atts"`
	AttH []hiddenAtt        `json:"atth"`
}

type hiddenFields struct {
	UserID             int64
	UID                int64
	Refs, ListUnsub    string
	UnsubPost          bool
	TextPart, HTMLPart string
	BodyText, BodyHTML string
	BodyFetched        bool
}

type hiddenAtt struct {
	Encoding string
	Data     []byte
}

func snapOf(m store.Message, atts []store.Attachment) *msgSnap {
	s := &msgSnap{Raw: m, Atts: atts, Hid: hiddenFields{UserID: m.UserID, UID: m.UID, Refs: m.Refs, ListUnsub: m.ListUnsubscribe, UnsubPost: m.UnsubscribePost,
		TextPart: m.TextPart, HTMLPart: m.HTMLPart, BodyText: m.BodyText, BodyHTML: m.BodyHTML, BodyFetched: m.BodyFetched}}
	for _, a := range atts {
		s.AttH = append(s.AttH, hiddenAtt{Encoding: a.Encoding, Data: a.Data})
	}
	return s
}

func (s *msgSnap) restore() (store.Message, []store.Attachment) {
	m := s.Raw
	m.UserID = s.Hid.UserID
	m.UID, m.Refs, m.ListUnsubscribe, m.UnsubscribePost = s.Hid.UID, s.Hid.Refs, s.Hid.ListUnsub, s.Hid.UnsubPost
	m.TextPart, m.HTMLPart, m.BodyText, m.BodyHTML, m.BodyFetched = s.Hid.TextPart, s.Hid.HTMLPart, s.Hid.BodyText, s.Hid.BodyHTML, s.Hid.BodyFetched
	atts := make([]store.Attachment, len(s.Atts))
	for i, a := range s.Atts {
		a.ID, a.UserID = 0, m.UserID
		if i < len(s.AttH) {
			a.Encoding, a.Data = s.AttH[i].Encoding, s.AttH[i].Data
		}
		atts[i] = a
	}
	return m, atts
}

func boolp(b bool) *bool { return &b }

// Apply changes a person's messages here at once and queues the change for
// the server (after the undo window). It returns the queued change ids for Undo.
func (e *Engine) Apply(ctx context.Context, uid int64, ids []int64, act Action) ([]int64, error) {
	ops, err := e.apply(ctx, uid, ids, act, undoWindow)
	if err == nil {
		go func() {
			time.Sleep(undoWindow + 200*time.Millisecond)
			poke(e.rt.ops)
		}()
	}
	return ops, err
}

func (e *Engine) apply(ctx context.Context, uid int64, ids []int64, act Action, delay time.Duration) ([]int64, error) {
	msgs, err := e.st.MessagesByIDs(ctx, uid, ids)
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, store.ErrNotFound
	}
	runAfter := time.Now().Add(delay).UTC().Format(time.RFC3339Nano)
	var opIDs []int64
	touched := map[int64]bool{}
	accounts := map[int64]store.Account{}
	account := func(id int64) (store.Account, error) {
		if a, ok := accounts[id]; ok {
			return a, nil
		}
		a, err := e.st.GetAccount(ctx, uid, id)
		if err == nil {
			accounts[id] = a
		}
		return a, err
	}
	queue := func(m store.Message, op string, folder string, srcUID int64, args opArgs) error {
		b, _ := json.Marshal(args)
		mid := m.ID
		po := &store.PendingOp{UserID: uid, AccountID: m.AccountID, Op: op, Folder: folder, UID: srcUID, MessageID: &mid, Args: b, RunAfter: runAfter}
		if err := e.st.AddOp(ctx, po); err != nil {
			return err
		}
		opIDs = append(opIDs, po.ID)
		return nil
	}
	for _, m := range msgs {
		a, err := account(m.AccountID)
		if err != nil {
			return opIDs, err
		}
		touched[m.FolderID] = true
		switch act.Kind {
		case "read", "unread", "star", "unstar":
			var f store.Flags
			args := opArgs{PrevSeen: boolp(m.Seen), PrevFlagged: boolp(m.Flagged)}
			switch act.Kind {
			case "read":
				if m.Seen {
					continue
				}
				f.Seen, args.Add = boolp(true), []string{`\Seen`}
			case "unread":
				if !m.Seen {
					continue
				}
				f.Seen, args.Del = boolp(false), []string{`\Seen`}
			case "star":
				if m.Flagged {
					continue
				}
				f.Flagged, args.Add = boolp(true), []string{`\Flagged`}
			case "unstar":
				if !m.Flagged {
					continue
				}
				f.Flagged, args.Del = boolp(false), []string{`\Flagged`}
			}
			if err := e.st.SetFlags(ctx, m.ID, f); err != nil {
				return opIDs, err
			}
			if err := queue(m, "flags", m.FolderName, m.UID, args); err != nil {
				return opIDs, err
			}
		case "delete":
			if m.FolderRole == "trash" || m.FolderRole == "junk" {
				atts, _ := e.st.Attachments(ctx, 0, m.ID)
				for i := range atts {
					if full, err := e.st.GetAttachment(ctx, 0, atts[i].ID); err == nil {
						atts[i] = full
					}
				}
				args := opArgs{Snap: snapOf(m, atts), FromID: m.FolderID}
				if err := e.st.DeleteMessage(ctx, m.ID); err != nil {
					return opIDs, err
				}
				m.ID = 0
				b, _ := json.Marshal(args)
				po := &store.PendingOp{UserID: uid, AccountID: m.AccountID, Op: "delete", Folder: m.FolderName, UID: m.UID, Args: b, RunAfter: runAfter}
				if err := e.st.AddOp(ctx, po); err != nil {
					return opIDs, err
				}
				opIDs = append(opIDs, po.ID)
				continue
			}
			fallthrough
		case "archive", "trash", "spam", "notspam", "inbox", "move":
			var dest store.Folder
			create := false
			switch act.Kind {
			case "move":
				dest, err = e.st.GetFolder(ctx, uid, act.FolderID)
				if err != nil {
					return opIDs, err
				}
				if dest.AccountID != m.AccountID {
					return opIDs, errors.New("messages can't move between accounts")
				}
			case "inbox", "notspam":
				dest, err = e.st.FolderByRole(ctx, m.AccountID, "inbox")
			case "archive":
				dest, create, err = e.ensureRoleFolder(ctx, a, "archive")
			case "trash", "delete":
				dest, create, err = e.ensureRoleFolder(ctx, a, "trash")
			case "spam":
				dest, create, err = e.ensureRoleFolder(ctx, a, "junk")
			}
			if err != nil {
				return opIDs, err
			}
			if dest.ID == m.FolderID {
				continue
			}
			srcName, srcUID := m.FolderName, m.UID
			// a message still on its way somewhere: replace that move
			if srcUID <= 0 {
				if pend, _ := e.st.PendingForMessage(ctx, m.ID); len(pend) > 0 {
					for _, p := range pend {
						if p.Op != "move" || p.Attempts > 0 {
							continue
						}
						if n, _ := e.st.CancelOps(ctx, uid, []int64{p.ID}); n == 1 {
							var pa opArgs
							_ = json.Unmarshal(p.Args, &pa)
							srcName, srcUID = p.Folder, p.UID
							if pa.FromID != 0 {
								touched[pa.FromID] = true
							}
						}
					}
				}
			}
			args := opArgs{To: dest.Name, ToID: dest.ID, FromID: m.FolderID, PrevUID: m.UID, Create: create, RoleOfDest: dest.Role}
			if err := e.st.SetUID(ctx, m.ID, dest.ID, -m.ID); err != nil {
				return opIDs, err
			}
			touched[dest.ID] = true
			if err := queue(m, "move", srcName, srcUID, args); err != nil {
				return opIDs, err
			}
		default:
			return opIDs, fmt.Errorf("unknown action %q", act.Kind)
		}
	}
	for fid := range touched {
		_ = e.st.RefreshFolderCounts(ctx, fid)
	}
	return opIDs, nil
}

// Undo takes back changes that have not reached the server yet and restores
// the messages here.
func (e *Engine) Undo(ctx context.Context, uid int64, opIDs []int64) error {
	ops, err := e.st.GetOps(ctx, uid, opIDs)
	if err != nil {
		return err
	}
	undone := 0
	touched := map[int64]bool{}
	for _, op := range ops { // newest first
		if op.Attempts > 0 {
			continue
		}
		if n, err := e.st.CancelOps(ctx, uid, []int64{op.ID}); err != nil || n == 0 {
			continue
		}
		undone++
		var args opArgs
		_ = json.Unmarshal(op.Args, &args)
		switch op.Op {
		case "flags":
			if op.MessageID != nil {
				_ = e.st.SetFlags(ctx, *op.MessageID, store.Flags{Seen: args.PrevSeen, Flagged: args.PrevFlagged})
				if m, err := e.st.GetMessage(ctx, 0, *op.MessageID); err == nil {
					touched[m.FolderID] = true
				}
			}
		case "move":
			if op.MessageID != nil {
				// back where it was: the source of this queued move (its own
				// UID there, or still pending from an earlier move)
				back := args.FromID
				if f, err := e.st.FolderByName(ctx, op.AccountID, op.Folder); err == nil {
					back = f.ID
				}
				buid := op.UID
				if buid <= 0 {
					buid = -*op.MessageID
				}
				if err := e.st.SetUID(ctx, *op.MessageID, back, buid); err != nil {
					_ = e.st.SetUID(ctx, *op.MessageID, back, -*op.MessageID)
				}
				touched[back] = true
				touched[args.ToID] = true
			}
		case "delete":
			if args.Snap != nil {
				m, atts := args.Snap.restore()
				m.ID = 0
				if err := e.st.InsertMessage(ctx, &m, atts); err == nil {
					touched[m.FolderID] = true
				}
			}
		}
	}
	for fid := range touched {
		_ = e.st.RefreshFolderCounts(ctx, fid)
	}
	if undone == 0 && len(opIDs) > 0 {
		return errors.New("too late to undo: the change already reached the mail server")
	}
	return nil
}

// opsLoop applies due changes, every few seconds or when poked.
func (e *Engine) opsLoop(ctx context.Context) {
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-e.rt.ops:
		}
		ids, err := e.st.AccountsWithDueOps(ctx, time.Now())
		if err != nil {
			continue
		}
		for _, id := range ids {
			e.applyOps(ctx, id)
		}
	}
}

// applyOps runs one account's due changes against its server.
func (e *Engine) applyOps(ctx context.Context, accountID int64) {
	a, err := e.st.GetAccount(ctx, 0, accountID)
	if err != nil {
		return
	}
	ops, err := e.st.DueOps(ctx, accountID, time.Now())
	if err != nil || len(ops) == 0 {
		return
	}
	changed := false
	for _, op := range ops {
		if ctx.Err() != nil {
			return
		}
		var args opArgs
		_ = json.Unmarshal(op.Args, &args)
		var err error
		if a.Provider == "demo" {
			err = e.applyLocal(ctx, op, args)
		} else {
			var ch bool
			ch, err = e.applyRemote(ctx, &a, op, args)
			changed = changed || ch
		}
		if err != nil {
			if errors.Is(err, errLater) {
				_ = e.st.RetryOp(ctx, op, "waiting for an earlier change")
				continue
			}
			e.log.Debug("apply change", "account", accountID, "op", op.Op, "err", err)
			_ = e.st.RetryOp(ctx, op, err.Error())
			if isAuthError(err) || strings.Contains(err.Error(), "can't reach") {
				break // the server is away; the rest waits for the next round
			}
			continue
		}
		_ = e.st.DoneOp(ctx, op.ID)
	}
	if changed {
		e.changed(a.UserID, a.ID, nil)
	}
}

var errLater = errors.New("later")

// applyLocal finishes a change for an account without a server (demo).
func (e *Engine) applyLocal(ctx context.Context, op store.PendingOp, args opArgs) error {
	if op.Op == "move" && op.MessageID != nil {
		m, err := e.st.GetMessage(ctx, 0, *op.MessageID)
		if err != nil {
			return nil
		}
		if m.UID <= 0 {
			return e.st.SetUID(ctx, m.ID, m.FolderID, e.st.MaxUID(ctx, m.FolderID)+1)
		}
	}
	return nil
}

func imapFlags(list []string) []imap.Flag {
	out := make([]imap.Flag, len(list))
	for i, f := range list {
		out[i] = imap.Flag(f)
	}
	return out
}

// applyRemote runs one change on the server. The bool says the local view changed.
func (e *Engine) applyRemote(ctx context.Context, a *store.Account, op store.PendingOp, args opArgs) (bool, error) {
	switch op.Op {
	case "flags":
		if op.MessageID == nil {
			return false, nil
		}
		m, err := e.st.GetMessage(ctx, 0, *op.MessageID)
		if err != nil {
			return false, nil // gone here: nothing to do
		}
		if m.UID <= 0 {
			return false, errLater
		}
		return false, e.withWork(ctx, a, m.FolderName, func(c *imapclient.Client) error {
			set := imap.UIDSetNum(imap.UID(m.UID))
			if len(args.Add) > 0 {
				if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: imapFlags(args.Add)}, nil).Close(); err != nil {
					return err
				}
			}
			if len(args.Del) > 0 {
				if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsDel, Silent: true, Flags: imapFlags(args.Del)}, nil).Close(); err != nil {
					return err
				}
			}
			return nil
		})
	case "delete":
		if op.UID <= 0 {
			return false, nil
		}
		return false, e.withWork(ctx, a, op.Folder, func(c *imapclient.Client) error {
			set := imap.UIDSetNum(imap.UID(op.UID))
			if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
				return err
			}
			if c.Caps().Has(imap.CapUIDPlus) {
				return c.UIDExpunge(set).Close()
			}
			return c.Expunge().Close()
		})
	case "move":
		if op.UID <= 0 {
			return false, errLater // the earlier move hasn't landed yet
		}
		if args.Create {
			err := e.withWork(ctx, a, "", func(c *imapclient.Client) error {
				if err := c.Create(args.To, nil).Wait(); err != nil && !alreadyExists(err) {
					return err
				}
				_ = c.Subscribe(args.To).Wait()
				return nil
			})
			if err != nil {
				return false, err
			}
		}
		var newUID imap.UID
		err := e.withWork(ctx, a, op.Folder, func(c *imapclient.Client) error {
			set := imap.UIDSetNum(imap.UID(op.UID))
			if c.Caps().Has(imap.CapMove) {
				md, err := c.Move(set, args.To).Wait()
				if err != nil {
					return err
				}
				newUID = firstUID(md.SourceUIDs, md.DestUIDs, imap.UID(op.UID))
				return nil
			}
			cd, err := c.Copy(set, args.To).Wait()
			if err != nil {
				return err
			}
			newUID = firstUID(cd.SourceUIDs, cd.DestUIDs, imap.UID(op.UID))
			if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}, nil).Close(); err != nil {
				return err
			}
			if c.Caps().Has(imap.CapUIDPlus) {
				return c.UIDExpunge(set).Close()
			}
			return c.Expunge().Close()
		})
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "no such message") || strings.Contains(strings.ToLower(err.Error()), "nonexistent") {
				return false, nil
			}
			return false, err
		}
		if op.MessageID == nil {
			return false, nil
		}
		m, err := e.st.GetMessage(ctx, 0, *op.MessageID)
		if err != nil {
			return false, nil
		}
		if newUID == 0 {
			// the server didn't say the new UID: the next sync brings it back
			if m.FolderID == args.ToID {
				_ = e.st.DeleteMessage(ctx, m.ID)
				_ = e.st.RefreshFolderCounts(ctx, args.ToID)
				e.Kick(a.ID)
				return true, nil
			}
			return false, nil
		}
		if m.FolderID == args.ToID {
			if err := e.st.SetUID(ctx, m.ID, args.ToID, int64(newUID)); err != nil {
				// the sync already brought it in under its new UID
				_ = e.st.DeleteMessage(ctx, m.ID)
				_ = e.st.RefreshFolderCounts(ctx, args.ToID)
				return true, nil
			}
		} else {
			// moved on again here meanwhile: the next move starts from where it is now
			_ = e.st.UpdateOpSource(ctx, m.ID, args.To, int64(newUID))
		}
		return false, nil
	}
	return false, nil
}

// firstUID finds the destination UID of src in a COPYUID answer.
func firstUID(src, dst imap.NumSet, uid imap.UID) imap.UID {
	ss, ok1 := src.(imap.UIDSet)
	ds, ok2 := dst.(imap.UIDSet)
	if !ok1 || !ok2 {
		return 0
	}
	sl, ok1 := ss.Nums()
	dl, ok2 := ds.Nums()
	if !ok1 || !ok2 || len(sl) != len(dl) {
		return 0
	}
	for i, s := range sl {
		if s == uid {
			return dl[i]
		}
	}
	return 0
}
