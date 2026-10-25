package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-email/internal/store"
)

// Router builds the chi router.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	if s.cfg.TrustProxy {
		r.Use(middleware.RealIP)
	}
	r.Use(middleware.RequestID)
	r.Use(s.logging)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	r.Get("/pocket-ask.js", s.core.AskWidget("mail"))
	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)
	r.Get("/api/health", s.handleHealth)
	if s.cfg.MetricsEnabled {
		r.Get("/metrics", s.handleMetrics)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(120 * time.Second))
		r.Post("/auth/login", s.handleLogin)
		r.Post("/auth/setup", s.handleSetup)
		r.Get("/auth/status", s.handleAuthStatus)
		r.Get("/auth/oidc/start", s.handleOIDCStart)
		r.Get("/auth/oidc/callback", s.handleOIDCCallback)

		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)
			r.Post("/auth/logout", s.handleLogout)
			r.Get("/auth/me", s.handleMe)
			r.Put("/auth/me", s.handleUpdateMe)

			// the sidebar: accounts with folders, the numbers next to the views
			r.Get("/sidebar", s.handleSidebar)
			r.Get("/counts", s.handleCounts)
			r.Get("/summary", s.handleSummary)
			r.Get("/summary/{date}", s.handleSummary)
			r.Get("/events", s.handleEvents) // live updates (server-sent events)

			// accounts (accounts_api.go)
			r.Get("/providers", s.handleProviders)
			r.Get("/accounts/discover", s.handleDiscover)
			r.Post("/accounts/test", s.handleTestAccount)
			r.Get("/accounts", s.handleListAccounts)
			r.Post("/accounts", s.handleCreateAccount)
			r.Get("/accounts/{id}", s.handleGetAccount)
			r.Put("/accounts/{id}", s.handleUpdateAccount)
			r.Delete("/accounts/{id}", s.handleDeleteAccount)
			r.Post("/accounts/{id}/sync", s.handleSyncAccount)
			r.Get("/accounts/{id}/folders", s.handleAccountFolders)
			r.Post("/accounts/{id}/folders", s.handleCreateFolder)
			r.Put("/folders/{id}", s.handleUpdateFolder)
			r.Get("/oauth/{provider}/start", s.handleOAuthStart)
			r.Get("/oauth/{provider}/callback", s.handleOAuthCallback)

			// messages and conversations (messages_api.go)
			r.Get("/messages", s.handleListMessages)
			r.Get("/messages/{id}", s.handleGetMessage)
			r.Get("/messages/{id}/raw", s.handleRawMessage)
			r.Get("/threads/{id}", s.handleThread)
			r.Post("/actions", s.handleActions)
			r.Post("/undo", s.handleUndo)
			r.Get("/attachments", s.handleListAttachments)
			r.Get("/attachments/{id}", s.handleAttachment)

			// writing (compose_api.go)
			r.Post("/compose", s.handleCompose)
			r.Get("/drafts", s.handleListDrafts)
			r.Get("/drafts/{id}", s.handleGetDraft)
			r.Put("/drafts/{id}", s.handleUpdateDraft)
			r.Delete("/drafts/{id}", s.handleDeleteDraft)
			r.Post("/drafts/{id}/files", s.handleAddDraftFile)
			r.Get("/drafts/{id}/files/{fid}", s.handleGetDraftFile)
			r.Delete("/drafts/{id}/files/{fid}", s.handleDeleteDraftFile)
			r.Post("/drafts/{id}/send", s.handleSendDraft)
			r.Post("/drafts/{id}/cancel", s.handleCancelDraft)
			r.Post("/send", s.handleSendDirect) // one call: another app (mail:send) or a script

			// senders, subscriptions, templates, rules (extras_api.go)
			r.Get("/senders", s.handleListSenders)
			r.Put("/senders/{addr}", s.handlePutSender)
			r.Get("/subscriptions", s.handleSubscriptions)
			r.Post("/subscriptions/unsubscribe", s.handleUnsubscribe)
			r.Post("/subscriptions/archive", s.handleArchiveSender)
			r.Get("/templates", s.handleListTemplates)
			r.Post("/templates", s.handleSaveTemplate)
			r.Put("/templates/{id}", s.handleSaveTemplate)
			r.Delete("/templates/{id}", s.handleDeleteTemplate)
			r.Get("/rules", s.handleListRules)
			r.Post("/rules", s.handleSaveRule)
			r.Put("/rules/{id}", s.handleSaveRule)
			r.Delete("/rules/{id}", s.handleDeleteRule)

			// the Pocket apps (apps_api.go)
			r.Get("/apps", s.handleAppsStatus)
			r.Get("/recipients", s.handleRecipients)
			r.Post("/messages/{id}/save", s.handleSaveToApp)
			r.Post("/messages/{id}/task", s.handleMakeTask)
			r.Post("/messages/{id}/event", s.handleMakeEvent)
			r.Post("/messages/{id}/contact", s.handleMakeContact)
			r.Post("/messages/{id}/note", s.handleMakeNote)
			r.Post("/messages/{id}/expense", s.handleMakeExpense)
			r.Delete("/links/{id}", s.handleDeleteLink)
			r.Get("/apps/contacts/card", s.handleContactCard)
			r.Get("/apps/calendar/day", s.handleCalendarDay)
			r.Get("/apps/files/browse", s.handleBrowseFiles)
			r.Get("/apps/documents/search", s.handleSearchDocuments)
			r.Get("/apps/money/accounts", s.handleMoneyAccounts)
			r.Post("/drafts/{id}/files/from", s.handleAttachFromApp)

			// AI (ai.go, ai_mail.go)
			r.Get("/ai/status", s.handleAIStatus)
			r.Get("/ai/models", s.handleAIModels)
			r.Post("/ai/summarize", s.handleAISummarize)
			r.Post("/ai/reply", s.handleAIReply)
			r.Post("/ai/write", s.handleAIWrite)
			r.Post("/ai/suggest", s.handleAISuggest)
			r.Post("/ai/extract", s.handleAIExtract)
			r.Post("/ai/digest", s.handleAIDigest)
			r.Post("/ai/ask", s.handleAsk)

			r.Post("/integrations/test", s.handleIntegrationTest)
			r.Post("/notify/test", s.handleNotifyTest)

			r.Get("/users", s.handleListUsers)
			r.Post("/users", s.handleCreateUser)
			r.Put("/users/{id}", s.handleUpdateUser)
			r.Delete("/users/{id}", s.handleDeleteUser)

			r.Get("/settings", s.handleGetSettings)
			r.Put("/settings", s.handlePutSettings)
			r.Get("/settings/system", s.handleSystemInfo)

			r.Get("/tokens", s.handleListTokens)
			r.Post("/tokens", s.handleCreateToken)
			r.Delete("/tokens/{id}", s.handleDeleteToken)

			r.Get("/backup", s.handleBackup)
			r.Get("/export.json", s.handleExportJSON)
			r.Get("/export.csv", s.handleExportCSV)
			r.Post("/import", s.handleImportJSON)
		})
	})

	r.NotFound(s.handleSPA)
	return r
}

