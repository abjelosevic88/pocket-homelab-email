package server

import (
	"context"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- mail accounts ----

// accountView is an account as the UI sees it: no secrets, with its folders
// and how the sync is doing right now.
type accountView struct {
	store.Account
	Folders []store.Folder `json:"folders"`
	Status  pmail.Status   `json:"status"`
}

func (s *Server) accountView(ctx context.Context, a store.Account) accountView {
	fs, err := s.store.ListFolders(ctx, a.UserID, a.ID)
	if err != nil || fs == nil {
		fs = []store.Folder{}
	}
	return accountView{Account: a, Folders: fs, Status: s.mail.Status(a.ID)}
}

func (s *Server) accountViews(ctx context.Context, uid int64) ([]accountView, error) {
	list, err := s.store.ListAccounts(ctx, uid)
	if err != nil {
		return nil, err
	}
	out := make([]accountView, 0, len(list))
	for _, a := range list {
		out = append(out, s.accountView(ctx, a))
	}
	return out, nil
}

// presetView is a provider with whether its "Sign in with …" is ready on this box.
type presetView struct {
	pmail.Preset
	OAuthReady bool `json:"oauth_ready"`
}

func (s *Server) presetView(ctx context.Context, p pmail.Preset) presetView {
	return presetView{Preset: p, OAuthReady: p.OAuth != "" && s.oauthReady(ctx, p.OAuth)}
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	out := []presetView{}
	for _, p := range pmail.Providers() {
		out = append(out, s.presetView(r.Context(), p))
	}
	writeJSON(w, http.StatusOK, out)
}

func validEmail(addr string) bool {
	a, err := mail.ParseAddress(addr)
	return err == nil && a.Address == addr && strings.Contains(addr, "@")
}

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if !validEmail(email) {
		writeErr(w, http.StatusBadRequest, "enter a full email address, like name@example.com")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.presetView(ctx, pmail.Discover(ctx, email)))
}

// accountInput is what the add/edit forms send. Password is never echoed back.
type accountInput struct {
	Name         *string `json:"name"`
	Email        *string `json:"email"`
	Label        *string `json:"label"`
	Provider     *string `json:"provider"`
	Username     *string `json:"username"`
	Password     *string `json:"password"`
	IMAPHost     *string `json:"imap_host"`
	IMAPPort     *int    `json:"imap_port"`
	IMAPSecurity *string `json:"imap_security"`
	SMTPHost     *string `json:"smtp_host"`
	SMTPPort     *int    `json:"smtp_port"`
	SMTPSecurity *string `json:"smtp_security"`
	SavesSent    *bool   `json:"saves_sent"`
	Color        *string `json:"color"`
	Signature    *string `json:"signature"`
	SyncEnabled  *bool   `json:"sync_enabled"`
	SyncDays     *int    `json:"sync_days"`
	Sort         *int    `json:"sort"`
}

func validSecurity(v string) bool { return v == "ssl" || v == "starttls" || v == "none" }

// apply copies the given fields onto an account; it returns a plain message when one is wrong.
func (in accountInput) apply(a *store.Account) string {
	str := func(p *string) string { return strings.TrimSpace(*p) }
	if in.Name != nil {
		a.Name = str(in.Name)
	}
	if in.Email != nil {
		a.Email = str(in.Email)
		if !validEmail(a.Email) {
			return "enter a full email address, like name@example.com"
		}
	}
	if in.Label != nil {
		a.Label = str(in.Label)
	}
	if in.Provider != nil && str(in.Provider) != "" {
		a.Provider = str(in.Provider)
		if a.Provider == "demo" {
			return "unknown provider"
		}
	}
	if in.Username != nil {
		a.Username = str(in.Username)
	}
	if in.IMAPHost != nil {
		a.IMAPHost = str(in.IMAPHost)
	}
	if in.SMTPHost != nil {
		a.SMTPHost = str(in.SMTPHost)
	}
	if in.IMAPPort != nil {
		a.IMAPPort = *in.IMAPPort
	}
	if in.SMTPPort != nil {
		a.SMTPPort = *in.SMTPPort
	}
	if in.IMAPSecurity != nil {
		a.IMAPSecurity = str(in.IMAPSecurity)
	}
	if in.SMTPSecurity != nil {
		a.SMTPSecurity = str(in.SMTPSecurity)
	}
	if in.SavesSent != nil {
		a.SavesSent = *in.SavesSent
	}
	if in.Color != nil {
		a.Color = str(in.Color)
	}
	if in.Signature != nil {
		a.Signature = sanitizeUserHTML(*in.Signature)
	}
	if in.SyncEnabled != nil {
		a.SyncEnabled = *in.SyncEnabled
	}
	if in.SyncDays != nil {
		a.SyncDays = *in.SyncDays
	}
	if in.Sort != nil {
		a.Sort = *in.Sort
	}
	switch {
	case a.IMAPPort < 0 || a.IMAPPort > 65535 || a.SMTPPort < 0 || a.SMTPPort > 65535:
		return "ports are 1 to 65535"
	case a.IMAPSecurity != "" && !validSecurity(a.IMAPSecurity), a.SMTPSecurity != "" && !validSecurity(a.SMTPSecurity):
		return "security is SSL/TLS, STARTTLS or none"
	case a.SyncDays != 0 && (a.SyncDays < 7 || a.SyncDays > 3650):
		return "keep 7 days to 10 years of mail here"
	case len(a.Label) > 60 || len(a.Name) > 120:
		return "that name is too long"
	}
	return ""
}

