package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// ---- senders ----

// GetSender loads what a person decided about a sender (zero value when nothing).
func (s *Store) GetSender(ctx context.Context, uid int64, addr string) Sender {
	sd := Sender{UserID: uid, Addr: strings.ToLower(addr)}
	var img, vip, muted int
	_ = s.DB.QueryRowContext(ctx, `SELECT name, allow_images, vip, muted, unsubscribed_at FROM senders WHERE user_id = ? AND addr = ?`, uid, sd.Addr).
		Scan(&sd.Name, &img, &vip, &muted, &sd.UnsubscribedAt)
	sd.AllowImages, sd.VIP, sd.Muted = img == 1, vip == 1, muted == 1
	return sd
}

// PutSender saves what a person decided about a sender.
func (s *Store) PutSender(ctx context.Context, sd Sender) error {
	sd.Addr = strings.ToLower(strings.TrimSpace(sd.Addr))
	_, err := s.DB.ExecContext(ctx, `INSERT INTO senders (user_id, addr, name, allow_images, vip, muted, unsubscribed_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, addr) DO UPDATE SET name = CASE WHEN excluded.name != '' THEN excluded.name ELSE senders.name END, allow_images = excluded.allow_images,
		vip = excluded.vip, muted = excluded.muted, unsubscribed_at = excluded.unsubscribed_at, updated_at = excluded.updated_at`,
		sd.UserID, sd.Addr, sd.Name, boolInt(sd.AllowImages), boolInt(sd.VIP), boolInt(sd.Muted), sd.UnsubscribedAt, Now())
	return err
}

