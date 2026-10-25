package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// authError means the server refused the sign-in: the account needs the
// person (state "auth"), retrying will not help.
type authError struct{ msg string }

func (e *authError) Error() string { return e.msg }

func isAuthError(err error) bool {
	var ae *authError
	return errors.As(err, &ae)
}

// testTLS lets tests trust a self-signed server.
var testTLS *tls.Config

func tlsFor(host string) *tls.Config {
	if testTLS != nil {
		return testTLS.Clone()
	}
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

func providerName(a *store.Account) string {
	if p, ok := presetFor(a.Provider); ok {
		return p.Name
	}
	return "The mail server"
}

// xoauth2 is the SASL XOAUTH2 mechanism (Gmail, Outlook).
type xoauth2 struct{ user, token string }

func (x *xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// Next answers the error challenge with an empty line so the server sends its final NO.
func (x *xoauth2) Next(challenge []byte) ([]byte, error) { return []byte{}, nil }

func username(a *store.Account) string {
	if a.Username != "" {
		return a.Username
	}
	return a.Email
}

// dialError turns a connection failure into plain words.
func dialError(host string, port int, err error) error {
	if err == nil {
		return nil
	}
	var dnsErr *net.DNSError
	msg := err.Error()
	switch {
	case errors.As(err, &dnsErr):
		return fmt.Errorf("can't find the mail server %s — check the server name", host)
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"):
		return fmt.Errorf("the security certificate of %s isn't valid — check the server name", host)
	case strings.Contains(msg, "first record does not look like a TLS handshake"), strings.Contains(msg, "tls: "):
		return fmt.Errorf("%s:%d doesn't speak the chosen security (SSL/STARTTLS) — try the other one or another port", host, port)
	case strings.Contains(msg, "refused"), strings.Contains(msg, "timeout"), strings.Contains(msg, "i/o timeout"), strings.Contains(msg, "unreachable"):
		return fmt.Errorf("can't reach %s on port %d — check the server name and port, or try again later", host, port)
	}
	return fmt.Errorf("can't connect to %s:%d: %v", host, port, err)
}

// loginError explains a refused sign-in for the provider.
func loginError(a *store.Account, err error) error {
	if err == nil {
		return nil
	}
	if isAuthError(err) {
		return err
	}
	var ie *imap.Error
	text := err.Error()
	if errors.As(err, &ie) {
		text = ie.Text
	}
	low := strings.ToLower(text)
	if !(strings.Contains(low, "auth") || strings.Contains(low, "invalid") || strings.Contains(low, "password") || strings.Contains(low, "credential") ||
		strings.Contains(low, "login") || strings.Contains(low, "denied") || (ie != nil && ie.Type == imap.StatusResponseTypeNo)) {
		return err
	}
	if a.Auth == "oauth" {
		return &authError{msg: providerName(a) + " did not accept the sign-in for " + a.Email + ". Sign in again."}
	}
	switch a.Provider {
	case "gmail":
		return &authError{msg: "Gmail did not accept the password. Use an app password (Google Account → Security → 2-Step Verification → App passwords), or Sign in with Google."}
	case "icloud":
		return &authError{msg: "iCloud did not accept the password. Use an app-specific password from account.apple.com → Sign-In and Security → App-Specific Passwords."}
	case "outlook":
		return &authError{msg: "Microsoft doesn't accept passwords for Outlook.com mail any more. Use Sign in with Microsoft."}
	case "yahoo", "aol", "fastmail", "yandex", "mailru", "zoho":
		return &authError{msg: providerName(a) + " did not accept the password. Use an app password made for mail apps, not your normal password."}
	case "gmx", "webde":
		return &authError{msg: providerName(a) + " did not accept the sign-in. Check the password, and that IMAP access is turned on in its settings."}
	}
	return &authError{msg: "The mail server did not accept the user name or password (" + strings.TrimSpace(text) + ")."}
}

// dialIMAP connects and signs in. password "" = the account's stored secret
// (or a fresh OAuth token). handler gets unsolicited updates (IDLE).
func (e *Engine) dialIMAP(ctx context.Context, a *store.Account, password string, handler *imapclient.UnilateralDataHandler) (*imapclient.Client, error) {
	if a.IMAPHost == "" {
		return nil, errors.New("the incoming (IMAP) server is missing")
	}
	addr := net.JoinHostPort(a.IMAPHost, strconv.Itoa(a.IMAPPort))
	opts := &imapclient.Options{
		TLSConfig:             tlsFor(a.IMAPHost),
		WordDecoder:           &mime.WordDecoder{CharsetReader: charset.Reader},
		UnilateralDataHandler: handler,
		Dialer:                &net.Dialer{Timeout: 20 * time.Second},
	}
	var c *imapclient.Client
	var err error
	switch a.IMAPSecurity {
	case "starttls":
		c, err = imapclient.DialStartTLS(addr, opts)
	case "none":
		c, err = imapclient.DialInsecure(addr, opts)
	default:
		c, err = imapclient.DialTLS(addr, opts)
	}
	if err != nil {
		return nil, dialError(a.IMAPHost, a.IMAPPort, err)
	}
	if err := c.WaitGreeting(); err != nil {
		c.Close()
		return nil, dialError(a.IMAPHost, a.IMAPPort, err)
	}
	if a.Auth == "oauth" && password == "" {
		tok, err := e.accessToken(ctx, a)
		if err != nil {
			c.Close()
			return nil, err
		}
		if err := c.Authenticate(&xoauth2{user: username(a), token: tok}); err != nil {
			c.Close()
			return nil, loginError(a, err)
		}
		return c, nil
	}
	if password == "" {
		p, err := e.box.Open(a.Secret)
		if err != nil {
			c.Close()
			return nil, &authError{msg: err.Error()}
		}
		password = p
	}
	if password == "" {
		c.Close()
		return nil, &authError{msg: "There is no password saved for " + a.Email + "."}
	}
	if err := c.Login(username(a), password).Wait(); err != nil {
		c.Close()
		return nil, loginError(a, err)
	}
	return c, nil
}

// dialSMTP connects to the outgoing server and signs in.
func (e *Engine) dialSMTP(ctx context.Context, a *store.Account, password string) (*smtp.Client, error) {
	if a.SMTPHost == "" {
		return nil, errors.New("the outgoing (SMTP) server is missing")
	}
	addr := net.JoinHostPort(a.SMTPHost, strconv.Itoa(a.SMTPPort))
	var c *smtp.Client
	var err error
	switch a.SMTPSecurity {
	case "starttls":
		c, err = smtp.DialStartTLS(addr, tlsFor(a.SMTPHost))
	case "none":
		c, err = smtp.Dial(addr)
	default:
		c, err = smtp.DialTLS(addr, tlsFor(a.SMTPHost))
	}
	if err != nil {
		return nil, dialError(a.SMTPHost, a.SMTPPort, err)
	}
	var auth sasl.Client
	if a.Auth == "oauth" && password == "" {
		tok, err := e.accessToken(ctx, a)
		if err != nil {
			c.Close()
			return nil, err
		}
		auth = &xoauth2{user: username(a), token: tok}
	} else {
		if password == "" {
			if password, err = e.box.Open(a.Secret); err != nil {
				c.Close()
				return nil, &authError{msg: err.Error()}
			}
		}
		if c.SupportsAuth("PLAIN") || !c.SupportsAuth("LOGIN") {
			auth = sasl.NewPlainClient("", username(a), password)
		} else {
			auth = sasl.NewLoginClient(username(a), password)
		}
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(auth); err != nil {
			c.Close()
			return nil, smtpLoginError(a, err)
		}
	}
	return c, nil
}

func smtpLoginError(a *store.Account, err error) error {
	var se *smtp.SMTPError
	if errors.As(err, &se) && (se.Code == 535 || se.Code == 534 || se.Code == 530) {
		if a.Auth == "oauth" {
			return &authError{msg: providerName(a) + " did not let this app send mail as " + a.Email + ". Sign in again (for Microsoft 365 an admin may have to allow SMTP sending)."}
		}
		e := loginError(a, &imap.Error{Type: imap.StatusResponseTypeNo, Text: se.Message})
		return fmt.Errorf("sending: %w", e)
	}
	return fmt.Errorf("sending: %v", err)
}

// Test signs in to IMAP and SMTP with these settings; password "" uses the
// account's stored secret (or OAuth tokens). The error is plain language.
func (e *Engine) Test(ctx context.Context, a store.Account, password string) error {
	if a.Provider == "demo" {
		return nil
	}
	c, err := e.dialIMAP(ctx, &a, password, nil)
	if err != nil {
		return err
	}
	_, lerr := c.List("", "INBOX", nil).Collect()
	_ = c.Logout().Wait()
	c.Close()
	if lerr != nil {
		return fmt.Errorf("signed in, but the inbox could not be read: %v", lerr)
	}
	s, err := e.dialSMTP(ctx, &a, password)
	if err != nil {
		return err
	}
	_ = s.Quit()
	return nil
}
