package mail

import (
	"context"
	"errors"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// roleNames recognise the well-known folders when the server doesn't mark them.
var roleNames = map[string]string{
	"inbox": "inbox",
	"sent":  "sent", "sent items": "sent", "sent messages": "sent", "sent mail": "sent", "poslano": "sent", "poslato": "sent", "poslane": "sent",
	"gesendet": "sent", "gesendete elemente": "sent", "envoyés": "sent", "enviados": "sent", "inviata": "sent", "posta inviata": "sent", "verzonden": "sent", "отправленные": "sent",
	"trash": "trash", "deleted items": "trash", "deleted messages": "trash", "deleted": "trash", "bin": "trash", "kanta": "trash", "smeće": "trash", "smece": "trash",
	"obrisano": "trash", "papierkorb": "trash", "gelöschte elemente": "trash", "corbeille": "trash", "papelera": "trash", "cestino": "trash", "удаленные": "trash",
	"junk": "junk", "spam": "junk", "junk e-mail": "junk", "junk email": "junk", "bulk mail": "junk", "neželjeno": "junk", "nezeljeno": "junk", "spam folder": "junk",
	"drafts": "drafts", "draft": "drafts", "nacrti": "drafts", "entwürfe": "drafts", "brouillons": "drafts", "borradores": "drafts", "bozze": "drafts", "черновики": "drafts",
	"archive": "archive", "archives": "archive", "arhiva": "archive", "archiv": "archive", "archivio": "archive", "archivo": "archive",
	"all mail": "all", "starred": "flagged", "important": "important",
}

func roleOf(ld *imap.ListData) string {
	for _, a := range ld.Attrs {
		switch strings.ToLower(string(a)) {
		case `\inbox`:
			return "inbox"
		case `\sent`:
			return "sent"
		case `\trash`:
			return "trash"
		case `\junk`, `\spam`:
			return "junk"
		case `\drafts`:
			return "drafts"
		case `\archive`:
			return "archive"
		case `\all`, `\allmail`:
			return "all"
		case `\flagged`, `\starred`:
			return "flagged"
		case `\important`:
			return "important"
		}
	}
	if strings.EqualFold(ld.Mailbox, "INBOX") {
		return "inbox"
	}
	name := ld.Mailbox
	if ld.Delim != 0 {
		if i := strings.LastIndex(name, string(ld.Delim)); i >= 0 {
			// only top-level (or [Gmail]/ and INBOX/) folders get a role by name
			parent := strings.ToLower(name[:i])
			if parent != "[gmail]" && parent != "[google mail]" && parent != "inbox" {
				return ""
			}
			name = name[i+1:]
		}
	}
	return roleNames[strings.ToLower(strings.TrimSpace(name))]
}

func displayOf(ld *imap.ListData) string {
	name := ld.Mailbox
	if strings.EqualFold(name, "INBOX") {
		return "Inbox"
	}
	if ld.Delim != 0 {
		if i := strings.LastIndex(name, string(ld.Delim)); i >= 0 {
			name = name[i+1:]
		}
	}
	return name
}

func hasAttr(ld *imap.ListData, attr imap.MailboxAttr) bool {
	for _, a := range ld.Attrs {
		if strings.EqualFold(string(a), string(attr)) {
			return true
		}
	}
	return false
}

// syncFolders reads the server's folder list into the store and returns
// the folders here afterwards. Folders gone from the server are removed.
func (e *Engine) syncFolders(ctx context.Context, c *imapclient.Client, a *store.Account) ([]store.Folder, error) {
	var opts *imap.ListOptions
	if c.Caps().Has(imap.CapSpecialUse) {
		opts = &imap.ListOptions{ReturnSpecialUse: true}
	}
	list, err := c.List("", "*", opts).Collect()
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, errors.New("the server listed no folders")
	}
	have, err := e.st.AccountFolders(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	byName := map[string]store.Folder{}
	for _, f := range have {
		byName[f.Name] = f
	}
	seenRole := map[string]bool{}
	onServer := map[string]bool{}
	for _, ld := range list {
		if hasAttr(ld, imap.MailboxAttrNonExistent) {
			continue
		}
		role := roleOf(ld)
		if role != "" && seenRole[role] {
			role = "" // one folder per role
		}
		if role != "" {
			seenRole[role] = true
		}
		delim := "/"
		if ld.Delim != 0 {
			delim = string(ld.Delim)
		}
		f := store.Folder{UserID: a.UserID, AccountID: a.ID, Name: ld.Mailbox, Display: displayOf(ld), Delimiter: delim, Role: role,
			Selectable: !hasAttr(ld, imap.MailboxAttrNoSelect), Sync: true}
		// Gmail shows every message in All Mail, Important and Starred too:
		// those are not synced (that would show everything twice).
		if role == "all" || role == "important" || role == "flagged" {
			f.Sync = false
		}
		if !f.Selectable {
			f.Sync = false
		}
		_, existed := byName[f.Name]
		if err := e.st.UpsertFolder(ctx, &f); err != nil {
			return nil, err
		}
		if !existed && !f.Sync {
			_ = e.st.SetFolderSyncDefault(ctx, f.ID, false)
		}
		onServer[f.Name] = true
	}
	for _, f := range have {
		// a folder made here for a move still on its way stays until the move lands
		if !onServer[f.Name] && !e.st.HasPendingIn(ctx, f.ID) {
			_ = e.st.DeleteFolder(ctx, f.ID)
		}
	}
	return e.st.AccountFolders(ctx, a.ID)
}

// CreateFolder makes a folder on the server and here.
func (e *Engine) CreateFolder(ctx context.Context, a store.Account, name string) (store.Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Folder{}, errors.New("give the folder a name")
	}
	if f, err := e.st.FolderByName(ctx, a.ID, name); err == nil {
		return f, nil
	}
	delim := "/"
	if fs, _ := e.st.AccountFolders(ctx, a.ID); len(fs) > 0 {
		delim = fs[0].Delimiter
	}
	if a.Provider != "demo" {
		err := e.withWork(ctx, &a, "", func(c *imapclient.Client) error {
			if err := c.Create(name, nil).Wait(); err != nil && !alreadyExists(err) {
				return err
			}
			_ = c.Subscribe(name).Wait()
			return nil
		})
		if err != nil {
			return store.Folder{}, err
		}
	}
	disp := name
	if i := strings.LastIndex(name, delim); i >= 0 {
		disp = name[i+1:]
	}
	f := store.Folder{UserID: a.UserID, AccountID: a.ID, Name: name, Display: disp, Delimiter: delim, Sync: true, Selectable: true}
	if err := e.st.UpsertFolder(ctx, &f); err != nil {
		return store.Folder{}, err
	}
	return e.st.GetFolder(ctx, 0, f.ID)
}

