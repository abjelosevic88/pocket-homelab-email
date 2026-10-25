package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	pmail "github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// ---- "Sign in with Google / Microsoft" for mail accounts ----
//
// The OAuth clients are the box owner's own (Google Cloud / Microsoft Entra
// app registrations): from GOOGLE_* / MICROSOFT_* in the environment, else
// what an admin entered under Settings → Accounts. The browser goes to the
// provider with PKCE; the state, verifier and who started it ride in a
// signed, short-lived cookie.

const oauthCookie = "pml_oauth"

func (s *Server) oauthClients(ctx context.Context) pmail.OAuthClients {
	st, _ := s.store.GetSettings(ctx, 0)
	c := pmail.OAuthClients{
		GoogleID: st.GoogleClientID, GoogleSecret: st.GoogleClientSecret,
		MicrosoftID: st.MicrosoftClientID, MicrosoftSecret: st.MicrosoftClientSecret, MicrosoftTenant: st.MicrosoftTenant,
	}
	if s.cfg.GoogleClientID != "" {
		c.GoogleID, c.GoogleSecret = s.cfg.GoogleClientID, s.cfg.GoogleClientSecret
	}
	if s.cfg.MicrosoftClientID != "" {
		c.MicrosoftID, c.MicrosoftSecret, c.MicrosoftTenant = s.cfg.MicrosoftClientID, s.cfg.MicrosoftClientSecret, s.cfg.MicrosoftTenant
	}
	if c.MicrosoftTenant == "" {
		c.MicrosoftTenant = "common"
	}
	return c
}

// oauthReady reports whether "Sign in with …" can be offered for a provider.
func (s *Server) oauthReady(ctx context.Context, provider string) bool {
	if s.cfg.BaseURL == "" {
		return false
	}
	c := s.oauthClients(ctx)
	switch provider {
	case "google":
		return c.GoogleID != ""
	case "microsoft":
		return c.MicrosoftID != ""
	}
	return false
}

// oauthRedirect is the redirect URI to register with the provider.
func (s *Server) oauthRedirect(provider string) string {
	if s.cfg.BaseURL == "" {
		return ""
	}
	return s.cfg.BaseURL + "/api/v1/oauth/" + provider + "/callback"
}

