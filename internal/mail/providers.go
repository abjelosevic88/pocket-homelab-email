package mail

import (
	"context"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// builtin are the providers known without asking the network.
var builtin = []struct {
	Preset
	domains []string
	mx      []string // MX host suffixes that mean this provider (custom domains)
}{
	{Preset{Provider: "gmail", Name: "Gmail", IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"oauth", "password"}, OAuth: "google", SavesSent: true, AppPassword: true, HelpURL: "https://myaccount.google.com/apppasswords",
		Help: "Sign in with Google, or use an app password: turn on 2-Step Verification in your Google Account, then make an app password and paste it here."},
		[]string{"gmail.com", "googlemail.com"}, []string{"google.com", "googlemail.com"}},
	{Preset{Provider: "icloud", Name: "iCloud Mail", IMAPHost: "imap.mail.me.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: "starttls",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://account.apple.com",
		Help: "iCloud needs an app-specific password, not your Apple Account password: go to account.apple.com → Sign-In and Security → App-Specific Passwords, make one and paste it here."},
		[]string{"icloud.com", "me.com", "mac.com"}, []string{"mail.icloud.com"}},
	{Preset{Provider: "outlook", Name: "Outlook.com / Microsoft 365", IMAPHost: "outlook.office365.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPSecurity: "starttls",
		Auth: []string{"oauth"}, OAuth: "microsoft", SavesSent: true,
		Help: "Microsoft only lets mail apps in with \"Sign in with Microsoft\" — passwords are no longer accepted for Outlook.com. For a work or school account your IT department may have to allow it."},
		[]string{"outlook.com", "hotmail.com", "hotmail.co.uk", "hotmail.de", "hotmail.fr", "hotmail.it", "live.com", "live.co.uk", "live.de", "live.fr", "msn.com", "outlook.de", "outlook.fr"},
		[]string{"mail.protection.outlook.com", "olc.protection.outlook.com"}},
	{Preset{Provider: "yahoo", Name: "Yahoo Mail", IMAPHost: "imap.mail.yahoo.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.yahoo.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://login.yahoo.com/account/security",
		Help: "Yahoo needs an app password: Account Security → Generate app password, then paste it here."},
		[]string{"yahoo.com", "yahoo.co.uk", "yahoo.de", "yahoo.fr", "ymail.com", "rocketmail.com"}, []string{"yahoodns.net"}},
	{Preset{Provider: "aol", Name: "AOL Mail", IMAPHost: "imap.aol.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.aol.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://login.aol.com/account/security",
		Help: "AOL needs an app password: Account Security → Generate app password, then paste it here."},
		[]string{"aol.com", "aim.com"}, nil},
	{Preset{Provider: "fastmail", Name: "Fastmail", IMAPHost: "imap.fastmail.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.fastmail.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://app.fastmail.com/settings/security/apps",
		Help: "Fastmail needs an app password: Settings → Privacy & Security → App passwords → New app password (IMAP and SMTP)."},
		[]string{"fastmail.com", "fastmail.fm", "messagingengine.com"}, []string{"messagingengine.com"}},
	{Preset{Provider: "zoho", Name: "Zoho Mail", IMAPHost: "imap.zoho.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.zoho.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://accounts.zoho.com/home#security/app_password",
		Help: "Turn on IMAP access in Zoho Mail settings; with two-factor sign-in use an app-specific password."},
		[]string{"zoho.com", "zohomail.com", "zohomail.eu"}, []string{"zoho.com", "zoho.eu"}},
	{Preset{Provider: "gmx", Name: "GMX", IMAPHost: "imap.gmx.net", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "mail.gmx.net", SMTPPort: 587, SMTPSecurity: "starttls",
		Auth: []string{"password"}, Help: "Turn on \"POP3/IMAP access\" in the GMX settings first (E-mail → Settings → POP3/IMAP)."},
		[]string{"gmx.net", "gmx.de", "gmx.at", "gmx.ch", "gmx.com"}, []string{"gmx.net"}},
	{Preset{Provider: "webde", Name: "WEB.DE", IMAPHost: "imap.web.de", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.web.de", SMTPPort: 587, SMTPSecurity: "starttls",
		Auth: []string{"password"}, Help: "Turn on \"POP3/IMAP access\" in the WEB.DE settings first."},
		[]string{"web.de"}, []string{"web.de"}},
	{Preset{Provider: "yandex", Name: "Yandex Mail", IMAPHost: "imap.yandex.com", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.yandex.com", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, HelpURL: "https://id.yandex.com/security/app-passwords",
		Help: "Allow mail apps in Yandex Mail settings and use an app password."},
		[]string{"yandex.com", "yandex.ru", "ya.ru"}, []string{"yandex.net", "yandex.ru"}},
	{Preset{Provider: "mailru", Name: "Mail.ru", IMAPHost: "imap.mail.ru", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mail.ru", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, AppPassword: true, Help: "Mail.ru needs a password for external apps (Security settings)."},
		[]string{"mail.ru", "inbox.ru", "list.ru", "bk.ru"}, []string{"mail.ru"}},
	{Preset{Provider: "proton", Name: "Proton Mail (Bridge)", IMAPHost: "127.0.0.1", IMAPPort: 1143, IMAPSecurity: "starttls", SMTPHost: "127.0.0.1", SMTPPort: 1025, SMTPSecurity: "starttls",
		Auth: []string{"password"}, HelpURL: "https://proton.me/mail/bridge",
		Help: "Proton Mail works only through Proton Mail Bridge running on a computer this server can reach. Use the server address, ports and the password Bridge shows you — not your Proton password."},
		[]string{"proton.me", "protonmail.com", "protonmail.ch", "pm.me"}, []string{"protonmail.ch"}},
	{Preset{Provider: "posteo", Name: "Posteo", IMAPHost: "posteo.de", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "posteo.de", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}}, []string{"posteo.de", "posteo.net"}, []string{"posteo.de"}},
	{Preset{Provider: "mailbox", Name: "mailbox.org", IMAPHost: "imap.mailbox.org", IMAPPort: 993, IMAPSecurity: "ssl", SMTPHost: "smtp.mailbox.org", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}}, []string{"mailbox.org"}, []string{"mailbox.org"}},
	{Preset{Provider: "hey", Name: "HEY", Auth: []string{},
		Help: "HEY doesn't let other mail apps in (it has no IMAP), so it can't be added here. Forward your HEY mail to another account instead."},
		[]string{"hey.com"}, []string{"hey.com"}},
}