func alreadyExists(err error) bool {
	var ie *imap.Error
	if errors.As(err, &ie) && ie.Code == imap.ResponseCodeAlreadyExists {
		return true
	}
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "exists")
}

// ensureRoleFolder finds (or makes, locally first) the folder for a role:
// archive, trash, junk, sent. Gmail archives into All Mail.
func (e *Engine) ensureRoleFolder(ctx context.Context, a store.Account, role string) (store.Folder, bool, error) {
	if role == "archive" && a.Provider == "gmail" {
		if f, err := e.st.FolderByRole(ctx, a.ID, "all"); err == nil {
			return f, false, nil
		}
	}
	if f, err := e.st.FolderByRole(ctx, a.ID, role); err == nil {
		return f, false, nil
	}
	if role == "archive" {
		if f, err := e.st.FolderByRole(ctx, a.ID, "all"); err == nil {
			return f, false, nil
		}
	}
	name := map[string]string{"archive": "Archive", "trash": "Trash", "junk": "Junk", "sent": "Sent", "drafts": "Drafts"}[role]
	if name == "" {
		return store.Folder{}, false, errors.New("unknown folder")
	}
	if f, err := e.st.FolderByName(ctx, a.ID, name); err == nil {
		return f, false, nil
	}
	f := store.Folder{UserID: a.UserID, AccountID: a.ID, Name: name, Display: name, Delimiter: "/", Role: role, Sync: true, Selectable: true}
	if fs, _ := e.st.AccountFolders(ctx, a.ID); len(fs) > 0 {
		f.Delimiter = fs[0].Delimiter
	}
	if err := e.st.UpsertFolder(ctx, &f); err != nil {
		return store.Folder{}, false, err
	}
	got, err := e.st.GetFolder(ctx, 0, f.ID)
	return got, a.Provider != "demo", err
}
