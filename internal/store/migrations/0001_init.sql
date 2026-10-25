CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- One mail account (Gmail, iCloud, Outlook, any IMAP/SMTP server) of one
-- person. Passwords and OAuth tokens are sealed with DATA_DIR/secret.key.
CREATE TABLE accounts (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name           TEXT NOT NULL DEFAULT '',          -- your name on outgoing mail
  email          TEXT NOT NULL,
  label          TEXT NOT NULL DEFAULT '',          -- "Work", "Personal"; '' = the address
  provider       TEXT NOT NULL DEFAULT 'custom',    -- gmail | icloud | outlook | yahoo | fastmail | ... | custom | demo
  auth           TEXT NOT NULL DEFAULT 'password',  -- password | oauth
  oauth_provider TEXT NOT NULL DEFAULT '',          -- google | microsoft
  username       TEXT NOT NULL DEFAULT '',
  imap_host      TEXT NOT NULL DEFAULT '',
  imap_port      INTEGER NOT NULL DEFAULT 993,
  imap_security  TEXT NOT NULL DEFAULT 'ssl',       -- ssl | starttls | none
  smtp_host      TEXT NOT NULL DEFAULT '',
  smtp_port      INTEGER NOT NULL DEFAULT 465,
  smtp_security  TEXT NOT NULL DEFAULT 'ssl',
  secret         TEXT NOT NULL DEFAULT '',          -- sealed password or OAuth refresh token
  access_token   TEXT NOT NULL DEFAULT '',          -- sealed OAuth access token
  token_expiry   TEXT NOT NULL DEFAULT '',
  color          TEXT NOT NULL DEFAULT '',
  signature      TEXT NOT NULL DEFAULT '',          -- HTML
  sort           INTEGER NOT NULL DEFAULT 0,
  sync_enabled   INTEGER NOT NULL DEFAULT 1,
  sync_days      INTEGER NOT NULL DEFAULT 90,       -- how far back mail is kept here
  saves_sent     INTEGER NOT NULL DEFAULT 0,        -- the server files sent mail itself (Gmail, Outlook)
  sync_state     TEXT NOT NULL DEFAULT 'new',       -- new | syncing | ok | error | auth
  sync_error     TEXT NOT NULL DEFAULT '',
  last_sync_at   TEXT NOT NULL DEFAULT '',
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX accounts_user ON accounts(user_id, sort);

-- An IMAP folder (mailbox) and how far it is synced.
CREATE TABLE folders (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  name           TEXT NOT NULL,                     -- full IMAP name, "INBOX", "[Gmail]/Sent Mail"
  display        TEXT NOT NULL DEFAULT '',          -- last path element, decoded
  delimiter      TEXT NOT NULL DEFAULT '/',
  role           TEXT NOT NULL DEFAULT '',          -- inbox | sent | drafts | trash | junk | archive | all | flagged | important | ''
  sync           INTEGER NOT NULL DEFAULT 1,
  selectable     INTEGER NOT NULL DEFAULT 1,
  uidvalidity    INTEGER NOT NULL DEFAULT 0,
  uidnext        INTEGER NOT NULL DEFAULT 0,
  highest_modseq INTEGER NOT NULL DEFAULT 0,
  last_uid       INTEGER NOT NULL DEFAULT 0,        -- the highest UID fetched
  oldest_uid     INTEGER NOT NULL DEFAULT 0,        -- the lowest UID fetched (backfill goes below it)
  unread         INTEGER NOT NULL DEFAULT 0,
  total          INTEGER NOT NULL DEFAULT 0,
  last_sync_at   TEXT NOT NULL DEFAULT '',
  UNIQUE (account_id, name)
);
CREATE INDEX folders_user ON folders(user_id, account_id);

-- One message in one folder. uid is the IMAP UID; a negative uid is a move
-- still on its way to the server (the real UID arrives with the next sync).
CREATE TABLE messages (
  id               INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id          INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id       INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  folder_id        INTEGER NOT NULL REFERENCES folders(id) ON DELETE CASCADE,
  uid              INTEGER NOT NULL,
  message_id       TEXT NOT NULL DEFAULT '',        -- the Message-ID header, without <>
  in_reply_to      TEXT NOT NULL DEFAULT '',
  refs             TEXT NOT NULL DEFAULT '',        -- References, space separated, without <>
  thread_key       TEXT NOT NULL DEFAULT '',        -- the conversation: the root Message-ID
  subject          TEXT NOT NULL DEFAULT '',
  from_name        TEXT NOT NULL DEFAULT '',
  from_addr        TEXT NOT NULL DEFAULT '',
  reply_to         TEXT NOT NULL DEFAULT '',        -- JSON [{name,addr}]
  to_json          TEXT NOT NULL DEFAULT '[]',
  cc_json          TEXT NOT NULL DEFAULT '[]',
  bcc_json         TEXT NOT NULL DEFAULT '[]',
  date             TEXT NOT NULL,                   -- RFC3339 UTC
  snippet          TEXT NOT NULL DEFAULT '',
  size             INTEGER NOT NULL DEFAULT 0,
  seen             INTEGER NOT NULL DEFAULT 0,
  flagged          INTEGER NOT NULL DEFAULT 0,
  answered         INTEGER NOT NULL DEFAULT 0,
  forwarded        INTEGER NOT NULL DEFAULT 0,
  draft            INTEGER NOT NULL DEFAULT 0,
  has_attachments  INTEGER NOT NULL DEFAULT 0,
  has_invite       INTEGER NOT NULL DEFAULT 0,      -- carries a text/calendar invitation
  category         TEXT NOT NULL DEFAULT 'people',  -- people | updates | newsletters | receipts
  list_id          TEXT NOT NULL DEFAULT '',
  list_unsubscribe TEXT NOT NULL DEFAULT '',        -- the List-Unsubscribe header
  unsubscribe_post INTEGER NOT NULL DEFAULT 0,      -- RFC 8058 one-click
  text_part        TEXT NOT NULL DEFAULT '',        -- IMAP section of the text/plain part
  html_part        TEXT NOT NULL DEFAULT '',
  body_text        TEXT NOT NULL DEFAULT '',
  body_html        TEXT NOT NULL DEFAULT '',
  body_fetched     INTEGER NOT NULL DEFAULT 0,
  snooze_until     TEXT NOT NULL DEFAULT '',        -- hidden from the inbox until then (local)
  pinned           INTEGER NOT NULL DEFAULT 0,      -- kept on top of the inbox (local)
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  UNIQUE (folder_id, uid)
);
CREATE INDEX messages_folder_date ON messages(user_id, folder_id, date DESC);
CREATE INDEX messages_user_date ON messages(user_id, date DESC);
CREATE INDEX messages_thread ON messages(user_id, account_id, thread_key);
CREATE INDEX messages_msgid ON messages(user_id, message_id);
CREATE INDEX messages_from ON messages(user_id, from_addr);
CREATE INDEX messages_snooze ON messages(user_id, snooze_until) WHERE snooze_until != '';

-- Full-text search over subject, people and the text (rowid = messages.id).
CREATE VIRTUAL TABLE messages_fts USING fts5(subject, people, body, tokenize = 'unicode61 remove_diacritics 2');

-- A part of a message that is a file (or an inline image). Fetched from the
-- server when opened; data is only kept for demo and small inline parts.
CREATE TABLE attachments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message_id  INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  part        TEXT NOT NULL DEFAULT '',             -- IMAP section, "2" or "1.2"
  filename    TEXT NOT NULL DEFAULT '',
  mime        TEXT NOT NULL DEFAULT 'application/octet-stream',
  size        INTEGER NOT NULL DEFAULT 0,
  encoding    TEXT NOT NULL DEFAULT '',             -- base64 | quoted-printable | 7bit ...
  content_id  TEXT NOT NULL DEFAULT '',
  inline      INTEGER NOT NULL DEFAULT 0,
  data        BLOB
);
CREATE INDEX attachments_message ON attachments(user_id, message_id);