// Providers lists the built-in providers for the "Add account" picker.
func Providers() []Preset {
	out := make([]Preset, 0, len(builtin)+1)
	for _, b := range builtin {
		p := b.Preset
		p.Found = "builtin"
		out = append(out, p)
	}
	out = append(out, Preset{Provider: "custom", Name: "Other (IMAP)", IMAPPort: 993, IMAPSecurity: "ssl", SMTPPort: 465, SMTPSecurity: "ssl",
		Auth: []string{"password"}, Found: "builtin", Help: "Enter the server names your email provider gives for IMAP (incoming) and SMTP (outgoing) mail."})
	return out
}

// presetFor returns a built-in provider by its key.
func presetFor(provider string) (Preset, bool) {
	for _, b := range builtin {
		if b.Provider == provider {
			return b.Preset, true
		}
	}
	return Preset{}, false
}

func splitEmail(email string) (local, domain string) {
	email = strings.TrimSpace(strings.ToLower(email))
	i := strings.LastIndex(email, "@")
	if i < 0 {
		return email, ""
	}
	return email[:i], email[i+1:]
}

// Lookups that can be replaced in tests.
var (
	lookupMX = func(ctx context.Context, domain string) ([]*net.MX, error) {
		return net.DefaultResolver.LookupMX(ctx, domain)
	}
	ispdbURL  = "https://autoconfig.thunderbird.net/v1.1/"
	ispdbHTTP = &http.Client{Timeout: 5 * time.Second}
)

