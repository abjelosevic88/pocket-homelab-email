package store

import (
	"encoding/json"
	"strings"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Address is one person on a message.
type Address struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// Account is one mail account of one person. Secrets never leave the store
// unsealed and are never sent to the browser.
type Account struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Label         string `json:"label"`
	Provider      string `json:"provider"`
	Auth          string `json:"auth"`
	OAuthProvider string `json:"oauth_provider"`
	Username      string `json:"username"`
	IMAPHost      string `json:"imap_host"`
	IMAPPort      int    `json:"imap_port"`
	IMAPSecurity  string `json:"imap_security"`
	SMTPHost      string `json:"smtp_host"`
	SMTPPort      int    `json:"smtp_port"`
	SMTPSecurity  string `json:"smtp_security"`
	Secret        string `json:"-"` // sealed password or refresh token
	AccessToken   string `json:"-"` // sealed
	TokenExpiry   string `json:"-"`
	Color         string `json:"color"`
	Signature     string `json:"signature"`
	Sort          int    `json:"sort"`
	SyncEnabled   bool   `json:"sync_enabled"`
	SyncDays      int    `json:"sync_days"`
	SavesSent     bool   `json:"saves_sent"`
	SyncState     string `json:"sync_state"`
	SyncError     string `json:"sync_error"`
	LastSyncAt    string `json:"last_sync_at"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// DisplayLabel is what the sidebar shows for the account.
func (a Account) DisplayLabel() string {
	if a.Label != "" {
		return a.Label
	}
	return a.Email
}

// Folder is one IMAP folder of an account.
type Folder struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"-"`
	AccountID     int64  `json:"account_id"`
	Name          string `json:"name"`
	Display       string `json:"display"`
	Delimiter     string `json:"delimiter"`
	Role          string `json:"role"`
	Sync          bool   `json:"sync"`
	Selectable    bool   `json:"selectable"`
	UIDValidity   uint32 `json:"-"`
	UIDNext       uint32 `json:"-"`
	HighestModSeq uint64 `json:"-"`
	LastUID       uint32 `json:"-"`
	OldestUID     uint32 `json:"-"`
	Unread        int    `json:"unread"`
	Total         int    `json:"total"`
	LastSyncAt    string `json:"last_sync_at"`
}

// Message is one message in one folder.
type Message struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"-"`
	AccountID       int64     `json:"account_id"`
	FolderID        int64     `json:"folder_id"`
	UID             int64     `json:"-"`
	MessageID       string    `json:"message_id"`
	InReplyTo       string    `json:"in_reply_to"`
	Refs            string    `json:"-"`
	ThreadKey       string    `json:"thread_key"`
	Subject         string    `json:"subject"`
	From            Address   `json:"from"`
	ReplyTo         []Address `json:"reply_to"`
	To              []Address `json:"to"`
	Cc              []Address `json:"cc"`
	Bcc             []Address `json:"bcc"`
	Date            string    `json:"date"`
	Snippet         string    `json:"snippet"`
	Size            int64     `json:"size"`
	Seen            bool      `json:"seen"`
	Flagged         bool      `json:"flagged"`
	Answered        bool      `json:"answered"`
	Forwarded       bool      `json:"forwarded"`
	Draft           bool      `json:"draft"`
	HasAttachments  bool      `json:"has_attachments"`
	HasInvite       bool      `json:"has_invite"`
	Category        string    `json:"category"`
	ListID          string    `json:"list_id"`
	ListUnsubscribe string    `json:"-"`
	UnsubscribePost bool      `json:"-"`
	TextPart        string    `json:"-"`
	HTMLPart        string    `json:"-"`
	BodyText        string    `json:"-"`
	BodyHTML        string    `json:"-"`
	BodyFetched     bool      `json:"-"`
	SnoozeUntil     string    `json:"snooze_until"`
	Pinned          bool      `json:"pinned"`
	CreatedAt       string    `json:"created_at"`

	// Filled in for lists and views, not stored.
	FolderRole  string `json:"folder_role,omitempty"`
	FolderName  string `json:"folder_name,omitempty"`
	ThreadCount int    `json:"thread_count,omitempty"`
	ThreadUnread int   `json:"thread_unread,omitempty"`
	Participants []string `json:"participants,omitempty"`
}

// Attachment is a file in a message.
type Attachment struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	MessageID int64  `json:"message_id"`
	Part      string `json:"part"`
	Filename  string `json:"filename"`
	MIME      string `json:"mime"`
	Size      int64  `json:"size"`
	Encoding  string `json:"-"`
	ContentID string `json:"content_id"`
	Inline    bool   `json:"inline"`
	Data      []byte `json:"-"`
}

// Draft is something being written, queued or sent from here.
type Draft struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"-"`
	AccountID int64       `json:"account_id"`
	Kind      string      `json:"kind"`
	ReplyToID *int64      `json:"reply_to_id"`
	To        []Address   `json:"to"`
	Cc        []Address   `json:"cc"`
	Bcc       []Address   `json:"bcc"`
	Subject   string      `json:"subject"`
	BodyHTML  string      `json:"body_html"`
	Status    string      `json:"status"`
	SendAt    string      `json:"send_at"`
	SentAt    string      `json:"sent_at"`
	Error     string      `json:"error"`
	Attempts  int         `json:"attempts"`
	MessageID string      `json:"message_id"`
	Source    string      `json:"source"`
	Files     []DraftFile `json:"files"`
	CreatedAt string      `json:"created_at"`
	UpdatedAt string      `json:"updated_at"`
}

