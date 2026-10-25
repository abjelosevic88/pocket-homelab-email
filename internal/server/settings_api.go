package server

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-email/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-email/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- settings ----

func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 8 {
		return "••••"
	}
	return s[:4] + "••••" + s[len(s)-4:]
}

// publicSettings is what one person sees: their settings with the secrets
// masked and, per Pocket app, the mode in effect (<app>_mode), whether it
// works and how (<app>_via: core | direct | "") and why not (<app>_detail).
// Admins also see the box-wide OAuth clients (masked).
func (s *Server) publicSettings(ctx context.Context, u *store.User, st store.Settings) map[string]any {
	out := map[string]any{
		"core_configured": s.core.Configured(), "core_registered": s.coreStatus()["registered"],
		"title": st.Title, "date_format": st.DateFormat, "time_format": st.TimeFormat, "week_start": st.WeekStart,
		"density": st.Density, "conversations": st.Conversations, "reading_pane": st.ReadingPane, "mark_read_after": st.MarkReadAfter,
		"remote_images": st.RemoteImages, "inbox_categories": st.InboxCategories, "show_preview": st.ShowPreview, "after_archive": st.AfterArchive,
		"undo_send_seconds": st.UndoSendSeconds, "default_account": st.DefaultAccount, "reply_all": st.ReplyAll, "compose_font": st.ComposeFont,
		"notify_new": st.NotifyNew, "quiet_from": st.QuietFrom, "quiet_to": st.QuietTo,
		"notify_url": st.NotifyURL, "notify_format": st.NotifyFormat, "notify_configured": st.NotifyURL != "",
		"immich_url": st.ImmichURL, "immich_key": mask(st.ImmichKey), "immich_mode": s.connMode(st.ImmichMode, st.ImmichKey),
		"ai_provider": st.AIProvider, "ai_pocket_model": st.AIPocketModel, "openai_api_key": mask(st.OpenAIKey), "openai_model": st.OpenAIModel, "openai_base_url": st.OpenAIBaseURL,
		"ai_language": st.AILanguage, "ai_digest": st.AIDigest,
		"setup_complete": st.SetupComplete, "base_url": s.cfg.BaseURL,
		"oauth_google": s.oauthReady(ctx, "google"), "oauth_microsoft": s.oauthReady(ctx, "microsoft"),
		"oauth_redirect_google": s.oauthRedirect("google"), "oauth_redirect_microsoft": s.oauthRedirect("microsoft"),
	}
	if u != nil && u.Role == "admin" {
		out["google_client_id"], out["google_client_secret"] = st.GoogleClientID, mask(st.GoogleClientSecret)
		out["microsoft_client_id"], out["microsoft_client_secret"], out["microsoft_tenant"] = st.MicrosoftClientID, mask(st.MicrosoftClientSecret), st.MicrosoftTenant
		out["oauth_from_env"] = s.cfg.GoogleClientID != "" || s.cfg.MicrosoftClientID != ""
	}
	for _, a := range pocketApps {
		mode, via, detail := s.connStatus(ctx, u, st, a)
		out[a.slug+"_mode"], out[a.slug+"_via"], out[a.slug+"_detail"] = mode, via, detail
		out[a.slug+"_configured"] = via != ""
		c := st.App(a.slug)
		out[a.slug+"_url"], out[a.slug+"_token"], out[a.slug+"_public_url"] = c.URL, mask(c.Token), c.PublicURL
		public := c.PublicURL
		if public == "" && via != "" {
			public = s.resolveSibling(ctx, a.slug)
		}
		out[a.slug+"_open_url"] = public
	}
	return out
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.GetSettings(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), userFrom(r.Context()), st))
}

