package mail

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/abjelosevic88/pocket-homelab-email/internal/secret"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// runtime is the engine's moving parts.
type runtime struct {
	mu       sync.Mutex
	ctx      context.Context // set by Run
	accounts map[int64]*acctRun
	locks    map[int64]*sync.Mutex // token refresh per account
	outbox   chan struct{}
	ops      chan struct{}
}

// acctRun is one account's sync loop and its connection for actions.
type acctRun struct {
	id     int64
	kick   chan struct{}
	inbox  chan struct{} // the server says the inbox changed (IDLE)
	cancel context.CancelFunc

	mu     sync.Mutex
	status Status

	work workConn
}

// workConn is the connection for what a person does (open a message, an
// attachment, apply a change, file sent mail), so it never waits behind a
// big sync. Opened when needed, closed after a few idle minutes.
type workConn struct {
	mu       sync.Mutex
	c        *imapclient.Client
	selected string
	used     time.Time
}

// New makes the engine; Run starts it.
func New(st *store.Store, box *secret.Box, cfg Config, log *slog.Logger) *Engine {
	if cfg.SyncInterval <= 0 {
		cfg.SyncInterval = 5 * time.Minute
	}
	if cfg.MaxMessageSize <= 0 {
		cfg.MaxMessageSize = 25 << 20
	}
	if log == nil {
		log = slog.Default()
	}
	return &Engine{st: st, box: box, cfg: cfg, log: log, rt: &runtime{
		accounts: map[int64]*acctRun{}, locks: map[int64]*sync.Mutex{},
		outbox: make(chan struct{}, 1), ops: make(chan struct{}, 1),
	}}
}

func (rt *runtime) lockFor(id int64) *sync.Mutex {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	m := rt.locks[id]
	if m == nil {
		m = &sync.Mutex{}
		rt.locks[id] = m
	}
	return m
}

// run returns the account's runtime, made on first use.
func (e *Engine) run(id int64) *acctRun {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()
	r := e.rt.accounts[id]
	if r == nil {
		r = &acctRun{id: id, kick: make(chan struct{}, 1), inbox: make(chan struct{}, 1)}
		e.rt.accounts[id] = r
	}
	return r
}

