package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// ---- drafts and the outbox ----

const draftCols = `id, user_id, account_id, kind, reply_to_id, to_json, cc_json, bcc_json, subject, body_html, status, send_at, sent_at, error, attempts, message_id, source, created_at, updated_at`

func scanDraft(sc interface{ Scan(...any) error }) (Draft, error) {
	var d Draft
	var reply sql.NullInt64
	var to, cc, bcc string
	err := sc.Scan(&d.ID, &d.UserID, &d.AccountID, &d.Kind, &reply, &to, &cc, &bcc, &d.Subject, &d.BodyHTML, &d.Status, &d.SendAt, &d.SentAt, &d.Error, &d.Attempts, &d.MessageID, &d.Source, &d.CreatedAt, &d.UpdatedAt)
	if reply.Valid {
		v := reply.Int64
		d.ReplyToID = &v
	}
	d.To, d.Cc, d.Bcc = addrs(to), addrs(cc), addrs(bcc)
	d.Files = []DraftFile{}
	return d, err
}

func (s *Store) queryDrafts(ctx context.Context, q string, args ...any) ([]Draft, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+draftCols+` FROM drafts `+q, args...)
	if err != nil {
		return nil, err
	}
	out := []Draft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, d)
	}
	rows.Close()
	for i := range out {
		out[i].Files, _ = s.DraftFiles(ctx, out[i].ID)
	}
	return out, nil
}

// ListDrafts lists a person's drafts with these statuses (none = every status), newest first.
func (s *Store) ListDrafts(ctx context.Context, uid int64, statuses ...string) ([]Draft, error) {
	q := `WHERE user_id = ?`
	args := []any{uid}
	if len(statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	return s.queryDrafts(ctx, q+` ORDER BY CASE WHEN send_at != '' THEN send_at ELSE updated_at END DESC LIMIT 500`, args...)
}

// GetDraft loads a person's draft with its files (uid 0 = any; engine only).
func (s *Store) GetDraft(ctx context.Context, uid, id int64) (Draft, error) {
	q := `SELECT ` + draftCols + ` FROM drafts WHERE id = ?`
	args := []any{id}
	if uid > 0 {
		q += ` AND user_id = ?`
		args = append(args, uid)
	}
	d, err := scanDraft(s.DB.QueryRowContext(ctx, q, args...))
	if err != nil {
		return d, notFound(err)
	}
	d.Files, _ = s.DraftFiles(ctx, d.ID)
	return d, nil
}

// CreateDraft stores a new draft (UserID, AccountID set by the caller).
func (s *Store) CreateDraft(ctx context.Context, d *Draft) error {
	if d.Kind == "" {
		d.Kind = "new"
	}
	if d.Status == "" {
		d.Status = "draft"
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO drafts (user_id, account_id, kind, reply_to_id, to_json, cc_json, bcc_json, subject, body_html, status, send_at, message_id, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.UserID, d.AccountID, d.Kind, nullI(d.ReplyToID), addrsJSON(d.To), addrsJSON(d.Cc), addrsJSON(d.Bcc), d.Subject, d.BodyHTML, d.Status, d.SendAt, d.MessageID, d.Source)
	if err != nil {
		return err
	}
	d.ID, _ = res.LastInsertId()
	got, err := s.GetDraft(ctx, d.UserID, d.ID)
	if err == nil {
		*d = got
	}
	return err
}

// UpdateDraft saves the editable parts of a person's draft (only while it is a draft or failed).
func (s *Store) UpdateDraft(ctx context.Context, d *Draft) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE drafts SET account_id = ?, to_json = ?, cc_json = ?, bcc_json = ?, subject = ?, body_html = ?, updated_at = ?
		WHERE id = ? AND user_id = ? AND status IN ('draft','failed')`,
		d.AccountID, addrsJSON(d.To), addrsJSON(d.Cc), addrsJSON(d.Bcc), d.Subject, d.BodyHTML, Now(), d.ID, d.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// QueueDraft puts a person's draft in the outbox to go at sendAt.
func (s *Store) QueueDraft(ctx context.Context, uid, id int64, sendAt, messageID string) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'queued', send_at = ?, message_id = CASE WHEN message_id = '' THEN ? ELSE message_id END, error = '', attempts = 0, updated_at = ?
		WHERE id = ? AND user_id = ? AND status IN ('draft','failed','queued')`, sendAt, messageID, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// UnqueueDraft takes a queued draft back (undo send, cancel a scheduled send).
// It fails with ErrConflict when it is already on its way.
func (s *Store) UnqueueDraft(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'draft', send_at = '', updated_at = ? WHERE id = ? AND user_id = ? AND status = 'queued'`, Now(), id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrConflict
	}
	return nil
}

// ClaimDueDrafts marks the queued drafts whose time has come as sending and returns them (engine).
func (s *Store) ClaimDueDrafts(ctx context.Context, now time.Time) ([]Draft, error) {
	ts := now.UTC().Format(time.RFC3339Nano)
	list, err := s.queryDrafts(ctx, `WHERE status = 'queued' AND send_at <= ? ORDER BY send_at LIMIT 20`, ts)
	if err != nil {
		return nil, err
	}
	out := list[:0]
	for _, d := range list {
		res, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'sending', attempts = attempts + 1 WHERE id = ? AND status = 'queued'`, d.ID)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			d.Status = "sending"
			d.Attempts++
			out = append(out, d)
		}
	}
	return out, nil
}