-- Something you started writing: a new message, a reply or a forward. Saved
-- as you type; queued with send_at when you press Send (undo send = cancel
-- before send_at), or scheduled for later.
CREATE TABLE drafts (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id        INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  kind           TEXT NOT NULL DEFAULT 'new',      -- new | reply | reply_all | forward
  reply_to_id    INTEGER,                          -- messages.id answered or forwarded
  to_json        TEXT NOT NULL DEFAULT '[]',
  cc_json        TEXT NOT NULL DEFAULT '[]',
  bcc_json       TEXT NOT NULL DEFAULT '[]',
  subject        TEXT NOT NULL DEFAULT '',
  body_html      TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT 'draft',    -- draft | queued | sending | sent | failed
  send_at        TEXT NOT NULL DEFAULT '',
  sent_at        TEXT NOT NULL DEFAULT '',
  error          TEXT NOT NULL DEFAULT '',
  attempts       INTEGER NOT NULL DEFAULT 0,
  message_id     TEXT NOT NULL DEFAULT '',         -- the Message-ID it goes out with
  source         TEXT NOT NULL DEFAULT '',         -- '' = you; an app slug when another app sent it
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX drafts_user ON drafts(user_id, status, updated_at DESC);
CREATE INDEX drafts_queue ON drafts(status, send_at);

-- A file attached to a draft (uploaded, forwarded, or picked from a Pocket app).
CREATE TABLE draft_files (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  draft_id  INTEGER NOT NULL REFERENCES drafts(id) ON DELETE CASCADE,
  filename  TEXT NOT NULL DEFAULT '',
  mime      TEXT NOT NULL DEFAULT 'application/octet-stream',
  size      INTEGER NOT NULL DEFAULT 0,
  data      BLOB NOT NULL
);
CREATE INDEX draft_files_draft ON draft_files(user_id, draft_id);

-- A change made here that still has to reach the mail server (seen, star,
-- move, delete). Applied after run_after so Undo can take it back.
CREATE TABLE pending_ops (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  op          TEXT NOT NULL,                         -- flags | move | delete | append
  folder      TEXT NOT NULL DEFAULT '',              -- source folder name
  uid         INTEGER NOT NULL DEFAULT 0,
  message_id  INTEGER,                               -- local messages.id
  args        TEXT NOT NULL DEFAULT '{}',            -- JSON
  run_after   TEXT NOT NULL DEFAULT '',
  attempts    INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX pending_ops_account ON pending_ops(account_id, run_after);

-- What you decided about a sender: show their images, mark them important
-- (VIP), mute them, or that you unsubscribed.
CREATE TABLE senders (
  user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  addr            TEXT NOT NULL,
  name            TEXT NOT NULL DEFAULT '',
  allow_images    INTEGER NOT NULL DEFAULT 0,
  vip             INTEGER NOT NULL DEFAULT 0,
  muted           INTEGER NOT NULL DEFAULT 0,
  unsubscribed_at TEXT NOT NULL DEFAULT '',
  updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  PRIMARY KEY (user_id, addr)
);

-- A rule run on new mail: when it matches, do these things (move, mark,
-- categorize, or hand attachments to a Pocket app).
CREATE TABLE rules (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  account_id  INTEGER REFERENCES accounts(id) ON DELETE CASCADE,  -- NULL = every account
  name        TEXT NOT NULL DEFAULT '',
  enabled     INTEGER NOT NULL DEFAULT 1,
  match       TEXT NOT NULL DEFAULT '{}',   -- JSON {from, to, subject, has_attachment, category}
  actions     TEXT NOT NULL DEFAULT '{}',   -- JSON {mark_read, star, archive, move_to, category, save_to: [documents|files|photos], task}
  hits        INTEGER NOT NULL DEFAULT 0,
  last_hit_at TEXT NOT NULL DEFAULT '',
  sort        INTEGER NOT NULL DEFAULT 0,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX rules_user ON rules(user_id, sort);

-- Reusable replies.
CREATE TABLE templates (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL DEFAULT '',
  subject     TEXT NOT NULL DEFAULT '',
  body_html   TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX templates_user ON templates(user_id, name);

-- What was made from a message in another Pocket app (a task, a document, a
-- calendar event…), so the message shows "Saved to Documents · Open".
CREATE TABLE app_links (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  message_id  INTEGER REFERENCES messages(id) ON DELETE SET NULL,
  thread_key  TEXT NOT NULL DEFAULT '',
  app         TEXT NOT NULL,          -- documents | files | photos | tasks | calendar | contacts | notes | money
  kind        TEXT NOT NULL DEFAULT '',  -- attachment | message | task | event | contact | note | expense
  label       TEXT NOT NULL DEFAULT '',
  item_id     TEXT NOT NULL DEFAULT '',
  item_url    TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX app_links_message ON app_links(user_id, message_id);
CREATE INDEX app_links_thread ON app_links(user_id, thread_key);

-- Keep the search index in step with the messages.
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, subject, people, body)
  VALUES (new.id, new.subject, new.from_name || ' ' || new.from_addr || ' ' || new.to_json || ' ' || new.cc_json, new.body_text);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE OF subject, from_name, from_addr, to_json, cc_json, body_text ON messages BEGIN
  DELETE FROM messages_fts WHERE rowid = old.id;
  INSERT INTO messages_fts(rowid, subject, people, body)
  VALUES (new.id, new.subject, new.from_name || ' ' || new.from_addr || ' ' || new.to_json || ' ' || new.cc_json, new.body_text);
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
  DELETE FROM messages_fts WHERE rowid = old.id;
END;
