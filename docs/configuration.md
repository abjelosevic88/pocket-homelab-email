# Configuration

Environment variables; every one has a default. Personal settings (reading, writing, notifications, connections, AI) live in the UI per person — the `OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings. Mail accounts are added in the UI (Settings → Accounts); their passwords and tokens are sealed with `DATA_DIR/secret.key`.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (OAuth and OIDC callbacks, links in notifications) |
| `DATA_DIR` | `./data` | Database, `secret.key`, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-mail.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `TZ` | – | Time zone for snooze, send later and quiet hours |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in to Pocket Mail with an OpenID Connect provider (not needed with Pocket Core) |
| `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` | – | "Sign in with Google" for Gmail and Google Workspace accounts (see below). Admins can also enter them under Settings → Accounts |
| `MICROSOFT_CLIENT_ID`, `MICROSOFT_CLIENT_SECRET`, `MICROSOFT_TENANT` | –, –, `common` | "Sign in with Microsoft" for Outlook.com and Microsoft 365 |
| `SYNC_INTERVAL` | `5m` | Full check of every account; new inbox mail arrives at once in between (IMAP IDLE) |
| `MAX_ATTACHMENT_MB` | `25` | Largest attachment (and all attachments of one message together) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | AI (summaries, replies, writing help) without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `USER_AGENT` | `PocketMail/1.0 (…)` | Sent with one-click unsubscribe requests and provider lookups |
| `DEMO_DATA` | `false` | Two demo accounts with four months of believable mail in the owner's account (screenshots, trying it out; never touches a network) |

Commands: `pocket-mail backup [-o FILE]`, `pocket-mail reset-password [-email] [-password]`, `pocket-mail version`.

## Mail providers

| Provider | How to sign in |
|---|---|
| Gmail / Google Workspace | "Sign in with Google" (needs the Google client below), or an [app password](https://myaccount.google.com/apppasswords) (2-Step Verification must be on) |
| iCloud Mail | An app-specific password from [account.apple.com](https://account.apple.com) → Sign-In and Security |
| Outlook.com, Hotmail, Microsoft 365 | "Sign in with Microsoft" (passwords are no longer accepted by Microsoft) |
| Yahoo, AOL, Fastmail, Zoho, GMX | An app password from the provider's security settings |
| Proton Mail | Through Proton Mail Bridge running next to Pocket Mail |
| Any other | The address and password; servers are found automatically (Thunderbird's provider database, then the domain's MX records) and can be changed under Advanced |

### Google sign-in

1. In [Google Cloud Console](https://console.cloud.google.com/) create a project, enable the **Gmail API**, and set up the OAuth consent screen (External, add the scope `https://mail.google.com/`, add yourself as a test user — or publish the app so sign-ins don't expire after 7 days).
2. Credentials → Create OAuth client ID → **Web application**, authorized redirect URI `BASE_URL/api/v1/oauth/google/callback`.
3. Put the client ID and secret in `GOOGLE_CLIENT_ID`/`GOOGLE_CLIENT_SECRET` (or Settings → Accounts → Sign-in setup).

Google treats full mail access as a restricted scope: an unpublished app shows an "unverified app" warning (Advanced → continue) and works for up to 100 people, which is what a home server needs.

### Microsoft sign-in

1. In the [Entra admin center](https://entra.microsoft.com/) → App registrations → New registration, "Accounts in any organizational directory and personal Microsoft accounts", redirect URI (Web) `BASE_URL/api/v1/oauth/microsoft/callback`.
2. API permissions → Add → APIs my organization uses → **Office 365 Exchange Online** (or Microsoft Graph) → delegated `IMAP.AccessAsUser.All` and `SMTP.Send`, plus `offline_access`, `openid`, `email`, `profile`.
3. Certificates & secrets → New client secret. Put the IDs in `MICROSOFT_CLIENT_ID`/`MICROSOFT_CLIENT_SECRET` (`MICROSOFT_TENANT=common`).

Work accounts may need an administrator to allow the app, and SMTP sending (SMTP AUTH) switched on for the mailbox.
