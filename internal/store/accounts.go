package store

import (
	"context"
	"database/sql"
	"strings"
)

// ---- accounts ----

const accountCols = `id, user_id, name, email, label, provider, auth, oauth_provider, username,
	imap_host, imap_port, imap_security, smtp_host, smtp_port, smtp_security, secret, access_token, token_expiry,
	color, signature, sort, sync_enabled, sync_days, saves_sent, sync_state, sync_error, last_sync_at, created_at, updated_at`

func scanAccount(sc interface{ Scan(...any) error }) (Account, error) {
	var a Account
	var syncOn, saves int
	err := sc.Scan(&a.ID, &a.UserID, &a.Name, &a.Email, &a.Label, &a.Provider, &a.Auth, &a.OAuthProvider, &a.Username,
		&a.IMAPHost, &a.IMAPPort, &a.IMAPSecurity, &a.SMTPHost, &a.SMTPPort, &a.SMTPSecurity, &a.Secret, &a.AccessToken, &a.TokenExpiry,
		&a.Color, &a.Signature, &a.Sort, &syncOn, &a.SyncDays, &saves, &a.SyncState, &a.SyncError, &a.LastSyncAt, &a.CreatedAt, &a.UpdatedAt)
	a.SyncEnabled, a.SavesSent = syncOn == 1, saves == 1
	return a, err
}

func (s *Store) queryAccounts(ctx context.Context, q string, args ...any) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+accountCols+` FROM accounts `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAccounts lists one person's accounts in their order.
func (s *Store) ListAccounts(ctx context.Context, uid int64) ([]Account, error) {
	return s.queryAccounts(ctx, `WHERE user_id = ? ORDER BY sort, id`, uid)
}

// AllAccounts lists every account on the box (for the sync engine only).
func (s *Store) AllAccounts(ctx context.Context) ([]Account, error) {
	return s.queryAccounts(ctx, `ORDER BY id`)
}

// GetAccount loads one account of a person (uid 0 = whoever owns it; engine only).
func (s *Store) GetAccount(ctx context.Context, uid, id int64) (Account, error) {
	q := `SELECT ` + accountCols + ` FROM accounts WHERE id = ?`
	args := []any{id}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	a, err := scanAccount(s.DB.QueryRowContext(ctx, q, args...))
	return a, notFound(err)
}

// AccountByEmail finds a person's account with that address.
func (s *Store) AccountByEmail(ctx context.Context, uid int64, email string) (Account, error) {
	a, err := scanAccount(s.DB.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE user_id = ? AND lower(email) = lower(?)`, uid, email))
	return a, notFound(err)
}

func (a *Account) normalize() {
	a.Email = strings.TrimSpace(a.Email)
	if a.Provider == "" {
		a.Provider = "custom"
	}
	if a.Auth == "" {
		a.Auth = "password"
	}
	if a.IMAPSecurity == "" {
		a.IMAPSecurity = "ssl"
	}
	if a.SMTPSecurity == "" {
		a.SMTPSecurity = "ssl"
	}
	if a.SyncDays <= 0 {
		a.SyncDays = 90
	}
	if a.SyncState == "" {
		a.SyncState = "new"
	}
}

// CreateAccount adds an account (UserID set by the caller).
func (s *Store) CreateAccount(ctx context.Context, a *Account) error {
	a.normalize()
	if a.Sort == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) + 1 FROM accounts WHERE user_id = ?`, a.UserID).Scan(&a.Sort)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO accounts (user_id, name, email, label, provider, auth, oauth_provider, username,
		imap_host, imap_port, imap_security, smtp_host, smtp_port, smtp_security, secret, access_token, token_expiry,
		color, signature, sort, sync_enabled, sync_days, saves_sent, sync_state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.UserID, a.Name, a.Email, a.Label, a.Provider, a.Auth, a.OAuthProvider, a.Username,
		a.IMAPHost, a.IMAPPort, a.IMAPSecurity, a.SMTPHost, a.SMTPPort, a.SMTPSecurity, a.Secret, a.AccessToken, a.TokenExpiry,
		a.Color, a.Signature, a.Sort, boolInt(a.SyncEnabled), a.SyncDays, boolInt(a.SavesSent), a.SyncState)
	if err != nil {
		return conflict(err)
	}
	a.ID, _ = res.LastInsertId()
	return nil
}

