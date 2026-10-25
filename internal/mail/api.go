// Package mail is the mail engine: provider presets and discovery, IMAP sync
// (folders, messages, flags, IDLE), the queue of changes for the server, the
// outbox (SMTP), MIME building and parsing, OAuth for Google and Microsoft,
// HTML sanitizing for safe display, invitations and unsubscribe.
//
// This file is the contract the HTTP API codes against. Accounts with
// provider "demo" never touch the network: their changes apply locally and
// sending files the message straight into the local Sent folder.
package mail

import (
	"context"
	"log/slog"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/secret"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// Config is what the engine needs from the app.
type Config struct {
	SyncInterval   time.Duration // full check of every account (IDLE pushes the inbox in between)
	MaxMessageSize int64         // bytes; bigger bodies/attachments are not fetched
	UserAgent      string
	// OAuth returns the OAuth clients in effect (env or admin settings), read each time.
	OAuth func(ctx context.Context) OAuthClients
	// OnChange is called after a sync or a change touched a person's mail;
	// newIDs are messages that arrived (not on the first sync of an account).
	OnChange func(uid, accountID int64, newIDs []int64)
	// OnSent is called after a draft went out (or failed for good: err != nil).
	OnSent func(d store.Draft, err error)
	// OnNew is called for each newly arrived message after the rules ran
	// (notifications, events). Not on the first sync of an account.
	OnNew func(m store.Message)
}

// OAuthClients are the "Sign in with …" clients.
type OAuthClients struct {
	GoogleID, GoogleSecret       string
	MicrosoftID, MicrosoftSecret string
	MicrosoftTenant              string // "common" by default
}

// Engine runs the background sync for every account on the box.
type Engine struct {
	st  *store.Store
	box *secret.Box
	cfg Config
	log *slog.Logger
	rt  *runtime
}

// Status is what the account page shows while syncing.
type Status struct {
	Syncing  bool   `json:"syncing"`
	Progress string `json:"progress"` // "Inbox: 1,200 of 4,000"
	Idle     bool   `json:"idle"`     // a live (IDLE) connection to the inbox is open
}

// Preset is what is known about a provider, from the built-in list, the
// Thunderbird ISPDB or the domain's MX records.
type Preset struct {
	Provider     string   `json:"provider"` // gmail | icloud | outlook | yahoo | fastmail | ... | custom
	Name         string   `json:"name"`     // "Gmail"
	IMAPHost     string   `json:"imap_host"`
	IMAPPort     int      `json:"imap_port"`
	IMAPSecurity string   `json:"imap_security"`
	SMTPHost     string   `json:"smtp_host"`
	SMTPPort     int      `json:"smtp_port"`
	SMTPSecurity string   `json:"smtp_security"`
	Username     string   `json:"username"` // the login (usually the address)
	Auth         []string `json:"auth"`     // "oauth", "password" in the order to offer them
	OAuth        string   `json:"oauth"`    // google | microsoft | ""
	SavesSent    bool     `json:"saves_sent"`
	AppPassword  bool     `json:"app_password"` // needs an app-specific password, not the normal one
	HelpURL      string   `json:"help_url"`     // where to make an app password
	Help         string   `json:"help"`         // one or two plain sentences
	Found        string   `json:"found"`        // builtin | ispdb | mx | guess
}

// Invite is a calendar invitation found in a message (text/calendar).
type Invite struct {
	UID       string   `json:"uid"`
	Method    string   `json:"method"` // REQUEST | CANCEL | REPLY
	Summary   string   `json:"summary"`
	Location  string   `json:"location"`
	Start     string   `json:"start"` // RFC3339
	End       string   `json:"end"`
	AllDay    bool     `json:"all_day"`
	Organizer Address  `json:"organizer"`
	Attendees []string `json:"attendees"`
	ICS       string   `json:"-"`
}

// Address is a name and an address.
type Address = store.Address

// Tokens are what an OAuth sign-in returns.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
	Email        string // the signed-in address
	Name         string
}

// SanitizeOptions controls how a message's HTML is cleaned for display.
type SanitizeOptions struct {
	AllowRemote bool              // keep remote images (otherwise blocked and counted)
	Inline      map[string]string // content-id (without <>) → URL or data: URI for cid: images
}

// Unsubscribe is how to leave a mailing list.
type Unsubscribe struct {
	Method string `json:"method"` // one_click | mailto | url | none
	URL    string `json:"url"`    // https URL (open in a browser) or the one-click endpoint
	Mailto string `json:"mailto"` // address (with ?subject=… kept)
}
