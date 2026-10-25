package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Queries the mail engine needs beyond the API's.

// GetOps loads a person's queued changes by id (missing ids are skipped).
func (s *Store) GetOps(ctx context.Context, uid int64, ids []int64) ([]PendingOp, error) {
	if len(ids) == 0 {
		return []PendingOp{}, nil
	}
	q := `SELECT id, user_id, account_id, op, folder, uid, message_id, args, run_after, attempts, error, created_at
		FROM pending_ops WHERE user_id = ? AND id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) ORDER BY id DESC`
	args := []any{uid}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingOp{}
	for rows.Next() {
		var op PendingOp
		var mid sql.NullInt64
		var a string
		if err := rows.Scan(&op.ID, &op.UserID, &op.AccountID, &op.Op, &op.Folder, &op.UID, &mid, &a, &op.RunAfter, &op.Attempts, &op.Error, &op.CreatedAt); err != nil {
			return nil, err
		}
		if mid.Valid {
			v := mid.Int64
			op.MessageID = &v
		}
		op.Args = json.RawMessage(a)
		out = append(out, op)
	}
	return out, rows.Err()
}

// UpdateOpSource points a message's later queued changes at where the
// message now really is on the server (after an earlier move landed).
func (s *Store) UpdateOpSource(ctx context.Context, messageID int64, folder string, uid int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE pending_ops SET folder = ?, uid = ? WHERE message_id = ? AND uid <= 0`, folder, uid, messageID)
	return err
}

// MaxUID is the highest UID stored for a folder (local demo moves).
func (s *Store) MaxUID(ctx context.Context, folderID int64) int64 {
	var n int64
	_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(uid), 0) FROM messages WHERE folder_id = ? AND uid > 0`, folderID).Scan(&n)
	return n
}

// MessageIDByUID finds the local id of a folder's message by UID (0 = not here).
func (s *Store) MessageIDByUID(ctx context.Context, folderID int64, uid int64) int64 {
	var id int64
	_ = s.DB.QueryRowContext(ctx, `SELECT id FROM messages WHERE folder_id = ? AND uid = ?`, folderID, uid).Scan(&id)
	return id
}

// HasPendingIn reports whether a folder holds messages still moving to the server.
func (s *Store) HasPendingIn(ctx context.Context, folderID int64) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM messages WHERE folder_id = ? AND uid <= 0`, folderID).Scan(&n)
	return n > 0
}

// SetFolderSyncDefault sets whether a folder syncs, without a person check (engine).
func (s *Store) SetFolderSyncDefault(ctx context.Context, id int64, on bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE folders SET sync = ? WHERE id = ?`, boolInt(on), id)
	return err
}