// UpdateAccount saves every editable field (secrets included) of a person's account.
func (s *Store) UpdateAccount(ctx context.Context, a *Account) error {
	a.normalize()
	res, err := s.DB.ExecContext(ctx, `UPDATE accounts SET name = ?, email = ?, label = ?, provider = ?, auth = ?, oauth_provider = ?, username = ?,
		imap_host = ?, imap_port = ?, imap_security = ?, smtp_host = ?, smtp_port = ?, smtp_security = ?, secret = ?, access_token = ?, token_expiry = ?,
		color = ?, signature = ?, sort = ?, sync_enabled = ?, sync_days = ?, saves_sent = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		a.Name, a.Email, a.Label, a.Provider, a.Auth, a.OAuthProvider, a.Username,
		a.IMAPHost, a.IMAPPort, a.IMAPSecurity, a.SMTPHost, a.SMTPPort, a.SMTPSecurity, a.Secret, a.AccessToken, a.TokenExpiry,
		a.Color, a.Signature, a.Sort, boolInt(a.SyncEnabled), a.SyncDays, boolInt(a.SavesSent), Now(), a.ID, a.UserID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAccountSync records how the last sync went (state new | syncing | ok | error | auth).
func (s *Store) SetAccountSync(ctx context.Context, id int64, state, msg string) error {
	q := `UPDATE accounts SET sync_state = ?, sync_error = ? WHERE id = ?`
	if state == "ok" {
		q = `UPDATE accounts SET sync_state = ?, sync_error = ?, last_sync_at = '` + Now() + `' WHERE id = ?`
	}
	_, err := s.DB.ExecContext(ctx, q, state, msg, id)
	return err
}

// SetAccountTokens stores refreshed OAuth tokens (already sealed). An empty
// refresh token keeps the old one.
func (s *Store) SetAccountTokens(ctx context.Context, id int64, access, expiry, refresh string) error {
	if refresh != "" {
		_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET access_token = ?, token_expiry = ?, secret = ? WHERE id = ?`, access, expiry, refresh, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE accounts SET access_token = ?, token_expiry = ? WHERE id = ?`, access, expiry, id)
	return err
}

// DeleteAccount removes an account and everything synced from it.
func (s *Store) DeleteAccount(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM accounts WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- folders ----

const folderCols = `id, user_id, account_id, name, display, delimiter, role, sync, selectable, uidvalidity, uidnext, highest_modseq, last_uid, oldest_uid, unread, total, last_sync_at`

func scanFolder(sc interface{ Scan(...any) error }) (Folder, error) {
	var f Folder
	var syncOn, sel int
	var uv, un, lu, ou int64
	var ms int64
	err := sc.Scan(&f.ID, &f.UserID, &f.AccountID, &f.Name, &f.Display, &f.Delimiter, &f.Role, &syncOn, &sel, &uv, &un, &ms, &lu, &ou, &f.Unread, &f.Total, &f.LastSyncAt)
	f.Sync, f.Selectable = syncOn == 1, sel == 1
	f.UIDValidity, f.UIDNext, f.HighestModSeq, f.LastUID, f.OldestUID = uint32(uv), uint32(un), uint64(ms), uint32(lu), uint32(ou)
	return f, err
}

func (s *Store) queryFolders(ctx context.Context, q string, args ...any) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+folderCols+` FROM folders `+q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// roleOrder sorts the well-known folders first.
const roleOrder = `CASE role WHEN 'inbox' THEN 0 WHEN 'flagged' THEN 1 WHEN 'drafts' THEN 2 WHEN 'sent' THEN 3 WHEN 'archive' THEN 4 WHEN 'all' THEN 5 WHEN 'junk' THEN 6 WHEN 'trash' THEN 7 WHEN 'important' THEN 8 ELSE 9 END`

// ListFolders lists a person's folders (accountID 0 = every account).
func (s *Store) ListFolders(ctx context.Context, uid, accountID int64) ([]Folder, error) {
	if accountID > 0 {
		return s.queryFolders(ctx, `WHERE user_id = ? AND account_id = ? ORDER BY `+roleOrder+`, lower(name)`, uid, accountID)
	}
	return s.queryFolders(ctx, `WHERE user_id = ? ORDER BY account_id, `+roleOrder+`, lower(name)`, uid)
}

// AccountFolders lists an account's folders (engine).
func (s *Store) AccountFolders(ctx context.Context, accountID int64) ([]Folder, error) {
	return s.queryFolders(ctx, `WHERE account_id = ? ORDER BY `+roleOrder+`, lower(name)`, accountID)
}

// GetFolder loads one folder of a person (uid 0 = any; engine only).
func (s *Store) GetFolder(ctx context.Context, uid, id int64) (Folder, error) {
	q := `SELECT ` + folderCols + ` FROM folders WHERE id = ?`
	args := []any{id}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	f, err := scanFolder(s.DB.QueryRowContext(ctx, q, args...))
	return f, notFound(err)
}

// FolderByName finds an account's folder by its IMAP name.
func (s *Store) FolderByName(ctx context.Context, accountID int64, name string) (Folder, error) {
	f, err := scanFolder(s.DB.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders WHERE account_id = ? AND name = ?`, accountID, name))
	return f, notFound(err)
}