// handlePutSettings saves the caller's personal settings. The box-wide title
// and OAuth clients are only taken from an admin. Per Pocket app the keys
// are <slug>_mode, <slug>_url, <slug>_token and <slug>_public_url.
func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	me := userFrom(r.Context())
	isAdmin := me.Role == "admin"
	cur, err := s.store.GetSettings(r.Context(), me.ID)
	if err != nil {
		s.fail(w, err, "settings")
		return
	}
	b, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var in store.Settings
	var raw map[string]any
	if err := jsonUnmarshal(b, &raw); err != nil || jsonUnmarshal(b, &in) != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	keys := map[string]bool{}
	for k := range raw {
		keys[k] = true
	}
	str := func(k string) string { v, _ := raw[k].(string); return v }
	bad := func(msg string) { writeErr(w, http.StatusBadRequest, msg) }
	box := false
	if keys["title"] && isAdmin {
		cur.Title = strings.TrimSpace(in.Title)
		if cur.Title == "" {
			cur.Title = store.DefaultTitle
		}
		box = true
	}
	secret := func(k string, dst *string, v string) {
		if keys[k] && !strings.Contains(v, "••") {
			*dst = strings.TrimSpace(v)
		}
	}
	url := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimRight(strings.TrimSpace(v), "/")
		}
	}
	text := func(k string, dst *string, v string) {
		if keys[k] {
			*dst = strings.TrimSpace(v)
		}
	}
	boolIn := func(k string, dst *bool, v bool) {
		if keys[k] {
			*dst = v
		}
	}
	intIn := func(k string, dst *int, v, lo, hi int) bool {
		if !keys[k] {
			return true
		}
		if v < lo || v > hi {
			bad(fmt.Sprintf("%s is %d to %d", k, lo, hi))
			return false
		}
		*dst = v
		return true
	}
	pick := func(k string, dst *string, v string, allowed ...string) bool {
		if !keys[k] {
			return true
		}
		for _, a := range allowed {
			if v == a {
				*dst = v
				return true
			}
		}
		bad(fmt.Sprintf("%s must be one of %s", k, strings.Join(allowed, ", ")))
		return false
	}
	if isAdmin {
		for _, k := range []string{"google_client_id", "google_client_secret", "microsoft_client_id", "microsoft_client_secret", "microsoft_tenant"} {
			if keys[k] {
				box = true
			}
		}
		text("google_client_id", &cur.GoogleClientID, in.GoogleClientID)
		secret("google_client_secret", &cur.GoogleClientSecret, in.GoogleClientSecret)
		text("microsoft_client_id", &cur.MicrosoftClientID, in.MicrosoftClientID)
		secret("microsoft_client_secret", &cur.MicrosoftClientSecret, in.MicrosoftClientSecret)
		text("microsoft_tenant", &cur.MicrosoftTenant, in.MicrosoftTenant)
	}
	text("date_format", &cur.DateFormat, in.DateFormat)
	text("time_format", &cur.TimeFormat, in.TimeFormat)
	if keys["week_start"] && (in.WeekStart == 1 || in.WeekStart == 7 || in.WeekStart == 6) {
		cur.WeekStart = in.WeekStart
	}
	if !pick("density", &cur.Density, in.Density, "comfortable", "compact") ||
		!pick("reading_pane", &cur.ReadingPane, in.ReadingPane, "right", "off") ||
		!pick("remote_images", &cur.RemoteImages, in.RemoteImages, "ask", "always", "never") ||
		!pick("after_archive", &cur.AfterArchive, in.AfterArchive, "next", "previous", "list") ||
		!pick("notify_new", &cur.NotifyNew, in.NotifyNew, "off", "people", "vip", "all") ||
		!intIn("mark_read_after", &cur.MarkReadAfter, in.MarkReadAfter, -1, 60) ||
		!intIn("undo_send_seconds", &cur.UndoSendSeconds, in.UndoSendSeconds, 0, 60) ||
		!intIn("quiet_from", &cur.QuietFrom, in.QuietFrom, 0, 23) || !intIn("quiet_to", &cur.QuietTo, in.QuietTo, 0, 23) {
		return
	}
	boolIn("conversations", &cur.Conversations, in.Conversations)
	boolIn("inbox_categories", &cur.InboxCategories, in.InboxCategories)
	boolIn("show_preview", &cur.ShowPreview, in.ShowPreview)
	boolIn("reply_all", &cur.ReplyAll, in.ReplyAll)
	text("compose_font", &cur.ComposeFont, in.ComposeFont)
	if keys["default_account"] {
		if in.DefaultAccount != 0 {
			if _, err := s.store.GetAccount(r.Context(), me.ID, in.DefaultAccount); err != nil {
				bad("unknown account")
				return
			}
		}
		cur.DefaultAccount = in.DefaultAccount
	}
	text("notify_url", &cur.NotifyURL, in.NotifyURL)
	if keys["notify_format"] && in.NotifyFormat != "" {
		cur.NotifyFormat = in.NotifyFormat
	}
	url("immich_url", &cur.ImmichURL, in.ImmichURL)
	secret("immich_key", &cur.ImmichKey, in.ImmichKey)
	if keys["ai_provider"] {
		if !validAIProvider(in.AIProvider) {
			bad("AI is automatic, Pocket AI or OpenAI")
			return
		}
		cur.AIProvider = in.AIProvider
	}
	if keys["ai_pocket_model"] {
		if !validPocketModel(in.AIPocketModel) {
			bad("unknown model")
			return
		}
		cur.AIPocketModel = in.AIPocketModel
	}
	secret("openai_api_key", &cur.OpenAIKey, in.OpenAIKey)
	text("openai_model", &cur.OpenAIModel, in.OpenAIModel)
	url("openai_base_url", &cur.OpenAIBaseURL, in.OpenAIBaseURL)
	text("ai_language", &cur.AILanguage, in.AILanguage)
	boolIn("ai_digest", &cur.AIDigest, in.AIDigest)
	validMode := func(v string) bool { return v == modeCore || v == modeDirect || v == modeOff }
	if keys["immich_mode"] && validMode(str("immich_mode")) {
		cur.ImmichMode = str("immich_mode")
	}
	for _, a := range pocketApps {
		c := cur.App(a.slug)
		if k := a.slug + "_mode"; keys[k] && validMode(str(k)) {
			c.Mode = str(k)
		}
		url(a.slug+"_url", &c.URL, str(a.slug+"_url"))
		secret(a.slug+"_token", &c.Token, str(a.slug+"_token"))
		url(a.slug+"_public_url", &c.PublicURL, str(a.slug+"_public_url"))
		cur.SetApp(a.slug, c)
	}
	if err := s.store.SaveUserSettings(r.Context(), me.ID, cur); err != nil {
		s.fail(w, err, "save settings")
		return
	}
	if isAdmin && box {
		if err := s.store.SaveBoxSettings(r.Context(), cur); err != nil {
			s.fail(w, err, "save settings")
			return
		}
	}
	for k := range keys {
		if modeKey(k) {
			s.reportModesSoon()
			break
		}
	}
	writeJSON(w, http.StatusOK, s.publicSettings(r.Context(), me, cur))
}

