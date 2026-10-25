// The shapes of the JSON API (see docs/api.md).

export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = '' | 'core' | 'direct'

export interface Address { name: string; addr: string }

export type SyncState = 'new' | 'syncing' | 'ok' | 'error' | 'auth'
export type Security = 'ssl' | 'starttls' | 'none'

export interface Account {
  id: number
  name: string
  email: string
  label: string
  provider: string
  auth: 'password' | 'oauth'
  oauth_provider: '' | 'google' | 'microsoft'
  username: string
  imap_host: string
  imap_port: number
  imap_security: Security
  smtp_host: string
  smtp_port: number
  smtp_security: Security
  color: string
  signature: string
  sort: number
  sync_enabled: boolean
  sync_days: number
  saves_sent: boolean
  sync_state: SyncState
  sync_error: string
  last_sync_at: string
  created_at: string
  updated_at: string
}

export type FolderRole = '' | 'inbox' | 'sent' | 'drafts' | 'trash' | 'junk' | 'archive' | 'all' | 'flagged' | 'important'

export interface Folder {
  id: number
  account_id: number
  name: string
  display: string
  delimiter: string
  role: FolderRole
  sync: boolean
  selectable: boolean
  unread: number
  total: number
  last_sync_at: string
}

export interface SyncStatus { syncing: boolean; progress: string; idle: boolean }

export interface AccountView extends Account { folders: Folder[]; status: SyncStatus }

/** Numbers next to the views: inbox (unread), unread, starred, snoozed, spam (unread), drafts, scheduled, failed, people, updates, newsletters, receipts. */
export type Counts = Record<string, number>

export interface Sidebar { accounts: AccountView[]; counts: Counts }

export type Category = 'people' | 'updates' | 'newsletters' | 'receipts'

export type Box = 'inbox' | 'unread' | 'starred' | 'snoozed' | 'drafts' | 'sent' | 'archive' | 'spam' | 'trash' | 'all' | 'attachments' | 'receipts' | 'newsletters' | 'updates' | 'people' | 'vip'

export interface Message {
  id: number
  account_id: number
  folder_id: number
  message_id: string
  in_reply_to: string
  thread_key: string
  subject: string
  from: Address
  reply_to: Address[]
  to: Address[]
  cc: Address[]
  bcc: Address[]
  date: string
  snippet: string
  size: number
  seen: boolean
  flagged: boolean
  answered: boolean
  forwarded: boolean
  draft: boolean
  has_attachments: boolean
  has_invite: boolean
  category: Category
  list_id: string
  snooze_until: string
  pinned: boolean
  created_at: string
  folder_role?: FolderRole
  folder_name?: string
  /** in lists: messages in the conversation (within the view) and how many are unread */
  thread_count?: number
  thread_unread?: number
}

export interface MessageList { messages: Message[]; more: boolean }

export interface Attachment {
  id: number
  message_id: number
  part: string
  filename: string
  mime: string
  size: number
  content_id: string
  inline: boolean
}

export interface AttachmentRow extends Attachment {
  subject: string
  from: Address
  date: string
  account_id: number
  thread_key: string
}

export interface Invite {
  uid: string
  method: string // REQUEST | CANCEL | REPLY
  summary: string
  location: string
  start: string
  end: string
  all_day: boolean
  organizer: Address
  attendees: string[]
}

export interface Unsubscribe { method: 'one_click' | 'mailto' | 'url' | 'none'; url: string; mailto: string }

export interface Sender { addr: string; name: string; allow_images: boolean; vip: boolean; muted: boolean; unsubscribed_at: string }

export type AppSlug = 'documents' | 'files' | 'photos' | 'tasks' | 'calendar' | 'contacts' | 'notes' | 'money'

/** Something made from a message in another Pocket app. */
export interface AppLink {
  id: number
  message_id: number | null
  thread_key: string
  app: AppSlug
  kind: string
  label: string
  item_id: string
  item_url: string
  created_at: string
}

/** One message as the reader shows it. html is already made safe (sanitized); "" = plain text only. */
export interface MessageFull {
  message: Message
  html: string
  text: string
  /** remote images held back; 0 = none or shown */
  images_blocked: number
  attachments: Attachment[]
  invite: Invite | null
  unsubscribe: Unsubscribe
  sender: Sender
  links: AppLink[]
}

export interface Thread {
  subject: string
  account_id: number
  thread_key: string
  messages: MessageFull[]
  links: AppLink[]
}

export type ActionKind = 'read' | 'unread' | 'star' | 'unstar' | 'archive' | 'trash' | 'spam' | 'notspam' | 'inbox' | 'delete' | 'move'
  | 'snooze' | 'unsnooze' | 'pin' | 'unpin' | 'category'

export interface ActionRequest {
  ids: number[]
  action: ActionKind
  /** apply to every message of the conversations of these ids */
  thread?: boolean
  folder_id?: number
  until?: string // snooze: RFC3339
  category?: Category
}

export interface ActionResult { ok: boolean; ops: number[]; affected: number[]; message: string }

export interface DraftFile { id: number; draft_id: number; filename: string; mime: string; size: number }

export type DraftStatus = 'draft' | 'queued' | 'sending' | 'sent' | 'failed'

export interface Draft {
  id: number
  account_id: number
  kind: 'new' | 'reply' | 'reply_all' | 'forward'
  reply_to_id: number | null
  to: Address[]
  cc: Address[]
  bcc: Address[]
  subject: string
  body_html: string
  status: DraftStatus
  send_at: string
  sent_at: string
  error: string
  attempts: number
  message_id: string
  source: string
  files: DraftFile[]
  created_at: string
  updated_at: string
}