// FolderByRole finds an account's folder with a role (inbox, sent, trash…).
func (s *Store) FolderByRole(ctx context.Context, accountID int64, role string) (Folder, error) {
	f, err := scanFolder(s.DB.QueryRowContext(ctx, `SELECT `+folderCols+` FROM folders WHERE account_id = ? AND role = ? ORDER BY id LIMIT 1`, accountID, role))
	return f, notFound(err)
}

// UpsertFolder adds a folder or updates its name parts, role and selectable
// flag; the sync position is kept. f.ID is set.
func (s *Store) UpsertFolder(ctx context.Context, f *Folder) error {
	if f.Delimiter == "" {
		f.Delimiter = "/"
	}
	err := s.DB.QueryRowContext(ctx, `INSERT INTO folders (user_id, account_id, name, display, delimiter, role, sync, selectable)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id, name) DO UPDATE SET display = excluded.display, delimiter = excluded.delimiter, role = excluded.role, selectable = excluded.selectable
		RETURNING id, sync`, f.UserID, f.AccountID, f.Name, f.Display, f.Delimiter, f.Role, boolInt(f.Sync), boolInt(f.Selectable)).Scan(&f.ID, &f.Sync)
	return err
}

// SaveFolderState stores where the sync of a folder got to.
func (s *Store) SaveFolderState(ctx context.Context, f *Folder) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE folders SET uidvalidity = ?, uidnext = ?, highest_modseq = ?, last_uid = ?, oldest_uid = ?, last_sync_at = ? WHERE id = ?`,
		int64(f.UIDValidity), int64(f.UIDNext), int64(f.HighestModSeq), int64(f.LastUID), int64(f.OldestUID), Now(), f.ID)
	return err
}

// SetFolderSync switches syncing of a person's folder on or off.
func (s *Store) SetFolderSync(ctx context.Context, uid, id int64, on bool) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE folders SET sync = ? WHERE id = ? AND user_id = ?`, boolInt(on), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFolder removes a folder that is gone from the server (with its messages).
func (s *Store) DeleteFolder(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM folders WHERE id = ?`, id)
	return err
}

// RefreshFolderCounts recounts a folder's unread and total messages from what is here.
func (s *Store) RefreshFolderCounts(ctx context.Context, folderID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE folders SET
		unread = (SELECT COUNT(1) FROM messages WHERE folder_id = folders.id AND seen = 0),
		total  = (SELECT COUNT(1) FROM messages WHERE folder_id = folders.id)
		WHERE id = ?`, folderID)
	return err
}

// nullStr maps "" to NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var _ = sql.ErrNoRows
