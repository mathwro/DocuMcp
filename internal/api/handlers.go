package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/documcp/documcp/internal/auth"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

// writeJSON writes v as JSON to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent; best-effort log only.
		slog.Error("write json", "err", err)
	}
}

// writeError writes a JSON error body with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs the full error and returns a generic 500 to the client,
// preventing internal details (file paths, SQL errors, etc.) from leaking.
func internalError(w http.ResponseWriter, op string, err error) {
	slog.Error(op, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// parseID reads the path parameter named "id" and returns it as int64.
// On parse failure it writes a 400 response and returns false.
func parseID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid id %q: %v", raw, err))
		return 0, false
	}
	return id, true
}

// sourceResponse wraps db.Source with a server-side Crawling flag.
type sourceResponse struct {
	db.Source
	Crawling bool `json:"Crawling"`
}

// listSources handles GET /api/sources.
// Returns a JSON array of all db.Source records, with a Crawling flag per source.
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources()
	if err != nil {
		internalError(w, "list sources", err)
		return
	}
	s.crawlingMu.Lock()
	result := make([]sourceResponse, len(sources))
	for i, src := range sources {
		result[i] = sourceResponse{Source: src, Crawling: s.crawlingIDs[src.ID]}
	}
	s.crawlingMu.Unlock()
	writeJSON(w, http.StatusOK, result)
}