// Discover works out the servers for an address: the built-in providers,
// then Thunderbird's ISPDB, then the domain's MX records, then a guess.
func Discover(ctx context.Context, email string) Preset {
	_, domain := splitEmail(email)
	finish := func(p Preset, found string) Preset {
		p.Found = found
		if p.Username == "" {
			p.Username = strings.TrimSpace(email)
		}
		return p
	}
	if domain == "" {
		return finish(Preset{Provider: "custom", Name: "Other (IMAP)", IMAPPort: 993, IMAPSecurity: "ssl", SMTPPort: 465, SMTPSecurity: "ssl", Auth: []string{"password"}}, "guess")
	}
	for _, b := range builtin {
		for _, d := range b.domains {
			if d == domain {
				return finish(b.Preset, "builtin")
			}
		}
	}
	if p, ok := ispdb(ctx, domain, email); ok {
		// a known provider behind a custom domain keeps its sign-in help
		for _, b := range builtin {
			if b.IMAPHost != "" && strings.EqualFold(b.IMAPHost, p.IMAPHost) {
				bp := b.Preset
				return finish(bp, "ispdb")
			}
		}
		return finish(p, "ispdb")
	}
	mctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	mxs, _ := lookupMX(mctx, domain)
	cancel()
	for _, mx := range mxs {
		host := strings.TrimSuffix(strings.ToLower(mx.Host), ".")
		for _, b := range builtin {
			for _, suf := range b.mx {
				if host == suf || strings.HasSuffix(host, "."+suf) {
					p := b.Preset
					if b.Provider == "gmail" {
						p.Name = "Google Workspace"
					}
					if b.Provider == "outlook" {
						p.Name = "Microsoft 365"
					}
					return finish(p, "mx")
				}
			}
		}
	}
	return finish(Preset{Provider: "custom", Name: domain, IMAPHost: "imap." + domain, IMAPPort: 993, IMAPSecurity: "ssl",
		SMTPHost: "smtp." + domain, SMTPPort: 465, SMTPSecurity: "ssl", Auth: []string{"password"},
		Help: "These server names are a guess. Check them with your email provider if signing in fails."}, "guess")
}

type ispdbDoc struct {
	Provider struct {
		DisplayName string        `xml:"displayName"`
		Incoming    []ispdbServer `xml:"incomingServer"`
		Outgoing    []ispdbServer `xml:"outgoingServer"`
	} `xml:"emailProvider"`
}

type ispdbServer struct {
	Type       string   `xml:"type,attr"`
	Hostname   string   `xml:"hostname"`
	Port       int      `xml:"port"`
	SocketType string   `xml:"socketType"`
	Username   string   `xml:"username"`
	Auth       []string `xml:"authentication"`
}

func ispdbSecurity(s string) string {
	switch strings.ToUpper(s) {
	case "SSL":
		return "ssl"
	case "STARTTLS":
		return "starttls"
	}
	return "none"
}

func ispdbUser(tpl, email string) string {
	local, domain := splitEmail(email)
	r := strings.NewReplacer("%EMAILADDRESS%", strings.TrimSpace(email), "%EMAILLOCALPART%", local, "%EMAILDOMAIN%", domain)
	return r.Replace(tpl)
}

func ispdb(ctx context.Context, domain, email string) (Preset, bool) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ispdbURL+domain, nil)
	if err != nil {
		return Preset{}, false
	}
	res, err := ispdbHTTP.Do(req)
	if err != nil {
		return Preset{}, false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Preset{}, false
	}
	var doc ispdbDoc
	if err := xml.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&doc); err != nil {
		return Preset{}, false
	}
	p := Preset{Provider: "custom", Name: doc.Provider.DisplayName, Auth: []string{"password"}}
	for _, in := range doc.Provider.Incoming {
		if in.Type == "imap" && in.Hostname != "" {
			p.IMAPHost, p.IMAPPort, p.IMAPSecurity, p.Username = in.Hostname, in.Port, ispdbSecurity(in.SocketType), ispdbUser(in.Username, email)
			break
		}
	}
	for _, out := range doc.Provider.Outgoing {
		if out.Type == "smtp" && out.Hostname != "" {
			p.SMTPHost, p.SMTPPort, p.SMTPSecurity = out.Hostname, out.Port, ispdbSecurity(out.SocketType)
			break
		}
	}
	if p.IMAPHost == "" || p.SMTPHost == "" {
		return Preset{}, false
	}
	if p.Name == "" {
		p.Name = domain
	}
	return p, true
}
