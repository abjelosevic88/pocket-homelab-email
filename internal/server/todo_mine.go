package server

// TEMPORARY stubs (core mail API) — replaced as the real handlers land.

import (
	"context"
	"net/http"

	"github.com/abjelosevic88/pocket-homelab-email/internal/mail"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

func (s *Server) handleSidebar(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleTestAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleUpdateAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSyncAccount(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleAccountFolders(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleOAuthStart(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleOAuthCallback(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleRawMessage(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleThread(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleUndo(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListAttachments(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleGetDraft(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleUpdateDraft(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteDraft(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleAddDraftFile(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleGetDraftFile(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteDraftFile(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSendDraft(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleCancelDraft(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSendDirect(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListSenders(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handlePutSender(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleUnsubscribe(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleArchiveSender(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSaveTemplate(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteTemplate(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleSaveRule(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotImplemented, "not built yet") }

func (s *Server) oauthClients(ctx context.Context) mail.OAuthClients { return mail.OAuthClients{} }
func (s *Server) mailChanged(uid, accountID int64, newIDs []int64)   {}
func (s *Server) mailSent(d store.Draft, err error)                   {}
func (s *Server) mailArrived(m store.Message)                        {}

type hub struct{}

func newHub() *hub { return &hub{} }

func (s *Server) oauthReady(ctx context.Context, provider string) bool { return false }
func (s *Server) oauthRedirect(provider string) string                 { return "" }

func (s *Server) seedDemo(ctx context.Context, uid int64) error { return nil }