// ListSenders lists a person's senders that have a decision (vip, muted, images).
func (s *Store) ListSenders(ctx context.Context, uid int64) ([]Sender, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT addr, name, allow_images, vip, muted, unsubscribed_at FROM senders WHERE user_id = ? ORDER BY vip DESC, addr`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sender{}
	for rows.Next() {
		sd := Sender{UserID: uid}
		var img, vip, muted int
		if err := rows.Scan(&sd.Addr, &sd.Name, &img, &vip, &muted, &sd.UnsubscribedAt); err != nil {
			return nil, err
		}
		sd.AllowImages, sd.VIP, sd.Muted = img == 1, vip == 1, muted == 1
		out = append(out, sd)
	}
	return out, rows.Err()
}

// ---- rules ----

const ruleCols = `id, user_id, account_id, name, enabled, match, actions, hits, last_hit_at, sort, created_at`

func scanRule(sc interface{ Scan(...any) error }) (Rule, error) {
	var r Rule
	var acct sql.NullInt64
	var en int
	var match, actions string
	err := sc.Scan(&r.ID, &r.UserID, &acct, &r.Name, &en, &match, &actions, &r.Hits, &r.LastHitAt, &r.Sort, &r.CreatedAt)
	if acct.Valid {
		v := acct.Int64
		r.AccountID = &v
	}
	r.Enabled = en == 1
	_ = json.Unmarshal([]byte(match), &r.Match)
	_ = json.Unmarshal([]byte(actions), &r.Actions)
	return r, err
}

// ListRules lists a person's rules in order (enabledOnly for the engine).
func (s *Store) ListRules(ctx context.Context, uid int64, enabledOnly bool) ([]Rule, error) {
	q := `SELECT ` + ruleCols + ` FROM rules WHERE user_id = ?`
	if enabledOnly {
		q += ` AND enabled = 1`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY sort, id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRule loads a person's rule.
func (s *Store) GetRule(ctx context.Context, uid, id int64) (Rule, error) {
	r, err := scanRule(s.DB.QueryRowContext(ctx, `SELECT `+ruleCols+` FROM rules WHERE id = ? AND user_id = ?`, id, uid))
	return r, notFound(err)
}

// SaveRule creates (ID 0) or updates a person's rule.
func (s *Store) SaveRule(ctx context.Context, r *Rule) error {
	m, _ := json.Marshal(r.Match)
	a, _ := json.Marshal(r.Actions)
	if r.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO rules (user_id, account_id, name, enabled, match, actions, sort) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			r.UserID, nullI(r.AccountID), r.Name, boolInt(r.Enabled), string(m), string(a), r.Sort)
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE rules SET account_id = ?, name = ?, enabled = ?, match = ?, actions = ?, sort = ? WHERE id = ? AND user_id = ?`,
		nullI(r.AccountID), r.Name, boolInt(r.Enabled), string(m), string(a), r.Sort, r.ID, r.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RuleHit counts a rule that matched.
func (s *Store) RuleHit(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE rules SET hits = hits + 1, last_hit_at = ? WHERE id = ?`, Now(), id)
	return err
}

// DeleteRule removes a person's rule.
func (s *Store) DeleteRule(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- templates ----

// ListTemplates lists a person's reusable replies.
func (s *Store) ListTemplates(ctx context.Context, uid int64) ([]Template, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, user_id, name, subject, body_html, created_at, updated_at FROM templates WHERE user_id = ? ORDER BY lower(name)`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Template{}
	for rows.Next() {
		var t Template
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Subject, &t.BodyHTML, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SaveTemplate creates (ID 0) or updates a person's template.
func (s *Store) SaveTemplate(ctx context.Context, t *Template) error {
	if t.ID == 0 {
		res, err := s.DB.ExecContext(ctx, `INSERT INTO templates (user_id, name, subject, body_html) VALUES (?, ?, ?, ?)`, t.UserID, t.Name, t.Subject, t.BodyHTML)
		if err != nil {
			return err
		}
		t.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE templates SET name = ?, subject = ?, body_html = ?, updated_at = ? WHERE id = ? AND user_id = ?`, t.Name, t.Subject, t.BodyHTML, Now(), t.ID, t.UserID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTemplate removes a person's template.
func (s *Store) DeleteTemplate(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM templates WHERE id = ? AND user_id = ?`, id, uid)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- what was made from a message in other apps ----

// AddAppLink records something made from a message in another Pocket app.
func (s *Store) AddAppLink(ctx context.Context, l *AppLink) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO app_links (user_id, message_id, thread_key, app, kind, label, item_id, item_url) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		l.UserID, nullI(l.MessageID), l.ThreadKey, l.App, l.Kind, l.Label, l.ItemID, l.ItemURL)
	if err != nil {
		return err
	}
	l.ID, _ = res.LastInsertId()
	l.CreatedAt = Now()
	return nil
}

// AppLinksForThread lists what was made from a conversation's messages.
func (s *Store) AppLinksForThread(ctx context.Context, uid int64, threadKey string, messageIDs []int64) ([]AppLink, error) {
	q := `SELECT id, message_id, thread_key, app, kind, label, item_id, item_url, created_at FROM app_links WHERE user_id = ? AND (thread_key = ?`
	args := []any{uid, threadKey}
	if len(messageIDs) > 0 {
		q += ` OR message_id IN (` + strings.TrimSuffix(strings.Repeat("?,", len(messageIDs)), ",") + `)`
		for _, id := range messageIDs {
			args = append(args, id)
		}
	}
	rows, err := s.DB.QueryContext(ctx, q+`) ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AppLink{}
	for rows.Next() {
		var l AppLink
		var mid sql.NullInt64
		if err := rows.Scan(&l.ID, &mid, &l.ThreadKey, &l.App, &l.Kind, &l.Label, &l.ItemID, &l.ItemURL, &l.CreatedAt); err != nil {
			return nil, err
		}
		if mid.Valid {
			v := mid.Int64
			l.MessageID = &v
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteAppLink forgets a link (the item is not touched).
func (s *Store) DeleteAppLink(ctx context.Context, uid, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM app_links WHERE id = ? AND user_id = ?`, id, uid)
	return err
}
