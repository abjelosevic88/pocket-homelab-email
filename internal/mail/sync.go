package mail

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

const (
	headerBatch   = 200
	firstBodies   = 50      // bodies fetched right away per folder on a first sync
	bodyCap       = 1 << 20 // the most of one text part kept for reading and search
	backfillBatch = 150     // bodies fetched per cycle in the background
	idleRestart   = 25 * time.Minute
)

// accountLoop keeps one account in step with its server until ctx ends.
func (e *Engine) accountLoop(ctx context.Context, r *acctRun) {
	defer func() {
		r.mu.Lock()
		r.cancel = nil
		r.mu.Unlock()
	}()
	var c *imapclient.Client
	closeConn := func() {
		if c != nil {
			c.Close()
			c = nil
		}
	}
	defer closeConn()
	backoff := 30 * time.Second
	for {
		a, err := e.st.GetAccount(ctx, 0, r.id)
		if err != nil {
			return // deleted
		}
		if a.Provider == "demo" || !a.SyncEnabled {
			closeConn()
			select {
			case <-ctx.Done():
				return
			case <-r.kick:
				continue
			case <-time.After(e.cfg.SyncInterval):
				continue
			}
		}
		if c == nil || connGone(c) {
			closeConn()
			handler := &imapclient.UnilateralDataHandler{
				Mailbox: func(d *imapclient.UnilateralDataMailbox) {
					if d.NumMessages != nil {
						poke(r.inbox)
					}
				},
				Expunge: func(uint32) { poke(r.inbox) },
			}
			_ = e.st.SetAccountSync(ctx, a.ID, "syncing", "")
			c, err = e.dialIMAP(ctx, &a, "", handler)
			if err != nil {
				e.syncFailed(ctx, r, &a, err, &backoff)
				if !e.wait(ctx, r, backoff) {
					return
				}
				continue
			}
		}
		err = e.syncAccount(ctx, c, r, &a)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			e.syncFailed(ctx, r, &a, err, &backoff)
			closeConn()
			if !e.wait(ctx, r, backoff) {
				return
			}
			continue
		}
		backoff = 30 * time.Second
		// between full syncs: IDLE on the inbox (or poll it)
		deadline := time.Now().Add(e.cfg.SyncInterval)
		for time.Now().Before(deadline) {
			why, err := e.idle(ctx, c, r, time.Until(deadline))
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				closeConn()
				break
			}
			if why == "kick" {
				break
			}
			if why == "inbox" {
				if err := e.syncInbox(ctx, c, r, &a); err != nil {
					closeConn()
					break
				}
			}
		}
	}
}

func (e *Engine) wait(ctx context.Context, r *acctRun, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-r.kick:
		return true
	case <-time.After(d):
		return true
	}
}

func (e *Engine) syncFailed(ctx context.Context, r *acctRun, a *store.Account, err error, backoff *time.Duration) {
	r.setStatus(func(s *Status) { s.Syncing, s.Idle, s.Progress = false, false, "" })
	state := "error"
	if isAuthError(err) {
		state = "auth"
		*backoff = 30 * time.Minute // don't hammer a server with a wrong password
	} else if *backoff < 10*time.Minute {
		*backoff *= 2
	}
	e.log.Warn("mail sync failed", "account", a.ID, "err", err)
	_ = e.st.SetAccountSync(ctx, a.ID, state, err.Error())
	if a.SyncState != state {
		e.changed(a.UserID, a.ID, nil)
	}
}

// idle waits on the selected inbox for news. It returns "inbox" (something
// arrived or changed), "kick" or "timeout".
func (e *Engine) idle(ctx context.Context, c *imapclient.Client, r *acctRun, max time.Duration) (string, error) {
	if max > idleRestart {
		max = idleRestart
	}
	if max <= 0 {
		return "timeout", nil
	}
	if _, err := c.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return "", err
	}
	select {
	case <-r.inbox: // drained: news that came during the sync is already in
	default:
	}
	if !c.Caps().Has(imap.CapIdle) {
		poll := 2 * time.Minute
		if max < poll {
			poll = max
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-r.kick:
			return "kick", nil
		case <-time.After(poll):
			return "inbox", nil
		}
	}
	r.setStatus(func(s *Status) { s.Idle = true })
	defer r.setStatus(func(s *Status) { s.Idle = false })
	cmd, err := c.Idle()
	if err != nil {
		return "", err
	}
	why := "timeout"
	select {
	case <-ctx.Done():
		why = "ctx"
	case <-r.kick:
		why = "kick"
	case <-r.inbox:
		why = "inbox"
	case <-c.Closed():
		return "", errors.New("connection closed")
	case <-time.After(max):
	}
	if err := cmd.Close(); err != nil {
		return "", err
	}
	if err := cmd.Wait(); err != nil {
		return "", err
	}
	if why == "ctx" {
		return "", ctx.Err()
	}
	return why, nil
}

