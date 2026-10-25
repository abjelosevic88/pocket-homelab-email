package server

// TEMPORARY stubs — the Pocket app actions (replace this file).

import (
	"context"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func (s *Server) handleAppsStatus(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleRecipients(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSaveToApp(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMakeTask(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMakeEvent(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMakeContact(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMakeNote(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMakeExpense(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteLink(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleContactCard(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCalendarDay(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleBrowseFiles(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSearchDocuments(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleMoneyAccounts(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleAttachFromApp(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

// ruleAppActions does the parts of a matching rule that need other apps
// (save attachments, make a task, notify).
func (s *Server) ruleAppActions(ctx context.Context, m store.Message, r store.Rule) {}

// immichConn reaches a person's Immich (photos).
type immichConn struct{}

// immichFor connects to Immich through Core's service (or url/key typed in).
func (s *Server) immichFor(ctx context.Context, uid int64, url, key string) (*immichConn, error) {
	return nil, &notConnected{"not built yet"}
}

func (im *immichConn) ping(ctx context.Context) (string, error) { return "", nil }
