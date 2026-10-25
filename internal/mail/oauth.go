package mail

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// OAuth endpoints (variables so tests can point them at a fake).
var (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	msBase         = "https://login.microsoftonline.com/"
	oauthHTTP      = &http.Client{Timeout: 20 * time.Second}
)

const (
	googleScope    = "https://mail.google.com/ openid email profile"
	microsoftScope = "https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send offline_access openid email profile"
)

func msTenant(c OAuthClients) string {
	if c.MicrosoftTenant == "" {
		return "common"
	}
	return c.MicrosoftTenant
}

func oauthClient(c OAuthClients, provider string) (id, secret, authURL, tokenURL, scope string, err error) {
	switch provider {
	case "google":
		if c.GoogleID == "" {
			return "", "", "", "", "", errors.New("Sign in with Google is not set up on this server yet. An admin can add it under Settings → Accounts, or use an app password instead.")
		}
		return c.GoogleID, c.GoogleSecret, googleAuthURL, googleTokenURL, googleScope, nil
	case "microsoft":
		if c.MicrosoftID == "" {
			return "", "", "", "", "", errors.New("Sign in with Microsoft is not set up on this server yet. An admin can add it under Settings → Accounts.")
		}
		t := msTenant(c)
		return c.MicrosoftID, c.MicrosoftSecret, msBase + t + "/oauth2/v2.0/authorize", msBase + t + "/oauth2/v2.0/token", microsoftScope, nil
	}
	return "", "", "", "", "", fmt.Errorf("unknown sign-in provider %q", provider)
}

func pkce(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// OAuthAuthURL is where to send the browser to sign in (PKCE S256 with verifier).
func OAuthAuthURL(c OAuthClients, provider, redirectURI, state, verifier, loginHint string) (string, error) {
	id, _, authURL, _, scope, err := oauthClient(c, provider)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"client_id": {id}, "redirect_uri": {redirectURI}, "response_type": {"code"}, "scope": {scope},
		"state": {state}, "code_challenge": {pkce(verifier)}, "code_challenge_method": {"S256"},
	}
	if loginHint != "" {
		q.Set("login_hint", loginHint)
	}
	switch provider {
	case "google":
		q.Set("access_type", "offline")
		q.Set("prompt", "consent")
		q.Set("include_granted_scopes", "true")
	case "microsoft":
		q.Set("response_mode", "query")
		q.Set("prompt", "select_account")
	}
	return authURL + "?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	IDToken          string `json:"id_token"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func postToken(ctx context.Context, tokenURL string, form url.Values) (tokenResponse, error) {
	var tr tokenResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tr, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := oauthHTTP.Do(req)
	if err != nil {
		return tr, fmt.Errorf("could not reach the sign-in service: %w", err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	_ = json.Unmarshal(b, &tr)
	if res.StatusCode != http.StatusOK || tr.AccessToken == "" {
		if tr.Error != "" {
			return tr, &oauthError{code: tr.Error, desc: tr.ErrorDescription}
		}
		return tr, fmt.Errorf("the sign-in service answered %s", res.Status)
	}
	return tr, nil
}

type oauthError struct{ code, desc string }

func (e *oauthError) Error() string {
	if e.code == "invalid_grant" {
		return "the permission to read this mailbox was withdrawn or has expired — sign in again"
	}
	if e.desc != "" {
		return e.code + ": " + e.desc
	}
	return e.code
}

// idClaims reads the (unverified) claims of an ID token that came straight
// from the token endpoint over TLS.
func idClaims(tok string) map[string]any {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func tokensFrom(tr tokenResponse) Tokens {
	t := Tokens{AccessToken: tr.AccessToken, RefreshToken: tr.RefreshToken}
	if tr.ExpiresIn > 0 {
		t.Expiry = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	} else {
		t.Expiry = time.Now().Add(50 * time.Minute)
	}
	if c := idClaims(tr.IDToken); c != nil {
		for _, k := range []string{"email", "preferred_username", "upn"} {
			if v, _ := c[k].(string); v != "" && strings.Contains(v, "@") {
				t.Email = v
				break
			}
		}
		t.Name, _ = c["name"].(string)
	}
	return t
}

// OAuthExchange trades the code for tokens and the signed-in address.
func OAuthExchange(ctx context.Context, c OAuthClients, provider, redirectURI, code, verifier string) (Tokens, error) {
	id, secret, _, tokenURL, scope, err := oauthClient(c, provider)
	if err != nil {
		return Tokens{}, err
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {id}, "code_verifier": {verifier}}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	if provider == "microsoft" {
		form.Set("scope", scope)
	}
	tr, err := postToken(ctx, tokenURL, form)
	if err != nil {
		return Tokens{}, err
	}
	t := tokensFrom(tr)
	if t.RefreshToken == "" {
		return t, errors.New("the sign-in did not allow offline access; try again and accept every permission")
	}
	if t.Email == "" {
		return t, errors.New("the sign-in did not say which address it was for")
	}
	return t, nil
}

func oauthRefresh(ctx context.Context, c OAuthClients, provider, refresh string) (Tokens, error) {
	id, secret, _, tokenURL, scope, err := oauthClient(c, provider)
	if err != nil {
		return Tokens{}, err
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {id}}
	if secret != "" {
		form.Set("client_secret", secret)
	}
	if provider == "microsoft" {
		form.Set("scope", scope)
	}
	tr, err := postToken(ctx, tokenURL, form)
	if err != nil {
		return Tokens{}, err
	}
	return tokensFrom(tr), nil
}

// accessToken returns a fresh OAuth access token for an account, refreshing
// (and storing) it when it is about to run out.
func (e *Engine) accessToken(ctx context.Context, a *store.Account) (string, error) {
	mu := e.rt.lockFor(a.ID)
	mu.Lock()
	defer mu.Unlock()
	if cur, err := e.st.GetAccount(ctx, 0, a.ID); err == nil {
		a.AccessToken, a.TokenExpiry, a.Secret = cur.AccessToken, cur.TokenExpiry, cur.Secret
	}
	if exp, err := time.Parse(time.RFC3339, a.TokenExpiry); err == nil && time.Until(exp) > 2*time.Minute {
		if tok, err := e.box.Open(a.AccessToken); err == nil && tok != "" {
			return tok, nil
		}
	}
	refresh, err := e.box.Open(a.Secret)
	if err != nil || refresh == "" {
		return "", &authError{msg: "Sign in again: the saved sign-in for " + a.Email + " is missing."}
	}
	var clients OAuthClients
	if e.cfg.OAuth != nil {
		clients = e.cfg.OAuth(ctx)
	}
	t, err := oauthRefresh(ctx, clients, a.OAuthProvider, refresh)
	if err != nil {
		var oe *oauthError
		if errors.As(err, &oe) || strings.Contains(err.Error(), "not set up") {
			return "", &authError{msg: providerName(a) + ": " + err.Error() + "."}
		}
		return "", err
	}
	sealed, err := e.box.Seal(t.AccessToken)
	if err != nil {
		return "", err
	}
	newRefresh := ""
	if t.RefreshToken != "" && t.RefreshToken != refresh {
		if newRefresh, err = e.box.Seal(t.RefreshToken); err != nil {
			return "", err
		}
	}
	exp := t.Expiry.UTC().Format(time.RFC3339)
	if err := e.st.SetAccountTokens(ctx, a.ID, sealed, exp, newRefresh); err != nil {
		return "", err
	}
	a.AccessToken, a.TokenExpiry = sealed, exp
	if newRefresh != "" {
		a.Secret = newRefresh
	}
	return t.AccessToken, nil
}