// folderOrder: the inbox first, then what people look at most.
func folderRank(f store.Folder) int {
	switch f.Role {
	case "inbox":
		return 0
	case "sent":
		return 1
	case "drafts":
		return 2
	case "archive":
		return 3
	case "":
		return 4
	case "junk":
		return 5
	case "trash":
		return 6
	}
	return 7
}

// syncAccount brings every synced folder of an account up to date.
func (e *Engine) syncAccount(ctx context.Context, c *imapclient.Client, r *acctRun, a *store.Account) error {
	first := a.LastSyncAt == ""
	r.setStatus(func(s *Status) { s.Syncing, s.Progress = true, "Folders" })
	defer r.setStatus(func(s *Status) { s.Syncing, s.Progress = false, "" })
	_ = e.st.SetAccountSync(ctx, a.ID, "syncing", "")
	folders, err := e.syncFolders(ctx, c, a)
	if err != nil {
		return err
	}
	sort.SliceStable(folders, func(i, j int) bool { return folderRank(folders[i]) < folderRank(folders[j]) })
	for _, f := range folders {
		if !f.Sync || !f.Selectable {
			continue
		}
		if err := e.syncFolder(ctx, c, r, a, f, first); err != nil {
			if connGone(c) {
				return err
			}
			e.log.Debug("folder sync failed", "account", a.ID, "folder", f.Name, "err", err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	_ = e.st.SetAccountSync(ctx, a.ID, "ok", "")
	if first {
		e.changed(a.UserID, a.ID, nil)
	}
	a.LastSyncAt = store.Now()
	r.setStatus(func(s *Status) { s.Progress = "Getting the text of older messages for search" })
	e.backfill(ctx, c, a)
	return nil
}

// syncInbox is the quick pass after IDLE says something changed.
func (e *Engine) syncInbox(ctx context.Context, c *imapclient.Client, r *acctRun, a *store.Account) error {
	f, err := e.st.FolderByRole(ctx, a.ID, "inbox")
	if err != nil {
		return nil
	}
	return e.syncFolder(ctx, c, r, a, f, false)
}

// syncFolder brings one folder up to date within the account's window.
func (e *Engine) syncFolder(ctx context.Context, c *imapclient.Client, r *acctRun, a *store.Account, f store.Folder, accountFirst bool) error {
	sel, err := c.Select(f.Name, &imap.SelectOptions{ReadOnly: true, CondStore: c.Caps().Has(imap.CapCondStore)}).Wait()
	if err != nil {
		return err
	}
	folderFirst := f.LastSyncAt == ""
	if f.UIDValidity != 0 && sel.UIDValidity != f.UIDValidity {
		_ = e.st.ClearFolder(ctx, f.ID)
		f.LastUID, f.OldestUID, f.HighestModSeq = 0, 0, 0
		folderFirst = true
	}
	quiet := accountFirst || folderFirst // no "new mail" for what was always there
	days := a.SyncDays
	if days <= 0 {
		days = 90
	}
	since := time.Now().AddDate(0, 0, -days)
	var window []imap.UID
	if sel.NumMessages > 0 {
		sd, err := c.UIDSearch(&imap.SearchCriteria{Since: since}, nil).Wait()
		if err != nil {
			return err
		}
		window = sd.AllUIDs()
	}
	inWindow := make(map[uint32]bool, len(window))
	for _, u := range window {
		inWindow[uint32(u)] = true
	}
	local, err := e.st.FolderUIDs(ctx, f.ID)
	if err != nil {
		return err
	}
	changed := false

	// gone from the window: deleted on the server, or just older than the window
	var missing []imap.UID
	for u := range local {
		if !inWindow[u] {
			missing = append(missing, imap.UID(u))
		}
	}
	if len(missing) > 0 {
		still := map[uint32]bool{}
		if sel.NumMessages > 0 {
			sd, err := c.UIDSearch(&imap.SearchCriteria{UID: []imap.UIDSet{imap.UIDSetNum(missing...)}}, nil).Wait()
			if err == nil {
				for _, u := range sd.AllUIDs() {
					still[uint32(u)] = true
				}
			}
		}
		var gone []uint32
		for _, u := range missing {
			if !still[uint32(u)] {
				gone = append(gone, uint32(u))
			}
		}
		if len(gone) > 0 {
			_ = e.st.DeleteUIDs(ctx, f.ID, gone)
			changed = true
		}
	}

	// flags of what is here
	if len(local) > 0 && sel.NumMessages > 0 {
		if n, err := e.syncFlags(ctx, c, f, sel, local, inWindow); err == nil && n > 0 {
			changed = true
		}
	}

	// new messages, newest first
	var fresh []imap.UID
	for _, u := range window {
		if _, ok := local[uint32(u)]; !ok {
			fresh = append(fresh, u)
		}
	}
	sort.Slice(fresh, func(i, j int) bool { return fresh[i] > fresh[j] })
	var newIDs []int64
	var arrived []store.Message
	total := len(fresh)
	done := 0
	bodies := 0
	for len(fresh) > 0 {
		n := len(fresh)
		if n > headerBatch {
			n = headerBatch
		}
		batch := fresh[:n]
		fresh = fresh[n:]
		msgs, err := e.fetchHeaders(ctx, c, a, f, batch)
		if err != nil {
			return err
		}
		var needBody []store.Message
		for i := range msgs {
			m := &msgs[i]
			if err := e.st.InsertMessage(ctx, &m.Message, m.atts); err != nil {
				e.log.Debug("store message", "err", err)
				continue
			}
			changed = true
			isNew := !quiet && uint32(m.UID) > f.LastUID
			if isNew {
				newIDs = append(newIDs, m.ID)
				arrived = append(arrived, m.Message)
			}
			if isNew || bodies < firstBodies {
				needBody = append(needBody, m.Message)
				bodies++
			}
		}
		if len(needBody) > 0 {
			e.fetchBodies(ctx, c, needBody)
		}
		done += n
		if total > headerBatch {
			name := f.Display
			r.setStatus(func(s *Status) { s.Progress = fmt.Sprintf("%s: %d of %d", name, done, total) })
		}
		if len(fresh) > 0 && changed {
			_ = e.st.RefreshFolderCounts(ctx, f.ID)
			e.changed(a.UserID, a.ID, nil)
		}
	}

	// keep the window: older messages go (flagged, pinned and snoozed stay)
	_ = e.st.PruneOlder(ctx, f.ID, since.UTC().Format(time.RFC3339))

	f.UIDValidity, f.UIDNext, f.HighestModSeq = sel.UIDValidity, uint32(sel.UIDNext), sel.HighestModSeq
	maxU, minU := f.LastUID, f.OldestUID
	for _, u := range window {
		if uint32(u) > maxU {
			maxU = uint32(u)
		}
		if minU == 0 || uint32(u) < minU {
			minU = uint32(u)
		}
	}
	f.LastUID, f.OldestUID = maxU, minU
	_ = e.st.SaveFolderState(ctx, &f)
	_ = e.st.RefreshFolderCounts(ctx, f.ID)

	if f.Role == "inbox" {
		for _, m := range arrived {
			e.newMail(ctx, a, m)
		}
	}
	if changed || len(newIDs) > 0 {
		e.changed(a.UserID, a.ID, newIDs)
	}
	return nil
}

// syncFlags refreshes the flags of the stored messages: CHANGEDSINCE when
// the server keeps mod-sequences, else every message in the window.
func (e *Engine) syncFlags(ctx context.Context, c *imapclient.Client, f store.Folder, sel *imap.SelectData, local map[uint32]store.UIDState, inWindow map[uint32]bool) (int, error) {
	opts := &imap.FetchOptions{UID: true, Flags: true}
	var set imap.UIDSet
	if f.HighestModSeq > 0 && sel.HighestModSeq > 0 && c.Caps().Has(imap.CapCondStore) {
		if sel.HighestModSeq == f.HighestModSeq {
			return 0, nil
		}
		opts.ChangedSince = f.HighestModSeq
		opts.ModSeq = true
		set.AddRange(1, 0) // 1:*
	} else {
		var uids []imap.UID
		for u := range local {
			if inWindow[u] {
				uids = append(uids, imap.UID(u))
			}
		}
		if len(uids) == 0 {
			return 0, nil
		}
		set = imap.UIDSetNum(uids...)
	}
	msgs, err := c.Fetch(set, opts).Collect()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, fm := range msgs {
		st, ok := local[uint32(fm.UID)]
		if !ok {
			continue
		}
		fl := flagsOf(fm.Flags)
		if fl.seen != st.Seen || fl.flagged != st.Flagged || fl.answered != st.Answered || fl.forwarded != st.Forwarded || fl.draft != st.Draft {
			_ = e.st.SetFlags(ctx, st.ID, store.Flags{Seen: &fl.seen, Flagged: &fl.flagged, Answered: &fl.answered, Forwarded: &fl.forwarded, Draft: &fl.draft})
			n++
		}
	}
	return n, nil
}

type flagSet struct{ seen, flagged, answered, forwarded, draft bool }

func flagsOf(list []imap.Flag) flagSet {
	var f flagSet
	for _, fl := range list {
		switch strings.ToLower(string(fl)) {
		case `\seen`:
			f.seen = true
		case `\flagged`:
			f.flagged = true
		case `\answered`:
			f.answered = true
		case `\draft`:
			f.draft = true
		case `$forwarded`:
			f.forwarded = true
		}
	}
	return f
}

// fetched is a message with its attachments, before it is stored.
type fetched struct {
	store.Message
	atts []store.Attachment
}

// fetchHeaders reads the envelope, flags and structure of some UIDs.
func (e *Engine) fetchHeaders(ctx context.Context, c *imapclient.Client, a *store.Account, f store.Folder, uids []imap.UID) ([]fetched, error) {
	hdr := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, HeaderFields: headerFields, Peek: true}
	opts := &imap.FetchOptions{UID: true, Flags: true, Envelope: true, InternalDate: true, RFC822Size: true,
		BodyStructure: &imap.FetchItemBodyStructure{Extended: true}, BodySection: []*imap.FetchItemBodySection{hdr}}
	list, err := c.Fetch(imap.UIDSetNum(uids...), opts).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]fetched, 0, len(list))
	for _, fm := range list {
		if fm.UID == 0 {
			continue
		}
		out = append(out, buildMessage(a, f, fm, fm.FindBodySection(hdr)))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID > out[j].UID })
	return out, nil
}

