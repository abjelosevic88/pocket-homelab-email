package mail

// TEMPORARY: the signatures of the engine with placeholder bodies, so the
// HTTP API compiles while the engine is written. Replaced file by file;
// delete this file when every function has a real home.

import (
	"context"
	"errors"
	"log/slog"

	"github.com/abjelosevic88/pocket-homelab-email/internal/secret"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

var errTodo = errors.New("not built yet")

type runtime struct{}

// New makes the engine; Run starts it.
func New(st *store.Store, box *secret.Box, cfg Config, log *slog.Logger) *Engine {
	return &Engine{st: st, box: box, cfg: cfg, log: log, rt: &runtime{}}
}

// Run syncs every account until ctx ends: a loop per account (full check
// every SyncInterval, IDLE on the inbox in between, backfill to SyncDays,
// bodies of recent mail for search), the queue of changes, the outbox and
// the snooze alarm. Accounts added later are picked up by Kick.
func (e *Engine) Run(ctx context.Context) {}

// Kick syncs an account now and applies its queued changes (new accounts start here).
func (e *Engine) Kick(accountID int64) {}

// KickOutbox sends what is due now.
func (e *Engine) KickOutbox() {}

// Status is how an account's sync is doing right now.
func (e *Engine) Status(accountID int64) Status { return Status{} }

// Test signs in to IMAP and SMTP with these settings; password "" uses the
// account's stored secret (or OAuth tokens). The error is plain language.
func (e *Engine) Test(ctx context.Context, a store.Account, password string) error { return errTodo }

// EnsureBody fetches a message's text and HTML from the server when they are not here yet.
func (e *Engine) EnsureBody(ctx context.Context, m *store.Message) error { return nil }

// AttachmentData returns an attachment's bytes (from the server, or what is kept here).
func (e *Engine) AttachmentData(ctx context.Context, m store.Message, a store.Attachment) ([]byte, error) {
	if a.Data != nil {
		return a.Data, nil
	}
	return nil, errTodo
}

// Raw returns the whole message as an .eml.
func (e *Engine) Raw(ctx context.Context, m store.Message) ([]byte, error) { return nil, errTodo }

// Action is a change to messages made here.
type Action struct {
	Kind     string // read | unread | star | unstar | archive | trash | spam | notspam | inbox | delete | move
	FolderID int64  // for move
}

// Apply changes a person's messages here at once and queues the change for
// the server (after the undo window). It returns the queued change ids for Undo.
func (e *Engine) Apply(ctx context.Context, uid int64, ids []int64, act Action) ([]int64, error) {
	return nil, errTodo
}

// Undo takes back changes that have not reached the server yet and restores
// the messages here.
func (e *Engine) Undo(ctx context.Context, uid int64, opIDs []int64) error { return errTodo }

// Unsubscribe leaves the mailing list a message came from: an RFC 8058
// one-click POST, or a mail to the list's unsubscribe address. For a plain
// web link it does nothing and returns it (Method "url") for the browser.
func (e *Engine) Unsubscribe(ctx context.Context, m store.Message) (Unsubscribe, error) {
	return Unsubscribe{Method: "none"}, errTodo
}

// CreateFolder makes a folder on the server and here.
func (e *Engine) CreateFolder(ctx context.Context, a store.Account, name string) (store.Folder, error) {
	return store.Folder{}, errTodo
}

// Discover works out the servers for an address: the built-in providers,
// then Thunderbird's ISPDB, then the domain's MX records, then a guess.
func Discover(ctx context.Context, email string) Preset { return Preset{Provider: "custom", Found: "guess"} }

// Providers lists the built-in providers for the "Add account" picker.
func Providers() []Preset { return nil }

// Sanitize makes a message's HTML safe to show: no scripts, no forms, no
// event handlers, links open in a new tab, remote images blocked unless
// allowed (the second value counts the blocked ones), cid: images resolved.
func Sanitize(html string, o SanitizeOptions) (string, int) { return "", 0 }

// HTMLToText is the readable text of an HTML body (search, snippets, AI).
func HTMLToText(html string) string { return html }

// Snippet is the first ~200 characters of a text, on one line.
func Snippet(text string) string { return text }

// ParseInvite reads the first event of a text/calendar part.
func ParseInvite(ics []byte) (*Invite, error) { return nil, errTodo }

// ParseUnsubscribe reads a List-Unsubscribe header.
func ParseUnsubscribe(header string, oneClick bool) Unsubscribe { return Unsubscribe{Method: "none"} }

// NewMessageID makes a Message-ID (without <>) for an address's domain.
func NewMessageID(fromAddr string) string { return "" }

// BuildMessage writes a draft as an RFC 5322 message (multipart/alternative
// text + HTML, attachments, In-Reply-To/References from orig).
func BuildMessage(a store.Account, d store.Draft, files []store.DraftFile, orig *store.Message) ([]byte, error) {
	return nil, errTodo
}

// OAuthAuthURL is where to send the browser to sign in (PKCE S256 with verifier).
func OAuthAuthURL(c OAuthClients, provider, redirectURI, state, verifier, loginHint string) (string, error) {
	return "", errTodo
}

// OAuthExchange trades the code for tokens and the signed-in address.
func OAuthExchange(ctx context.Context, c OAuthClients, provider, redirectURI, code, verifier string) (Tokens, error) {
	return Tokens{}, errTodo
}