// DraftFile is a file attached to a draft.
type DraftFile struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"-"`
	DraftID  int64  `json:"draft_id"`
	Filename string `json:"filename"`
	MIME     string `json:"mime"`
	Size     int64  `json:"size"`
	Data     []byte `json:"-"`
}

// PendingOp is a change still to be applied on the mail server.
type PendingOp struct {
	ID        int64           `json:"id"`
	UserID    int64           `json:"-"`
	AccountID int64           `json:"account_id"`
	Op        string          `json:"op"` // flags | move | delete | append
	Folder    string          `json:"folder"`
	UID       int64           `json:"uid"`
	MessageID *int64          `json:"message_id"`
	Args      json.RawMessage `json:"args"`
	RunAfter  string          `json:"run_after"`
	Attempts  int             `json:"attempts"`
	Error     string          `json:"error"`
	CreatedAt string          `json:"created_at"`
}

// Sender is what a person decided about one sender.
type Sender struct {
	UserID         int64  `json:"-"`
	Addr           string `json:"addr"`
	Name           string `json:"name"`
	AllowImages    bool   `json:"allow_images"`
	VIP            bool   `json:"vip"`
	Muted          bool   `json:"muted"`
	UnsubscribedAt string `json:"unsubscribed_at"`
}

// RuleMatch says which new messages a rule applies to (all given parts must match).
type RuleMatch struct {
	From          string `json:"from,omitempty"`    // substring of the sender address or name
	To            string `json:"to,omitempty"`      // substring of a recipient
	Subject       string `json:"subject,omitempty"` // substring of the subject
	HasAttachment bool   `json:"has_attachment,omitempty"`
	Category      string `json:"category,omitempty"`
}

// RuleActions is what a rule does.
type RuleActions struct {
	MarkRead bool     `json:"mark_read,omitempty"`
	Star     bool     `json:"star,omitempty"`
	Archive  bool     `json:"archive,omitempty"`
	MoveTo   string   `json:"move_to,omitempty"` // folder name
	Category string   `json:"category,omitempty"`
	SaveTo   []string `json:"save_to,omitempty"` // documents | files | photos — the attachments
	Task     bool     `json:"task,omitempty"`    // make a Pocket Tasks task
	Notify   bool     `json:"notify,omitempty"`  // push a notification
}

// Rule runs on new mail.
type Rule struct {
	ID        int64       `json:"id"`
	UserID    int64       `json:"-"`
	AccountID *int64      `json:"account_id"`
	Name      string      `json:"name"`
	Enabled   bool        `json:"enabled"`
	Match     RuleMatch   `json:"match"`
	Actions   RuleActions `json:"actions"`
	Hits      int         `json:"hits"`
	LastHitAt string      `json:"last_hit_at"`
	Sort      int         `json:"sort"`
	CreatedAt string      `json:"created_at"`
}

// Template is a reusable reply.
type Template struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	BodyHTML  string `json:"body_html"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// AppLink is something made from a message in another Pocket app.
type AppLink struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	MessageID *int64 `json:"message_id"`
	ThreadKey string `json:"thread_key"`
	App       string `json:"app"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	ItemID    string `json:"item_id"`
	ItemURL   string `json:"item_url"`
	CreatedAt string `json:"created_at"`
}

// AppConn is how one person reaches one sibling Pocket app: "core" (an app
// link through Pocket Core), "direct" (URL + token) or "off"; "" = not chosen.
type AppConn struct {
	Mode      string `json:"mode"`
	URL       string `json:"url"`
	Token     string `json:"token"`
	PublicURL string `json:"public_url"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	// Box-wide (admin): the OAuth clients for "Sign in with Google/Microsoft".
	GoogleClientID        string `json:"google_client_id"`
	GoogleClientSecret    string `json:"google_client_secret"`
	MicrosoftClientID     string `json:"microsoft_client_id"`
	MicrosoftClientSecret string `json:"microsoft_client_secret"`
	MicrosoftTenant       string `json:"microsoft_tenant"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Reading.
	Density         string `json:"density"`          // comfortable | compact
	Conversations   bool   `json:"conversations"`    // group a thread into one row
	ReadingPane     string `json:"reading_pane"`     // right | off
	MarkReadAfter   int    `json:"mark_read_after"`  // seconds an open message needs before it is marked read; -1 = never
	RemoteImages    string `json:"remote_images"`    // ask | always | never
	InboxCategories bool   `json:"inbox_categories"` // People / Updates / Newsletters / Receipts tabs in the inbox
	ShowPreview     bool   `json:"show_preview"`
	AfterArchive    string `json:"after_archive"` // next | previous | list

	// Writing.
	UndoSendSeconds int    `json:"undo_send_seconds"` // 0 = off
	DefaultAccount  int64  `json:"default_account"`
	ReplyAll        bool   `json:"reply_all"` // the Reply button answers everyone
	ComposeFont     string `json:"compose_font"`

	// Notifications.
	NotifyNew    string `json:"notify_new"` // off | people | vip | all
	QuietFrom    int    `json:"quiet_from"` // hour; QuietFrom == QuietTo = no quiet hours
	QuietTo      int    `json:"quiet_to"`
	NotifyURL    string `json:"notify_url"`
	NotifyFormat string `json:"notify_format"` // apprise | ntfy | json

	// Pocket apps this person reaches (keyed by app slug).
	Apps map[string]AppConn `json:"apps"`

	// Photos go to Immich (through Pocket Core's service, or directly).
	ImmichMode string `json:"immich_mode"`
	ImmichURL  string `json:"immich_url"`
	ImmichKey  string `json:"immich_key"`

	// AI: Pocket AI through Core, or an OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
	AILanguage    string `json:"ai_language"` // "" = the language of the email
	AIDigest      bool   `json:"ai_digest"`   // "Catch me up" summary on the inbox
}

// App is the saved connection for one app slug (zero value when none).
func (st Settings) App(slug string) AppConn {
	if st.Apps == nil {
		return AppConn{}
	}
	return st.Apps[slug]
}

// SetApp stores a connection.
func (st *Settings) SetApp(slug string, c AppConn) {
	if st.Apps == nil {
		st.Apps = map[string]AppConn{}
	}
	st.Apps[slug] = c
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