func buildMessage(a *store.Account, f store.Folder, fm *imapclient.FetchMessageBuffer, hdr []byte) fetched {
	m := fetched{Message: store.Message{UserID: a.UserID, AccountID: a.ID, FolderID: f.ID, UID: int64(fm.UID), Size: fm.RFC822Size}}
	hi := parseHeaderFields(hdr)
	env := fm.Envelope
	if env == nil {
		env = &imap.Envelope{}
	}
	m.Subject = strings.TrimSpace(env.Subject)
	m.MessageID = env.MessageID
	if len(env.InReplyTo) > 0 {
		m.InReplyTo = env.InReplyTo[0]
	}
	m.Refs = hi.refs
	from := toAddrs(env.From)
	if len(from) == 0 {
		from = toAddrs(env.Sender)
	}
	if len(from) > 0 {
		m.From = from[0]
	}
	m.ReplyTo, m.To, m.Cc, m.Bcc = toAddrs(env.ReplyTo), toAddrs(env.To), toAddrs(env.Cc), toAddrs(env.Bcc)
	if len(m.ReplyTo) == 1 && strings.EqualFold(m.ReplyTo[0].Addr, m.From.Addr) {
		m.ReplyTo = []store.Address{}
	}
	d := fm.InternalDate
	if d.IsZero() {
		d = env.Date
	}
	if d.IsZero() {
		d = time.Now()
	}
	m.Date = d.UTC().Format(time.RFC3339)
	fl := flagsOf(fm.Flags)
	m.Seen, m.Flagged, m.Answered, m.Forwarded, m.Draft = fl.seen, fl.flagged, fl.answered, fl.forwarded, fl.draft
	st := analyze(fm.BodyStructure)
	m.TextPart, m.HTMLPart = st.text.encode(), st.html.encode()
	m.atts = st.attachments
	for _, at := range st.attachments {
		if !at.Inline {
			m.HasAttachments = true
		}
	}
	m.HasInvite = st.invite
	m.ListID, m.ListUnsubscribe, m.UnsubscribePost = hi.listID, hi.listUnsub, hi.oneClick
	switch f.Role {
	case "sent", "drafts":
		m.Category = "people"
	default:
		m.Category = categorize(m.From.Addr, m.Subject, hi)
	}
	if m.TextPart == "" && m.HTMLPart == "" {
		m.BodyFetched = true // nothing to read but the files
	}
	return m
}