// withPreset fills empty server settings from what is known about the address.
func withPreset(ctx context.Context, a *store.Account) {
	if a.IMAPHost != "" && a.SMTPHost != "" {
		if a.Username == "" {
			a.Username = a.Email
		}
		return
	}
	p := pmail.Discover(ctx, a.Email)
	if a.Provider == "" || a.Provider == "custom" {
		a.Provider = p.Provider
	}
	if a.IMAPHost == "" {
		a.IMAPHost, a.IMAPPort, a.IMAPSecurity = p.IMAPHost, p.IMAPPort, p.IMAPSecurity
	}
	if a.SMTPHost == "" {
		a.SMTPHost, a.SMTPPort, a.SMTPSecurity = p.SMTPHost, p.SMTPPort, p.SMTPSecurity
	}
	if a.Username == "" {
		a.Username = firstNonEmpty(p.Username, a.Email)
	}
	a.SavesSent = a.SavesSent || p.SavesSent
}

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	uid := actorID(r.Context())
	a := store.Account{UserID: uid, Auth: "password"}
	if msg := in.apply(&a); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	withPreset(ctx, &a)
	pass := ""
	if in.Password != nil {
		pass = *in.Password
	}
	if pass == "" {
		writeErr(w, http.StatusBadRequest, "enter the password")
		return
	}
	if err := s.mail.Test(ctx, a, pass); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	list, err := s.accountViews(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "accounts")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleCreateAccount adds an account with a password (OAuth accounts come
// in through /oauth/…/callback). The sign-in is checked before it is saved.
func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx := r.Context()
	uid := actorID(ctx)
	a := store.Account{UserID: uid, Auth: "password", SyncEnabled: true}
	if in.Email == nil {
		writeErr(w, http.StatusBadRequest, "enter the email address")
		return
	}
	if msg := in.apply(&a); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if _, err := s.store.AccountByEmail(ctx, uid, a.Email); err == nil {
		writeErr(w, http.StatusConflict, "this address is already added")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	withPreset(tctx, &a)
	if a.Name == "" {
		if u := userFrom(ctx); u != nil {
			a.Name = u.Name
		}
	}
	pass := ""
	if in.Password != nil {
		pass = *in.Password
	}
	if pass == "" {
		writeErr(w, http.StatusBadRequest, "enter the password")
		return
	}
	if err := s.mail.Test(tctx, a, pass); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sealed, err := s.box.Seal(pass)
	if err != nil {
		s.fail(w, err, "seal password")
		return
	}
	a.Secret = sealed
	if a.Color == "" {
		a.Color = accountColor(a.Email)
	}
	if err := s.store.CreateAccount(ctx, &a); err != nil {
		s.fail(w, err, "add account")
		return
	}
	s.mail.Kick(a.ID)
	s.firstAccountDefault(ctx, uid, a.ID)
	writeJSON(w, http.StatusCreated, s.accountView(ctx, a))
}

// firstAccountDefault makes the first account the one new mail is written from.
func (s *Server) firstAccountDefault(ctx context.Context, uid, id int64) {
	st, err := s.store.GetSettings(ctx, uid)
	if err != nil || st.DefaultAccount != 0 {
		return
	}
	st.DefaultAccount = id
	_ = s.store.SaveUserSettings(ctx, uid, st)
}

var accountColors = []string{"#0b6bcb", "#e5484d", "#16a34a", "#d97706", "#7c3aed", "#0891b2", "#db2777", "#65a30d"}

func accountColor(email string) string {
	h := 0
	for _, c := range strings.ToLower(email) {
		h = h*31 + int(c)
	}
	if h < 0 {
		h = -h
	}
	return accountColors[h%len(accountColors)]
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAccount(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "account")
		return
	}
	writeJSON(w, http.StatusOK, s.accountView(r.Context(), a))
}

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.store.GetAccount(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "account")
		return
	}
	var in accountInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if a.Provider == "demo" {
		in.Email, in.Provider, in.IMAPHost, in.SMTPHost, in.Password = nil, nil, nil, nil, nil
	}
	before := a
	if msg := in.apply(&a); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	serverChanged := a.IMAPHost != before.IMAPHost || a.IMAPPort != before.IMAPPort || a.IMAPSecurity != before.IMAPSecurity ||
		a.SMTPHost != before.SMTPHost || a.SMTPPort != before.SMTPPort || a.SMTPSecurity != before.SMTPSecurity ||
		a.Username != before.Username || a.Email != before.Email
	pass := ""
	if in.Password != nil {
		pass = *in.Password
	}
	if pass != "" || (serverChanged && a.Auth == "password") {
		tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if err := s.mail.Test(tctx, a, pass); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if pass != "" {
			if a.Secret, err = s.box.Seal(pass); err != nil {
				s.fail(w, err, "seal password")
				return
			}
			a.Auth, a.OAuthProvider, a.AccessToken, a.TokenExpiry = "password", "", "", ""
		}
	}
	if err := s.store.UpdateAccount(ctx, &a); err != nil {
		s.fail(w, err, "save account")
		return
	}
	if pass != "" || serverChanged || (a.SyncEnabled && !before.SyncEnabled) || a.SyncDays != before.SyncDays {
		if a.SyncState == "auth" || a.SyncState == "error" {
			_ = s.store.SetAccountSync(ctx, a.ID, "new", "")
		}
		s.mail.Kick(a.ID)
	}
	a, _ = s.store.GetAccount(ctx, a.UserID, a.ID)
	writeJSON(w, http.StatusOK, s.accountView(ctx, a))
}

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id := idParam(r, "id")
	if err := s.store.DeleteAccount(ctx, uid, id); err != nil {
		s.fail(w, err, "remove account")
		return
	}
	s.mail.Kick(id) // the engine notices the account is gone and stops its loop
	if st, err := s.store.GetSettings(ctx, uid); err == nil && st.DefaultAccount == id {
		st.DefaultAccount = 0
		if list, _ := s.store.ListAccounts(ctx, uid); len(list) > 0 {
			st.DefaultAccount = list[0].ID
		}
		_ = s.store.SaveUserSettings(ctx, uid, st)
	}
	s.hub.send(uid, "change", map[string]any{"type": "change", "account_id": id, "new": []int64{}})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSyncAccount(w http.ResponseWriter, r *http.Request) {
	a, err := s.store.GetAccount(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "account")
		return
	}
	if a.SyncState == "error" || a.SyncState == "auth" {
		_ = s.store.SetAccountSync(r.Context(), a.ID, "new", "")
	}
	s.mail.Kick(a.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAccountFolders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetAccount(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "account")
		return
	}
	fs, err := s.store.ListFolders(ctx, uid, a.ID)
	if err != nil {
		s.fail(w, err, "folders")
		return
	}
	writeJSON(w, http.StatusOK, fs)
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, err := s.store.GetAccount(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "account")
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" || len(in.Name) > 200 {
		writeErr(w, http.StatusBadRequest, "give the folder a name")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	f, err := s.mail.CreateFolder(tctx, a, strings.TrimSpace(in.Name))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		Sync *bool `json:"sync"`
	}
	if err := decode(r, &in); err != nil || in.Sync == nil {
		writeErr(w, http.StatusBadRequest, "send sync: true or false")
		return
	}
	id := idParam(r, "id")
	if err := s.store.SetFolderSync(ctx, uid, id, *in.Sync); err != nil {
		s.fail(w, err, "folder")
		return
	}
	f, err := s.store.GetFolder(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "folder")
		return
	}
	if *in.Sync {
		s.mail.Kick(f.AccountID)
	}
	writeJSON(w, http.StatusOK, f)
}

var _ = chi.URLParam