// ---------- helpers ----------

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		s.requests.Add(1)
		if ww.Status() >= 500 {
			s.errors.Add(1)
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			return
		}
		s.log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "dur", time.Since(start).String(), "ip", r.RemoteAddr)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

func (s *Server) fail(w http.ResponseWriter, err error, context string) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
		return
	case errors.Is(err, store.ErrConflict), errors.Is(err, core.ErrOperationConflict):
		writeErr(w, http.StatusConflict, "already exists")
		return
	}
	s.log.Error(context, "err", err)
	writeErr(w, http.StatusInternalServerError, context+": "+err.Error())
}

func (s *Server) readBody(r *http.Request) ([]byte, error) {
	limit := s.cfg.MaxBodyBytes
	if limit <= 0 {
		limit = 32 << 20
	}
	return io.ReadAll(http.MaxBytesReader(nil, r.Body, limit))
}

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 32<<20))
	return dec.Decode(v)
}

// decodeKeys decodes JSON into v and also reports which top-level keys were
// present, so PUT handlers can tell "not sent" from "sent empty".
func decodeKeys(r *http.Request, v any) (map[string]bool, error) {
	b, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, json.Unmarshal(b, v)
}

func idParam(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return n
}

func queryInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(name))
	return n
}

func queryBool(r *http.Request, name string) bool {
	v := strings.ToLower(r.URL.Query().Get(name))
	return v == "1" || v == "true" || v == "yes"
}

func validDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- health / metrics ----------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"core_outbox": s.coreOutboxHealth(r.Context()), "status": "ok", "version": Version, "uptime": time.Since(s.started).Round(time.Second).String()})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DB.PingContext(r.Context()); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts := s.store.Counts(ctx)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP pocket_mail_info Build info.\n# TYPE pocket_mail_info gauge\npocket_mail_info{version=%q} 1\n", Version)
	fmt.Fprintf(w, "# HELP pocket_mail_uptime_seconds Process uptime.\n# TYPE pocket_mail_uptime_seconds gauge\npocket_mail_uptime_seconds %d\n", int(time.Since(s.started).Seconds()))
	fmt.Fprintf(w, "# HELP pocket_mail_http_requests_total HTTP requests served.\n# TYPE pocket_mail_http_requests_total counter\npocket_mail_http_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP pocket_mail_http_errors_total HTTP 5xx responses.\n# TYPE pocket_mail_http_errors_total counter\npocket_mail_http_errors_total %d\n", s.errors.Load())
	fmt.Fprintf(w, "# HELP pocket_mail_rows Rows per table.\n# TYPE pocket_mail_rows gauge\n")
	for k, v := range counts {
		fmt.Fprintf(w, "pocket_mail_rows{table=%q} %d\n", k, v)
	}
	fmt.Fprintf(w, "# HELP pocket_mail_sent_total Messages sent since start.\n# TYPE pocket_mail_sent_total counter\npocket_mail_sent_total %d\n", s.saved.Load())
}

// ---------- static / SPA ----------

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	if s.webFS == nil {
		http.Error(w, "web UI not built (run `make web`)", http.StatusNotFound)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := s.webFS.Open(path); err == nil {
		f.Close()
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		http.FileServerFS(s.webFS).ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.webFS).ServeHTTP(w, r)
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