// fetchBodies fetches and stores the text of some messages of the selected
// folder: one FETCH per kind of structure, so a single-part message's
// BODY[TEXT] is never asked of a multipart one.
func (e *Engine) fetchBodies(ctx context.Context, c *imapclient.Client, msgs []store.Message) {
	groups := map[string][]*store.Message{}
	var keys []string
	for i := range msgs {
		m := &msgs[i]
		if m.UID <= 0 || (m.TextPart == "" && m.HTMLPart == "") {
			continue
		}
		k := decodePart(m.TextPart).section + " " + decodePart(m.HTMLPart).section
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], m)
	}
	for _, k := range keys {
		if ctx.Err() != nil {
			return
		}
		ms := groups[k]
		byUID := map[imap.UID]*store.Message{}
		var uids []imap.UID
		for _, m := range ms {
			byUID[imap.UID(m.UID)] = m
			uids = append(uids, imap.UID(m.UID))
		}
		tp, hp := decodePart(ms[0].TextPart), decodePart(ms[0].HTMLPart)
		var secs []*imap.FetchItemBodySection
		if tp.section != "" {
			secs = append(secs, sectionOf(tp.section, bodyCap))
		}
		if hp.section != "" && hp.section != tp.section {
			secs = append(secs, sectionOf(hp.section, bodyCap))
		}
		list, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, BodySection: secs}).Collect()
		if err != nil {
			e.log.Debug("fetch bodies", "err", err)
			return
		}
		for _, fm := range list {
			m := byUID[fm.UID]
			if m == nil {
				continue
			}
			var text, html string
			if m.TextPart != "" {
				p := decodePart(m.TextPart)
				text = decodeBody(fm.FindBodySection(sectionOf(p.section, bodyCap)), p, "text/plain")
			}
			if m.HTMLPart != "" {
				p := decodePart(m.HTMLPart)
				html = decodeBody(fm.FindBodySection(sectionOf(p.section, bodyCap)), p, "text/html")
			}
			e.storeBody(ctx, m, text, html)
		}
	}
}