export interface ComposeRequest {
  kind: 'new' | 'reply' | 'reply_all' | 'forward'
  message_id?: number
  account_id?: number
  to?: Address[]
  subject?: string
  body_html?: string
}

export interface SendResult { draft: Draft; undo_until: string }

export interface Preset {
  provider: string
  name: string
  imap_host: string
  imap_port: number
  imap_security: Security
  smtp_host: string
  smtp_port: number
  smtp_security: Security
  username: string
  auth: ('oauth' | 'password')[]
  oauth: '' | 'google' | 'microsoft'
  saves_sent: boolean
  app_password: boolean
  help_url: string
  help: string
  found: 'builtin' | 'ispdb' | 'mx' | 'guess'
  /** discover/providers: the OAuth client for this provider is set up on this box */
  oauth_ready?: boolean
}

export interface AccountInput {
  name?: string
  email?: string
  label?: string
  provider?: string
  username?: string
  password?: string
  imap_host?: string
  imap_port?: number
  imap_security?: Security
  smtp_host?: string
  smtp_port?: number
  smtp_security?: Security
  saves_sent?: boolean
  color?: string
  signature?: string
  sync_enabled?: boolean
  sync_days?: number
  sort?: number
}

export interface Subscription {
  addr: string
  name: string
  list_id: string
  count: number
  unread: number
  last: string
  last_subject: string
  last_message_id: number
  can_unsubscribe: boolean
  one_click: boolean
  unsubscribed_at: string
  muted: boolean
  category: Category
}

export interface UnsubscribeResult { method: Unsubscribe['method']; url: string; done: boolean; message: string }

export interface Template { id: number; name: string; subject: string; body_html: string; created_at: string; updated_at: string }

export interface RuleMatch { from?: string; to?: string; subject?: string; has_attachment?: boolean; category?: Category | '' }
export interface RuleActions {
  mark_read?: boolean
  star?: boolean
  archive?: boolean
  move_to?: string
  category?: Category | ''
  save_to?: ('documents' | 'files' | 'photos')[]
  task?: boolean
  notify?: boolean
}
export interface Rule {
  id: number
  account_id: number | null
  name: string
  enabled: boolean
  match: RuleMatch
  actions: RuleActions
  hits: number
  last_hit_at: string
  sort: number
  created_at: string
}

/** GET /apps: what the reader can offer for a message. */
export interface AppState { name: string; configured: boolean; detail: string; open_url: string }
export interface AppsStatus {
  apps: Partial<Record<AppSlug, AppState>>
  ai: { ready: boolean; provider: string; detail: string }
  core: boolean
}

export interface Recipient { name: string; addr: string; source: 'contacts' | 'mail'; photo_url?: string }

export interface ContactCard { found: boolean; name: string; org: string; title: string; phone: string; photo_url: string; url: string }

export interface DayEvent { title: string; start: string; end: string; all_day: boolean }

export interface FileEntry { name: string; path: string; dir: boolean; size: number; mime: string }
export interface DocumentHit { id: number; title: string; created: string; mime: string }
export interface MoneyAccount { id: number; name: string; currency: string }

export interface AISummary { summary: string; points: string[]; actions: string[] }
export interface AIExtract {
  tasks: { title: string; due?: string }[]
  events: { title: string; start: string; end?: string; all_day?: boolean; location?: string }[]
  expense: { amount: number; currency: string; date: string; merchant: string } | null
}
export interface AIDigest { digest: string; items: { message_id: number; from: string; subject: string; why: string }[] }

/** Settings as the API returns them (secrets masked). Per Pocket app slug: <slug>_mode/_via/_detail/_configured/_url/_token/_public_url/_open_url. */
export interface Settings {
  core_configured: boolean
  core_registered?: boolean
  title: string
  date_format: string
  time_format: string
  week_start: number
  density: 'comfortable' | 'compact'
  conversations: boolean
  reading_pane: 'right' | 'off'
  mark_read_after: number
  remote_images: 'ask' | 'always' | 'never'
  inbox_categories: boolean
  show_preview: boolean
  after_archive: 'next' | 'previous' | 'list'
  undo_send_seconds: number
  default_account: number
  reply_all: boolean
  compose_font: string
  notify_new: 'off' | 'people' | 'vip' | 'all'
  quiet_from: number
  quiet_to: number
  notify_url: string
  notify_format: string
  notify_configured: boolean
  immich_url: string
  immich_key: string
  immich_mode: ConnMode
  ai_provider: 'auto' | 'pocket' | 'openai'
  ai_pocket_model: string
  openai_api_key: string
  openai_model: string
  openai_base_url: string
  ai_language: string
  ai_digest: boolean
  setup_complete: boolean
  base_url: string
  oauth_google: boolean
  oauth_microsoft: boolean
  oauth_redirect_google: string
  oauth_redirect_microsoft: string
  // admins only
  google_client_id?: string
  google_client_secret?: string
  microsoft_client_id?: string
  microsoft_client_secret?: string
  microsoft_tenant?: string
  oauth_from_env?: boolean
  [k: string]: unknown
}

export interface SystemInfo {
  version: string
  go: string
  os: string
  arch: string
  core: { configured: boolean; url: string; registered: boolean; error: string }
  auth_mode: string
  data_dir: string
  db_path: string
  db_bytes: number
  uptime: string
  timezone: string
  counts: Record<string, number>
  saved: number
}

/** Live updates from GET /api/v1/events (server-sent events, JSON in data). */
export type LiveEvent =
  | { type: 'change'; account_id: number; new: number[] }
  | { type: 'sync'; account_id: number; state: SyncState; error: string }
  | { type: 'sent'; draft_id: number; ok: boolean; error: string }
