# API

JSON over HTTP under `/api/v1`. Authenticate with the session cookie, a personal token (`Authorization: Bearer pml_…`, Settings → API & shortcuts), a Pocket Core token, or a Pocket Core link token (`pcx_…`, only the routes of its scope). Everything is per person: other people's ids answer 404. Passwords and OAuth tokens of mail accounts are never returned.

## Accounts

| | |
|---|---|
| `GET /providers` | Built-in providers (Gmail, iCloud, Outlook, Yahoo, Fastmail, …) with servers, sign-in methods and help; `oauth_ready` when "Sign in with …" is set up |
| `GET /accounts/discover?email=` | What is known about an address: built-in provider, Thunderbird's ISPDB, the domain's MX records, or a guess |
| `POST /accounts/test` | Check a sign-in before saving: `{email, password, username?, imap_host?, imap_port?, imap_security?, smtp_host?, …}` → `{ok, error}` |
| `GET /accounts`, `POST /accounts` | Your accounts (with folders and sync status) / add one with a password (it is tested first) |
| `GET/PUT/DELETE /accounts/{id}` | One account; PUT takes any subset (`label`, `name`, `color`, `signature`, `sync_enabled`, `sync_days`, server fields, `password`) |
| `POST /accounts/{id}/sync` | Sync now |
| `GET/POST /accounts/{id}/folders`, `PUT /folders/{id}` `{sync}` | Folders; make one on the server; switch syncing a folder on or off |
| `GET /oauth/{google\|microsoft}/start?email=&account_id=` | Sign in with Google / Microsoft (browser redirect); comes back to `/settings/accounts?connected=<id>` |

## Reading

| | |
|---|---|
| `GET /sidebar` | `{accounts, counts}` — every account with folders and status, and the numbers next to the views |
| `GET /counts` | `{inbox, unread, starred, snoozed, spam, drafts, scheduled, failed, people, updates, newsletters, receipts}` |
| `GET /messages?box=&account=&folder=&category=&q=&unread=&starred=&attachments=&from=&conversations=&offset=&limit=` | One page of a view, newest first: `{messages, more}`. Views: `inbox unread starred snoozed drafts sent archive spam trash all attachments receipts newsletters updates people vip`. With conversations each row is a thread's newest message with `thread_count`/`thread_unread` |
| `GET /messages/{id}?images=1` | One message for reading: `{message, html (sanitized), text, images_blocked, attachments, invite, unsubscribe, sender, links}` |
| `GET /threads/{id}` | The conversation of a message, oldest first, each as above: `{subject, account_id, thread_key, messages, links}` |
| `GET /messages/{id}/raw` | The original as `.eml` |
| `GET /attachments?kind=images\|documents&q=&offset=` | Every attachment across your mail, newest first |
| `GET /attachments/{id}?download=1` | One attachment (images, PDF and text open in the browser; everything else downloads) |
| `GET /summary[/{date}]` | One day in numbers: received, from people, unread, sent, top senders |
| `GET /events` | Live updates (server-sent events): `change`, `sync`, `sent` |

Search (`q`) understands `from: to: subject: in:<view> before:YYYY-MM-DD after: newer_than:7d older_than:1m is:unread is:read is:starred has:attachment filename: account:`; other words are matched (as prefixes, accents ignored) in subjects, people and text.

## Changing

| | |
|---|---|
| `POST /actions` | `{ids, action, thread?, folder_id?, until?, category?}` → `{ok, ops, affected, message}`. Actions: `read unread star unstar archive trash spam notspam inbox delete move snooze unsnooze pin unpin category`. `thread: true` applies to the whole conversations (moves only touch the copies in the same folder). Server changes wait a few seconds so they can be undone |
| `POST /undo` `{ops}` | Take back changes that have not reached the server yet |

## Writing

| | |
|---|---|
| `POST /compose` | Start a draft: `{kind: new\|reply\|reply_all\|forward, message_id?, account_id?, to?, subject?, body_html?}`. Replies come with recipients, subject, signature and the quote; forwards with the attachments |
| `GET /drafts?status=draft,queued,failed,sent` | Drafts, the outbox and sent-from-here |
| `GET/PUT/DELETE /drafts/{id}` | One draft; PUT takes any subset of `account_id, to, cc, bcc, subject, body_html` |
| `POST /drafts/{id}/files` (multipart `file`), `GET/DELETE /drafts/{id}/files/{fid}` | Attachments |
| `POST /drafts/{id}/send` `{send_at?}` | Send (after the undo window) or schedule → `{draft, undo_until}` |
| `POST /drafts/{id}/cancel` | Undo send / cancel a scheduled message (back to a draft) |
| `POST /send` | One call for scripts and other apps: `{account_id? \| from?, to, cc?, bcc?, subject, body_html \| body_text, send_at?, attachments?: [{filename, mime, data (base64)}]}` → `202 {draft_id, status, send_at, message_id, from}` |

## Senders, lists, templates, rules

| | |
|---|---|
| `GET /senders`, `PUT /senders/{addr}` `{allow_images?, vip?, muted?}` | What you decided about a sender |
| `GET /subscriptions` | Newsletters and lists you get, with how much they send |
| `POST /subscriptions/unsubscribe` `{addr}` | One-click unsubscribe, an unsubscribe mail sent for you, or the page to open (`{method, url, done, message}`) |
| `POST /subscriptions/archive` `{addr}` | Archive everything from a sender in the inbox |
| `GET/POST /templates`, `PUT/DELETE /templates/{id}` | Reusable replies |
| `GET/POST /rules`, `PUT/DELETE /rules/{id}` | Rules for new mail: `match {from, to, subject, has_attachment, category}` → `actions {mark_read, star, archive, move_to, category, save_to: [documents, files, photos], task, notify}` |

## The Pocket apps and AI

| | |
|---|---|
| `GET /apps` | Which Pocket apps (and AI) are connected for you |
| `GET /recipients?q=` | Address suggestions: Pocket Contacts first, then people you write with |
| `POST /messages/{id}/save` `{app: documents\|files\|photos\|notes, attachment_ids?, whole?, path?}` | Save attachments or the whole email in another app |
| `POST /messages/{id}/task`, `/event`, `/contact`, `/note`, `/expense` | Make a task, a calendar event (or add the invitation), a contact, a note, an expense |
| `GET /apps/contacts/card?addr=`, `/apps/calendar/day?date=`, `/apps/files/browse?path=`, `/apps/documents/search?q=`, `/apps/money/accounts` | What the reader and the composer show from other apps |
| `POST /drafts/{id}/files/from` `{app: files\|documents, ref}` | Attach a file from Pocket Files or Pocket Documents |
| `POST /ai/summarize`, `/ai/reply`, `/ai/write`, `/ai/suggest`, `/ai/extract`, `/ai/digest`, `/ai/ask` | Summaries, reply drafts, writing help, quick replies, tasks/dates/amounts from a message, "catch me up" |

## Pocket Core

Offers `mail:read` (messages, conversations, counts, accounts, the day's summary) and `mail:send` (`POST /send`, filed in Sent like any other message) to other Pocket apps. Uses Pocket Documents (`documents:write`, `documents:read`), Pocket Files (`files:write`, `files:read`), Pocket Tasks (`tasks:write`), Pocket Calendar (`calendar:write`, `calendar:read`), Pocket Contacts (`contacts:read`, `contacts:write`), Pocket Notes (`notes:write`), Pocket Money (`transactions:write`, `accounts:read`), the Immich service (photos) and Pocket AI. Events: `mail.received` (new mail in an inbox: sender, subject, tab), `mail.sent`.
