# Backup

One SQLite file: `DATA_DIR/pocket-mail.db` (WAL mode). Take a consistent copy with `pocket-mail backup` (writes `DATA_DIR/backups/pocket-mail-<time>.db` with `VACUUM INTO`) or Settings → Backup & data → Download database (admin). Back up that copy, not the live file. `DATA_DIR/core.token*` is the Pocket Core registration.

Restore: stop the app, replace `pocket-mail.db` (remove `-wal`/`-shm` files), start.

Every person can also export their own data as JSON (`/api/v1/export.json`, re-importable) or the daily numbers as CSV.