type oauthState struct {
	State     string `json:"s"`
	Verifier  string `json:"v"`
	Provider  string `json:"p"`
	UserID    int64  `json:"u"`
	AccountID int64  `json:"a"`
	Email     string `json:"e"`
	Label     string `json:"l"`
	At        int64  `json:"t"`
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := chi.URLParam(r, "provider")
	if provider != "google" && provider != "microsoft" {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if !s.oauthReady(ctx, provider) {
		writeErr(w, http.StatusConflict, "sign-in with "+providerName(provider)+" is not set up on this box yet (Settings → Accounts → Sign-in setup)")
		return
	}
	uid := actorID(ctx)
	q := r.URL.Query()
	st := oauthState{State: randToken(24), Verifier: randToken(48), Provider: provider, UserID: uid,
		Email: strings.TrimSpace(q.Get("email")), Label: strings.TrimSpace(q.Get("label")), At: time.Now().Unix()}
	if id := queryInt(r, "account_id"); id > 0 {
		if _, err := s.store.GetAccount(ctx, uid, int64(id)); err != nil {
			s.fail(w, err, "account")
			return
		}
		st.AccountID = int64(id)
	}
	b, _ := json.Marshal(st)
	val := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: val + "." + s.box.Sign(val), Path: "/api/v1/oauth/", MaxAge: 600,
		HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	u, err := pmail.OAuthAuthURL(s.oauthClients(ctx), provider, s.oauthRedirect(provider), st.State, st.Verifier, st.Email)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	http.Redirect(w, r, u, http.StatusFound)
}

func providerName(p string) string {
	switch p {
	case "google":
		return "Google"
	case "microsoft":
		return "Microsoft"
	}
	return p
}

// handleOAuthCallback finishes the sign-in: the account is added (or its
// tokens renewed) and the browser goes back to Settings → Accounts.
func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	provider := chi.URLParam(r, "provider")
	back := func(q string) { http.Redirect(w, r, "/settings/accounts?"+q, http.StatusFound) }
	fail := func(msg string) { back("oauth_error=" + url.QueryEscape(msg)) }
	c, err := r.Cookie(oauthCookie)
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Value: "", Path: "/api/v1/oauth/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	if err != nil {
		fail("The sign-in took too long or was started in another browser. Try again.")
		return
	}
	val, sig, ok := strings.Cut(c.Value, ".")
	var st oauthState
	raw, derr := base64.RawURLEncoding.DecodeString(val)
	if !ok || sig != s.box.Sign(val) || derr != nil || json.Unmarshal(raw, &st) != nil {
		fail("The sign-in could not be checked. Try again.")
		return
	}
	q := r.URL.Query()
	switch {
	case st.Provider != provider || st.UserID != actorID(ctx) || q.Get("state") != st.State || time.Since(time.Unix(st.At, 0)) > 10*time.Minute:
		fail("The sign-in could not be checked. Try again.")
		return
	case q.Get("error") != "":
		msg := q.Get("error_description")
		if q.Get("error") == "access_denied" {
			msg = "You did not allow access, so nothing was added."
		}
		fail(firstNonEmpty(msg, q.Get("error")))
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tok, err := pmail.OAuthExchange(tctx, s.oauthClients(ctx), provider, s.oauthRedirect(provider), q.Get("code"), st.Verifier)
	if err != nil {
		fail(err.Error())
		return
	}
	email := firstNonEmpty(tok.Email, st.Email)
	if !validEmail(email) {
		fail(providerName(provider) + " did not say which address you signed in with.")
		return
	}
	var a store.Account
	switch {
	case st.AccountID > 0:
		a, err = s.store.GetAccount(ctx, st.UserID, st.AccountID)
	default:
		a, err = s.store.AccountByEmail(ctx, st.UserID, email)
	}
	isNew := err != nil
	if isNew {
		a = store.Account{UserID: st.UserID, Email: email, Label: st.Label, SyncEnabled: true}
		p := presetFor(provider, email)
		a.Provider, a.IMAPHost, a.IMAPPort, a.IMAPSecurity = p.Provider, p.IMAPHost, p.IMAPPort, p.IMAPSecurity
		a.SMTPHost, a.SMTPPort, a.SMTPSecurity, a.SavesSent = p.SMTPHost, p.SMTPPort, p.SMTPSecurity, p.SavesSent
		a.Username = email
		a.Name = tok.Name
		if a.Name == "" {
			if u := userFrom(ctx); u != nil {
				a.Name = u.Name
			}
		}
		a.Color = accountColor(email)
	}
	a.Auth, a.OAuthProvider = "oauth", provider
	if a.Username == "" {
		a.Username = email
	}
	if a.AccessToken, err = s.box.Seal(tok.AccessToken); err == nil && tok.RefreshToken != "" {
		a.Secret, err = s.box.Seal(tok.RefreshToken)
	}
	if err != nil {
		fail("The sign-in could not be stored: " + err.Error())
		return
	}
	if tok.RefreshToken == "" && isNew {
		fail(providerName(provider) + " gave no lasting access. Remove Pocket Mail from your account's connected apps and try again.")
		return
	}
	a.TokenExpiry = tok.Expiry.UTC().Format(time.RFC3339)
	if isNew {
		err = s.store.CreateAccount(ctx, &a)
	} else {
		err = s.store.UpdateAccount(ctx, &a)
	}
	if err != nil {
		fail("The account could not be saved: " + err.Error())
		return
	}
	_ = s.store.SetAccountSync(ctx, a.ID, "new", "")
	s.mail.Kick(a.ID)
	s.firstAccountDefault(ctx, st.UserID, a.ID)
	back("connected=" + strconv.FormatInt(a.ID, 10))
}

// presetFor is the server settings for an OAuth account: the built-in
// preset of the provider (Gmail for Google, Outlook for Microsoft).
func presetFor(provider, email string) pmail.Preset {
	want := "gmail"
	if provider == "microsoft" {
		want = "outlook"
	}
	for _, p := range pmail.Providers() {
		if p.Provider == want {
			return p
		}
	}
	if provider == "microsoft" {
		return pmail.Preset{Provider: "outlook", IMAPHost: "outlook.office365.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPSecurity: "starttls", SavesSent: true}
	}
	return pmail.Preset{Provider: "gmail", IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "ssl", SavesSent: true}
}