func (e *Engine) storeBody(ctx context.Context, m *store.Message, text, html string) {
	if strings.TrimSpace(text) == "" && html != "" {
		text = HTMLToText(html)
	}
	snip := Snippet(text)
	if err := e.st.SetBody(ctx, m.ID, text, html, snip); err == nil {
		m.BodyText, m.BodyHTML, m.BodyFetched = text, html, true
		if snip != "" {
			m.Snippet = snip
		}
	}
}

// backfill fetches the text of older messages, a batch per cycle, so search covers the window.
func (e *Engine) backfill(ctx context.Context, c *imapclient.Client, a *store.Account) {
	list, err := e.st.MessagesNeedingBody(ctx, a.ID, backfillBatch)
	if err != nil || len(list) == 0 {
		return
	}
	byFolder := map[string][]store.Message{}
	var order []string
	for _, m := range list {
		if _, ok := byFolder[m.FolderName]; !ok {
			order = append(order, m.FolderName)
		}
		byFolder[m.FolderName] = append(byFolder[m.FolderName], m)
	}
	for _, name := range order {
		if ctx.Err() != nil {
			return
		}
		if _, err := c.Select(name, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
			continue
		}
		e.fetchBodies(ctx, c, byFolder[name])
	}
	e.changed(a.UserID, a.ID, nil)
}

// newMail runs the rules on a message that just arrived, then tells the app.
func (e *Engine) newMail(ctx context.Context, a *store.Account, m store.Message) {
	e.runRules(ctx, a, &m)
	if e.cfg.OnNew != nil {
		if cur, err := e.st.GetMessage(ctx, 0, m.ID); err == nil {
			m = cur
		}
		e.cfg.OnNew(m)
	}
}