// handleIntegrationTest checks a connection with the caller's own saved
// settings (or the URL/key typed into the form).
func (s *Server) handleIntegrationTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Which  string `json:"which"`
		URL    string `json:"url"`
		Key    string `json:"key"`
		Format string `json:"format"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	st, _ := s.store.GetSettings(r.Context(), uid)
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	keyOr := func(saved string) string {
		if strings.Contains(in.Key, "••") || in.Key == "" {
			return saved
		}
		return in.Key
	}
	urlOr := func(saved string) string {
		if strings.TrimSpace(in.URL) == "" {
			return saved
		}
		return strings.TrimRight(strings.TrimSpace(in.URL), "/")
	}
	var msg string
	var err error
	if a, ok := appBySlug(in.Which); ok {
		c := st.App(a.slug)
		typed := in.Key != "" && !strings.Contains(in.Key, "••")
		var conn *siblingConn
		if tok := keyOr(c.Token); tok != "" && (typed || s.connMode(c.Mode, c.Token) == modeDirect) {
			base := urlOr(c.URL)
			if base == "" {
				base = s.resolveSibling(ctx, a.slug)
			}
			conn = &siblingConn{client: pocket.New(base, tok, 15*time.Second), via: modeDirect}
		} else {
			conn, err = s.clients(ctx, uid).conn(ctx, a, a.scopes...)
		}
		if err == nil {
			how := "direct connection"
			if conn.via == modeCore {
				how = "through Pocket Core"
			}
			var v string
			if v, err = conn.client.Health(ctx); err == nil {
				msg = fmt.Sprintf("%s %s, %s", a.name, v, how)
			}
		}
	} else if in.Which == "immich" {
		var im *immichConn
		if im, err = s.immichFor(ctx, uid, urlOr(st.ImmichURL), keyOr(st.ImmichKey)); err == nil {
			msg, err = im.ping(ctx)
		}
	} else if in.Which == "openai" {
		c := ai.New(urlOr(st.OpenAIBaseURL), keyOr(st.OpenAIKey), st.OpenAIModel, 20*time.Second)
		var ids []string
		if ids, err = c.Models(ctx); err == nil {
			msg = fmt.Sprintf("The key works (%d models)", len(ids))
		}
	} else if in.Which == "notify" {
		f := in.Format
		if f == "" {
			f = st.NotifyFormat
		}
		n := notify.New(urlOr(st.NotifyURL), f)
		err = n.Send(ctx, notify.Message{Title: "Pocket Mail", Body: "Notifications work. New mail from people will arrive here.", Priority: "info", Tags: []string{"envelope"}})
		msg = "Test message sent via " + n.Format
	} else {
		writeErr(w, http.StatusBadRequest, "unknown integration")
		return
	}
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

func (s *Server) handleNotifyTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	if err := n.Send(ctx, notify.Message{Title: "Pocket Mail", Body: "Notifications work.", Priority: "info", Tags: []string{"envelope"}}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- system / backup / export ----

func (s *Server) handleSystemInfo(w http.ResponseWriter, r *http.Request) {
	var size int64
	if fi, err := os.Stat(s.cfg.DBPath); err == nil {
		size = fi.Size()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH,
		"core":      s.coreStatus(),
		"auth_mode": s.cfg.AuthMode, "data_dir": s.cfg.DataDir, "db_path": s.cfg.DBPath, "db_bytes": size,
		"uptime": time.Since(s.started).Round(time.Second).String(), "timezone": time.Now().Location().String(),
		"counts": s.store.CountsFor(r.Context(), actorID(r.Context())), "saved": s.saved.Load(),
	})
}

// handleBackup downloads the whole database, an admin-only box operation.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	dir := filepath.Join(s.cfg.DataDir, "backups")
	name := fmt.Sprintf("pocket-mail-%s.db", time.Now().Format("20060102-150405"))
	dst := filepath.Join(dir, name)
	if err := s.store.Backup(r.Context(), dst); err != nil {
		s.fail(w, err, "backup")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, dst)
}

// exportDoc is the JSON export of one person's setup: accounts (without
// passwords), signatures, rules, templates and sender decisions. The mail
// itself stays on the mail servers (download any message as .eml).
type exportDoc struct {
	App       string           `json:"app"`
	Version   string           `json:"version"`
	Exported  string           `json:"exported_at"`
	Accounts  []store.Account  `json:"accounts"`
	Rules     []store.Rule     `json:"rules"`
	Templates []store.Template `json:"templates"`
	Senders   []store.Sender   `json:"senders"`
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-mail", Version: Version, Exported: store.Now()}
	doc.Accounts, _ = s.store.ListAccounts(ctx, uid)
	doc.Rules, _ = s.store.ListRules(ctx, uid, false)
	doc.Templates, _ = s.store.ListTemplates(ctx, uid)
	doc.Senders, _ = s.store.ListSenders(ctx, uid)
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-mail-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes the people you correspond with (an address book from your mail).
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.store.Correspondents(ctx, actorID(ctx), "", 5000)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-mail-people.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"name", "email", "messages", "last"})
	for _, c := range list {
		_ = cw.Write([]string{c.Name, c.Addr, fmt.Sprint(c.Count), c.Last})
	}
	cw.Flush()
}

// handleImportJSON merges an export into the caller's own setup. Accounts
// already here (same address) are skipped and come without passwords (sign
// in again); rules and templates with the same name are skipped, so
// importing the same file twice adds nothing.
func (s *Server) handleImportJSON(w http.ResponseWriter, r *http.Request) {
	var doc exportDoc
	if err := decode(r, &doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if doc.App != "" && doc.App != "pocket-mail" {
		writeErr(w, http.StatusBadRequest, "this is not a Pocket Mail export")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	counts := map[string]int{}
	for _, a := range doc.Accounts {
		if a.Email == "" || a.Provider == "demo" {
			continue
		}
		if _, err := s.store.AccountByEmail(ctx, uid, a.Email); err == nil {
			counts["skipped"]++
			continue
		}
		a.ID, a.UserID, a.Secret, a.AccessToken, a.TokenExpiry = 0, uid, "", "", ""
		a.SyncState, a.SyncError, a.SyncEnabled = "auth", "Sign in again: passwords are not part of an export.", true
		if err := s.store.CreateAccount(ctx, &a); err == nil {
			_ = s.store.SetAccountSync(ctx, a.ID, "auth", a.SyncError)
			counts["accounts"]++
		}
	}
	names := func(list []string) map[string]bool {
		m := map[string]bool{}
		for _, n := range list {
			m[strings.ToLower(n)] = true
		}
		return m
	}
	var have []string
	if rs, err := s.store.ListRules(ctx, uid, false); err == nil {
		for _, ru := range rs {
			have = append(have, ru.Name)
		}
	}
	haveRules := names(have)
	for _, ru := range doc.Rules {
		if haveRules[strings.ToLower(ru.Name)] {
			continue
		}
		ru.ID, ru.UserID, ru.AccountID, ru.Hits, ru.LastHitAt = 0, uid, nil, 0, ""
		if err := s.store.SaveRule(ctx, &ru); err == nil {
			counts["rules"]++
		}
	}
	have = nil
	if ts, err := s.store.ListTemplates(ctx, uid); err == nil {
		for _, t := range ts {
			have = append(have, t.Name)
		}
	}
	haveTpl := names(have)
	for _, t := range doc.Templates {
		if haveTpl[strings.ToLower(t.Name)] {
			continue
		}
		t.ID, t.UserID = 0, uid
		if err := s.store.SaveTemplate(ctx, &t); err == nil {
			counts["templates"]++
		}
	}
	for _, sd := range doc.Senders {
		sd.UserID = uid
		if sd.Addr != "" && s.store.PutSender(ctx, sd) == nil {
			counts["senders"]++
		}
	}
	writeJSON(w, http.StatusOK, counts)
}