func poke(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// Run syncs every account until ctx ends: a loop per account (full check
// every SyncInterval, IDLE on the inbox in between, backfill to SyncDays,
// bodies of recent mail for search), the queue of changes, the outbox and
// the snooze alarm. Accounts added later are picked up by Kick.
func (e *Engine) Run(ctx context.Context) {
	e.rt.mu.Lock()
	e.rt.ctx = ctx
	e.rt.mu.Unlock()
	_ = e.st.ResetSending(ctx)
	e.reconcile(ctx)
	go e.outboxLoop(ctx)
	go e.opsLoop(ctx)
	snooze := time.NewTicker(time.Minute)
	accounts := time.NewTicker(time.Minute)
	defer snooze.Stop()
	defer accounts.Stop()
	for {
		select {
		case <-ctx.Done():
			e.closeAll()
			return
		case <-snooze.C:
			e.wakeSnoozed(ctx)
		case <-accounts.C:
			e.reconcile(ctx)
			e.closeIdleWork()
		}
	}
}

// reconcile starts loops for new accounts and stops those that are gone.
func (e *Engine) reconcile(ctx context.Context) {
	list, err := e.st.AllAccounts(ctx)
	if err != nil {
		return
	}
	alive := map[int64]bool{}
	for _, a := range list {
		alive[a.ID] = true
		e.start(a.ID)
	}
	e.rt.mu.Lock()
	var gone []*acctRun
	for id, r := range e.rt.accounts {
		if !alive[id] {
			gone = append(gone, r)
			delete(e.rt.accounts, id)
		}
	}
	e.rt.mu.Unlock()
	for _, r := range gone {
		if r.cancel != nil {
			r.cancel()
		}
		r.work.close()
	}
}

// start runs an account's loop once Run has begun.
func (e *Engine) start(id int64) {
	e.rt.mu.Lock()
	ctx := e.rt.ctx
	e.rt.mu.Unlock()
	if ctx == nil {
		return
	}
	r := e.run(id)
	r.mu.Lock()
	if r.cancel != nil {
		r.mu.Unlock()
		return
	}
	actx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.mu.Unlock()
	go e.accountLoop(actx, r)
}

// Kick syncs an account now and applies its queued changes (new accounts start here).
func (e *Engine) Kick(accountID int64) {
	e.start(accountID)
	poke(e.run(accountID).kick)
	poke(e.rt.ops)
}

// KickOutbox sends what is due now.
func (e *Engine) KickOutbox() { poke(e.rt.outbox) }

// Status is how an account's sync is doing right now.
func (e *Engine) Status(accountID int64) Status {
	e.rt.mu.Lock()
	r := e.rt.accounts[accountID]
	e.rt.mu.Unlock()
	if r == nil {
		return Status{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *acctRun) setStatus(f func(*Status)) {
	r.mu.Lock()
	f(&r.status)
	r.mu.Unlock()
}

func (e *Engine) closeAll() {
	e.rt.mu.Lock()
	defer e.rt.mu.Unlock()
	for _, r := range e.rt.accounts {
		r.work.close()
	}
}

func (e *Engine) closeIdleWork() {
	e.rt.mu.Lock()
	list := make([]*acctRun, 0, len(e.rt.accounts))
	for _, r := range e.rt.accounts {
		list = append(list, r)
	}
	e.rt.mu.Unlock()
	for _, r := range list {
		r.work.mu.Lock()
		if r.work.c != nil && time.Since(r.work.used) > 3*time.Minute {
			_ = r.work.c.Logout().Wait()
			r.work.c.Close()
			r.work.c, r.work.selected = nil, ""
		}
		r.work.mu.Unlock()
	}
}

func (w *workConn) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.c != nil {
		w.c.Close()
		w.c, w.selected = nil, ""
	}
}

// withWork runs f on the account's action connection with folder selected
// ("" = none), reconnecting once when the connection dropped.
func (e *Engine) withWork(ctx context.Context, a *store.Account, folder string, f func(c *imapclient.Client) error) error {
	r := e.run(a.ID)
	w := &r.work
	w.mu.Lock()
	defer w.mu.Unlock()
	for attempt := 0; attempt < 2; attempt++ {
		if w.c == nil {
			c, err := e.dialIMAP(ctx, a, "", nil)
			if err != nil {
				return err
			}
			w.c, w.selected = c, ""
		}
		w.used = time.Now()
		if folder != "" && w.selected != folder {
			if _, err := w.c.Select(folder, nil).Wait(); err != nil {
				if connGone(w.c) {
					w.c.Close()
					w.c = nil
					continue
				}
				return err
			}
			w.selected = folder
		}
		err := f(w.c)
		if err != nil && connGone(w.c) {
			w.c.Close()
			w.c = nil
			if attempt == 0 {
				continue
			}
		}
		return err
	}
	return errors.New("the mail server closed the connection")
}

func connGone(c *imapclient.Client) bool {
	select {
	case <-c.Closed():
		return true
	default:
	}
	return c.State() == imap.ConnStateLogout || c.State() == imap.ConnStateNone
}

// changed tells the app a person's mail changed.
func (e *Engine) changed(uid, accountID int64, newIDs []int64) {
	if e.cfg.OnChange != nil {
		e.cfg.OnChange(uid, accountID, newIDs)
	}
}

// wakeSnoozed ends due snoozes and says so.
func (e *Engine) wakeSnoozed(ctx context.Context) {
	list, err := e.st.WakeSnoozed(ctx)
	if err != nil || len(list) == 0 {
		return
	}
	type key struct{ uid, acct int64 }
	seen := map[key][]int64{}
	for _, m := range list {
		k := key{m.UserID, m.AccountID}
		seen[k] = append(seen[k], m.ID)
		_ = e.st.RefreshFolderCounts(ctx, m.FolderID)
	}
	for k, ids := range seen {
		e.changed(k.uid, k.acct, ids)
	}
}
