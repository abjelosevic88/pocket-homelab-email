# Backup

Keep two things:

- the database `DATA_DIR/pocket-mail.db` (WAL mode). Take a consistent copy with `pocket-mail backup` (writes `DATA_DIR/backups/pocket-mail-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file.
- `DATA_DIR/secret.key`, which seals the mail accounts' passwords and sign-in tokens. Without it the accounts stay but everyone has to sign in to them again.

`DATA_DIR/core.token*` is the Pocket Core registration.

The mail itself lives on your mail providers; the database holds the recent months for speed and search, your drafts, scheduled messages, rules, templates and settings.

Restore: stop the app, replace `pocket-mail.db` (remove `-wal`/`-shm` files) and `secret.key`, start.

Every person can also export their own setup as JSON (`/api/v1/export.json`: accounts without passwords, rules, templates, sender choices; re-importable) and the people they write with as CSV.