// createSource handles POST /api/sources.
// Decodes a db.Source from the request body, inserts it, and returns 201 with the created source.
func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	// Cap request body to 1 MiB to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var src db.Source
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err).Error())
		return
	}

	id, err := s.store.InsertSource(src)
	if err != nil {
		internalError(w, "insert source", err)
		return
	}

	created, err := s.store.GetSource(id)
	if err != nil {
		internalError(w, "get source after insert", err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// deleteSource handles DELETE /api/sources/{id}.
// Deletes the source (and its pages via cascade) and returns 204 No Content.
func (s *Server) deleteSource(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	if _, err := s.store.GetSource(id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		internalError(w, "get source", err)
		return
	}

	if err := s.store.DeleteSource(id); err != nil {
		internalError(w, "delete source", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// triggerCrawl handles POST /api/sources/{id}/crawl.
// Starts a background crawl for the specified source and returns 202 Accepted.
func (s *Server) triggerCrawl(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	src, err := s.store.GetSource(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		internalError(w, "get source", err)
		return
	}

	if s.crawler != nil {
		s.crawlingMu.Lock()
		s.crawlingIDs[id] = true
		s.crawlingMu.Unlock()
		crawlCtx := s.ctx
		go func() {
			if err := s.crawler.Crawl(crawlCtx, *src); err != nil {
				slog.Error("background crawl failed", "source_id", id, "err", err)
			}
			s.crawlingMu.Lock()
			delete(s.crawlingIDs, id)
			s.crawlingMu.Unlock()
		}()
	}

	writeJSON(w, http.StatusAccepted, map[string]string{"status": "crawl started"})
}

// searchHandler handles GET /api/search?q=<query>.
// Runs an FTS5 full-text search and returns a JSON array of search.Result.
func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	results, err := search.FTS(s.store, q, 20)
	if err != nil {
		internalError(w, "search", err)
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// IsGitHubFlow reports whether the given source type should use the GitHub
// device code flow (as opposed to the Microsoft flow).
func IsGitHubFlow(sourceType string) bool {
	return sourceType == "github_wiki" || sourceType == "github_repo"
}

// authStart handles POST /api/sources/{id}/auth/start.
// Initiates a device code flow for the source and returns the user code and
// verification URI so the Web UI can display them to the user.
func (s *Server) authStart(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	src, err := s.store.GetSource(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		internalError(w, "get source", err)
		return
	}

	if IsGitHubFlow(src.Type) {
		writeError(w, http.StatusBadRequest,
			"github sources use PUT /api/sources/{id}/auth/token with a fine-grained personal access token")
		return
	}

	msFlow, err := auth.NewMicrosoftDeviceFlow("https://login.microsoftonline.com", "common")
	if err != nil {
		internalError(w, "start microsoft device flow", err)
		return
	}
	pf := pendingFlow{
		msFlow:   msFlow,
		provider: "microsoft",
	}
	s.flowMu.Lock()
	s.pendingFlows[id] = &pf
	s.flowMu.Unlock()

	slog.Info("auth start: Microsoft device flow initiated", "source_id", id)
	// device_code is a server-side secret; omit it from the client response.
	writeJSON(w, http.StatusOK, map[string]any{
		"user_code":        msFlow.UserCode,
		"verification_uri": msFlow.VerificationURI,
		"expires_in":       int(time.Until(msFlow.ExpiresAt).Seconds()),
	})
}

// authPoll handles GET /api/sources/{id}/auth/poll.
// Performs a single poll attempt on the pending device code flow.
// Returns 200 {"status":"ok"} when the token has been received and saved,
// 202 {"status":"pending"} while the user has not yet completed auth,
// or 400 {"status":"error","message":"..."} on terminal failure.
func (s *Server) authPoll(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	s.flowMu.Lock()
	pf := s.pendingFlows[id]
	s.flowMu.Unlock()

	if pf == nil {
		writeError(w, http.StatusNotFound, "no pending auth flow for this source")
		return
	}

	// Use a context that is slightly longer than the minimum poll interval (5 s)
	// so we give exactly one polling round-trip a chance to complete.
	pollCtx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()

	token, pollErr := pf.msFlow.Poll(pollCtx)

	if pollErr != nil {
		// A context deadline exceeded means the poll interval elapsed without a
		// response — the user simply hasn't completed the flow yet.
		if errors.Is(pollErr, context.DeadlineExceeded) || errors.Is(pollErr, context.Canceled) {
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "pending"})
			return
		}
		// Any other error is terminal (expired, denied, etc.).
		slog.Error("auth poll failed", "source_id", id, "err", pollErr)
		s.flowMu.Lock()
		delete(s.pendingFlows, id)
		s.flowMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"status":  "error",
			"message": pollErr.Error(),
		})
		return
	}

	// Token received — save it and remove the pending flow.
	if err := s.tokenStore.Save(id, pf.provider, token); err != nil {
		internalError(w, "save token", err)
		return
	}
	s.flowMu.Lock()
	delete(s.pendingFlows, id)
	s.flowMu.Unlock()

	slog.Info("auth poll: token saved", "source_id", id, "provider", pf.provider)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// authSetToken handles PUT /api/sources/{id}/auth/token.
// Stores a user-supplied personal access token (e.g. a fine-grained PAT) for
// the source's GitHub provider. Only github_wiki and github_repo sources are
// supported — Azure DevOps uses the Microsoft device-code flow instead.
func (s *Server) authSetToken(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	src, err := s.store.GetSource(id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		internalError(w, "get source", err)
		return
	}

	if !IsGitHubFlow(src.Type) {
		writeError(w, http.StatusBadRequest,
			"auth/token is only supported for github_wiki and github_repo sources")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<14) // 16 KiB is more than enough for a PAT
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err).Error())
		return
	}
	if body.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	token := &auth.Token{
		AccessToken: body.Token,
		// Fine-grained PATs have an expiry chosen by the user on creation; we
		// don't know it from the token itself. Leave zero — the crawler does
		// not consult ExpiresAt.
	}
	if err := s.tokenStore.Save(id, "github", token); err != nil {
		internalError(w, "save token", err)
		return
	}

	slog.Info("auth set token: saved", "source_id", id, "provider", "github")
	w.WriteHeader(http.StatusNoContent)
}

// authRevoke handles DELETE /api/sources/{id}/auth.
// Removes any stored token for the source and returns 204 No Content.
func (s *Server) authRevoke(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}

	if _, err := s.store.GetSource(id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "source not found")
			return
		}
		internalError(w, "get source", err)
		return
	}

	// Remove any pending in-memory flow for this source as well.
	s.flowMu.Lock()
	delete(s.pendingFlows, id)
	s.flowMu.Unlock()

	// Delete tokens for both possible providers. A real SQL error returns 500;
	// a no-op delete (row never existed) is fine and returns nil from DeleteToken.
	for _, provider := range []string{"microsoft", "github"} {
		if err := s.store.DeleteToken(id, provider); err != nil {
			internalError(w, "delete token", err)
			return
		}
	}

	slog.Info("auth revoke: completed", "source_id", id)
	w.WriteHeader(http.StatusNoContent)
}
