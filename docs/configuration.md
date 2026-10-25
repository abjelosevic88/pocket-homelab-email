# Configuration

Environment variables; every one has a default. Personal settings (goals, sounds, reminders, connections, notifications) live in the UI per person — the `*_URL`/`*_TOKEN`/`OPENAI_*`/`NOTIFY_*` values below are copied once into the first admin's settings.

| Variable | Default | What |
|---|---|---|
| `PORT`, `HOST` | `8080`, `0.0.0.0` | Listen address |
| `BASE_URL` | – | Public URL (links in pushes, the OIDC callback) |
| `DATA_DIR` | `./data` | Database, Core token, backups |
| `DB_PATH` | `$DATA_DIR/pocket-mail.db` | SQLite file |
| `MAX_BODY_BYTES` | `67108864` | Largest JSON request, e.g. an export being imported (64 MB) |
| `TZ` | – | Time zone for reminders and for practices sent without an offset |
| `AUTH_MODE` | `local` | `local` (accounts), `proxy` (trust `AUTH_PROXY_HEADER`/`AUTH_PROXY_EMAIL_HEADER`), `none` |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD`, `ADMIN_NAME` | – | Seed the first admin while the database is empty |
| `SESSION_TTL`, `SECURE_COOKIES` | `720h`, `false` | Set `SECURE_COOKIES=true` behind HTTPS |
| `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `OIDC_NAME`, `OIDC_AUTO_CREATE` | – | Sign in with an OpenID Connect provider (not needed with Pocket Core) |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | – | Apprise (`…/notify/key`), ntfy topic URL or JSON webhook; format `apprise`/`ntfy`/`json` |
| `HABITS_URL`, `HABITS_TOKEN` | – | Pocket Habits, direct connection (or use Pocket Core) |
| `HEALTH_URL`, `HEALTH_TOKEN` | – | Pocket Health, direct connection: mindful minutes in, heart rate variability, resting heart rate and sleep back (or use Pocket Core) |
| `NAVIDROME_URL`, `NAVIDROME_USER`, `NAVIDROME_PASSWORD` | – | Background music from Navidrome playlists (or your Navidrome login in Pocket Core) |
| `OPENAI_API_KEY`, `OPENAI_MODEL`, `OPENAI_BASE_URL` | – | The reflection and questions without Pocket AI |
| `CORE_URL`, `CORE_REGISTRATION_KEY`, `CORE_APP_URL` | – | Pocket Core |
| `TRUST_PROXY`, `METRICS_ENABLED`, `LOG_LEVEL`, `LOG_FORMAT` | `true`, `true`, `info`, `text` | |
| `DEMO_DATA` | `false` | Seed four months of plausible practice into the owner account (screenshots, trying it out) |

Commands: `pocket-mail backup [-o FILE]`, `pocket-mail reset-password [-email] [-password]`, `pocket-mail version`.