// FinishDraft records how sending went: sent, or failed (retry: back in the queue at retryAt).
func (s *Store) FinishDraft(ctx context.Context, id int64, sendErr string, retryAt string) error {
	switch {
	case sendErr == "":
		_, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'sent', sent_at = ?, error = '' WHERE id = ?`, Now(), id)
		return err
	case retryAt != "":
		_, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'queued', send_at = ?, error = ? WHERE id = ?`, retryAt, sendErr, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'failed', error = ? WHERE id = ?`, sendErr, id)
	return err
}

// ResetSending puts drafts stuck in "sending" (a restart mid-send) back in the queue.
func (s *Store) ResetSending(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE drafts SET status = 'queued' WHERE status = 'sending'`)
	return err
}

// PurgeSentDrafts forgets sent drafts older than the cutoff (the copy is in Sent).
func (s *Store) PurgeSentDrafts(ctx context.Context, before string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM drafts WHERE status = 'sent' AND sent_at < ?`, before)
	return err
}

// DeleteDraft removes a person's draft (not while it is being sent).
func (s *Store) DeleteDraft(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM drafts WHERE id = ? AND user_id = ? AND status != 'sending'`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DraftFiles lists a draft's files (without the data).
func (s *Store) DraftFiles(ctx context.Context, draftID int64) ([]DraftFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, draft_id, filename, mime, size FROM draft_files WHERE draft_id = ? ORDER BY id`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftFile{}
	for rows.Next() {
		var f DraftFile
		if err := rows.Scan(&f.ID, &f.UserID, &f.DraftID, &f.Filename, &f.MIME, &f.Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DraftFilesData loads a draft's files with their bytes (for sending).
func (s *Store) DraftFilesData(ctx context.Context, draftID int64) ([]DraftFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, draft_id, filename, mime, size, data FROM draft_files WHERE draft_id = ? ORDER BY id`, draftID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DraftFile{}
	for rows.Next() {
		var f DraftFile
		if err := rows.Scan(&f.ID, &f.UserID, &f.DraftID, &f.Filename, &f.MIME, &f.Size, &f.Data); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetDraftFile loads one of a person's draft files with its bytes.
func (s *Store) GetDraftFile(ctx context.Context, uid, id int64) (DraftFile, error) {
	var f DraftFile
	err := s.DB.QueryRowContext(ctx, `SELECT id, user_id, draft_id, filename, mime, size, data FROM draft_files WHERE id = ? AND user_id = ?`, id, uid).
		Scan(&f.ID, &f.UserID, &f.DraftID, &f.Filename, &f.MIME, &f.Size, &f.Data)
	return f, notFound(err)
}

// AddDraftFile attaches a file to a person's draft.
func (s *Store) AddDraftFile(ctx context.Context, f *DraftFile) error {
	if f.MIME == "" {
		f.MIME = "application/octet-stream"
	}
	f.Size = int64(len(f.Data))
	res, err := s.DB.ExecContext(ctx, `INSERT INTO draft_files (user_id, draft_id, filename, mime, size, data) SELECT ?, id, ?, ?, ?, ? FROM drafts WHERE id = ? AND user_id = ?`,
		f.UserID, f.Filename, f.MIME, f.Size, f.Data, f.DraftID, f.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	f.ID, _ = res.LastInsertId()
	return nil
}

// DeleteDraftFile removes a file from a person's draft.
func (s *Store) DeleteDraftFile(ctx context.Context, uid, draftID, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM draft_files WHERE id = ? AND draft_id = ? AND user_id = ?`, id, draftID, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- changes waiting for the server ----

// AddOp queues a change for the server (run after runAfter, for Undo).
func (s *Store) AddOp(ctx context.Context, op *PendingOp) error {
	if len(op.Args) == 0 {
		op.Args = json.RawMessage(`{}`)
	}
	if op.RunAfter == "" {
		op.RunAfter = Now()
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO pending_ops (user_id, account_id, op, folder, uid, message_id, args, run_after) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		op.UserID, op.AccountID, op.Op, op.Folder, op.UID, nullI(op.MessageID), string(op.Args), op.RunAfter)
	if err != nil {
		return err
	}
	op.ID, _ = res.LastInsertId()
	return nil
}

// DueOps lists an account's changes whose time has come, oldest first (engine).
func (s *Store) DueOps(ctx context.Context, accountID int64, now time.Time) ([]PendingOp, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, account_id, op, folder, uid, message_id, args, run_after, attempts, error, created_at
		FROM pending_ops WHERE account_id = ? AND run_after <= ? ORDER BY id LIMIT 200`, accountID, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PendingOp{}
	for rows.Next() {
		var op PendingOp
		var mid sql.NullInt64
		var args string
		if err := rows.Scan(&op.ID, &op.UserID, &op.AccountID, &op.Op, &op.Folder, &op.UID, &mid, &args, &op.RunAfter, &op.Attempts, &op.Error, &op.CreatedAt); err != nil {
			return nil, err
		}
		if mid.Valid {
			v := mid.Int64
			op.MessageID = &v
		}
		op.Args = json.RawMessage(args)
		out = append(out, op)
	}
	return out, rows.Err()
}

// AccountsWithDueOps lists the accounts that have changes to apply now (engine).
func (s *Store) AccountsWithDueOps(ctx context.Context, now time.Time) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT account_id FROM pending_ops WHERE run_after <= ?`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// DoneOp forgets an applied change.
func (s *Store) DoneOp(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM pending_ops WHERE id = ?`, id)
	return err
}

// RetryOp records a failed attempt; after 8 attempts the change is dropped.
func (s *Store) RetryOp(ctx context.Context, op PendingOp, msg string) error {
	if op.Attempts+1 >= 8 {
		return s.DoneOp(ctx, op.ID)
	}
	wait := time.Duration(1<<op.Attempts) * 30 * time.Second
	_, err := s.DB.ExecContext(ctx, `UPDATE pending_ops SET attempts = attempts + 1, error = ?, run_after = ? WHERE id = ?`, msg, time.Now().Add(wait).UTC().Format(time.RFC3339Nano), op.ID)
	return err
}

// CancelOps takes back a person's changes that have not reached the server yet; it returns how many.
func (s *Store) CancelOps(ctx context.Context, uid int64, ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		res, err := s.DB.ExecContext(ctx, `DELETE FROM pending_ops WHERE id = ? AND user_id = ? AND attempts = 0`, id, uid)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

// PendingMoves lists a message's queued moves (so a second move replaces the first).
func (s *Store) PendingForMessage(ctx context.Context, messageID int64) ([]PendingOp, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, op, folder, uid, args, attempts FROM pending_ops WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PendingOp
	for rows.Next() {
		var op PendingOp
		var args string
		if err := rows.Scan(&op.ID, &op.Op, &op.Folder, &op.UID, &args, &op.Attempts); err != nil {
			return nil, err
		}
		op.Args = json.RawMessage(args)
		out = append(out, op)
	}
	return out, rows.Err()
}
