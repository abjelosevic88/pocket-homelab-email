package mail

import (
	"context"
	"strings"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func containsFold(hay, needle string) bool {
	return strings.Contains(strings.ToLower(hay), strings.ToLower(strings.TrimSpace(needle)))
}

// MatchRule reports whether a rule applies to a message: every part given
// must match (substrings, any case).
func MatchRule(r store.Rule, m store.Message) bool {
	if !r.Enabled {
		return false
	}
	if r.AccountID != nil && *r.AccountID != m.AccountID {
		return false
	}
	mt := r.Match
	if mt.From == "" && mt.To == "" && mt.Subject == "" && !mt.HasAttachment && mt.Category == "" {
		return false // a rule that matches everything is a mistake
	}
	if mt.From != "" && !containsFold(m.From.Addr, mt.From) && !containsFold(m.From.Name, mt.From) {
		return false
	}
	if mt.To != "" {
		ok := false
		for _, a := range append(append([]store.Address{}, m.To...), m.Cc...) {
			if containsFold(a.Addr, mt.To) || containsFold(a.Name, mt.To) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if mt.Subject != "" && !containsFold(m.Subject, mt.Subject) {
		return false
	}
	if mt.HasAttachment && !m.HasAttachments {
		return false
	}
	if mt.Category != "" && mt.Category != m.Category {
		return false
	}
	return true
}

// runRules applies the local actions of the person's matching rules to a
// new message (the app does the rest: saving files, tasks, notifications).
func (e *Engine) runRules(ctx context.Context, a *store.Account, m *store.Message) {
	rules, err := e.st.ListRules(ctx, a.UserID, true)
	if err != nil {
		return
	}
	for _, r := range rules {
		if !MatchRule(r, *m) {
			continue
		}
		_ = e.st.RuleHit(ctx, r.ID)
		act := r.Actions
		if act.Category != "" && act.Category != m.Category {
			if e.st.SetCategory(ctx, a.UserID, m.ID, act.Category) == nil {
				m.Category = act.Category
			}
		}
		if act.MarkRead && !m.Seen {
			_, _ = e.apply(ctx, a.UserID, []int64{m.ID}, Action{Kind: "read"}, 0)
		}
		if act.Star && !m.Flagged {
			_, _ = e.apply(ctx, a.UserID, []int64{m.ID}, Action{Kind: "star"}, 0)
		}
		switch {
		case act.MoveTo != "":
			if f, err := e.st.FolderByName(ctx, a.ID, act.MoveTo); err == nil {
				_, _ = e.apply(ctx, a.UserID, []int64{m.ID}, Action{Kind: "move", FolderID: f.ID}, 0)
			}
		case act.Archive:
			_, _ = e.apply(ctx, a.UserID, []int64{m.ID}, Action{Kind: "archive"}, 0)
		}
	}
}
