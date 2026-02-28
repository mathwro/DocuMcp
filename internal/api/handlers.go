package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/documcp/documcp/internal/auth"
	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

// githubClientID returns the GitHub OAuth app client ID to use for device flows.
// Falls back to the well-known public client ID if the env var is not set.
func githubClientID() string {
	if id := os.Getenv("GITHUB_CLIENT_ID"); id != "" {
		return id
	}
	return "178c6fc778ccc68e1d6a"
}

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

// listSources handles GET /api/sources.
// Returns a JSON array of all db.Source records.
func (s *Server) listSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.store.ListSources()
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("list sources: %w", err).Error())
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

// createSource handles POST /api/sources.
// Decodes a db.Source from the request body, inserts it, and returns 201 with the created source.
func (s *Server) createSource(w http.ResponseWriter, r *http.Request) {
	var src db.Source
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode body: %w", err).Error())
		return
	}

	id, err := s.store.InsertSource(src)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("insert source: %w", err).Error())
		return
	}

	created, err := s.store.GetSource(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("get source after insert: %w", err).Error())
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
		writeError(w, http.StatusInternalServerError, fmt.Errorf("get source: %w", err).Error())
		return
	}

	if err := s.store.DeleteSource(id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("delete source: %w", err).Error())
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
			writeError(w, http.StatusNotFound, fmt.Errorf("get source: %w", err).Error())
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Errorf("get source: %w", err).Error())
		return
	}

	if s.crawler != nil {
		go func() {
			if err := s.crawler.Crawl(context.Background(), *src); err != nil {
				slog.Error("background crawl failed", "source_id", id, "err", err)
			}
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
		writeError(w, http.StatusInternalServerError, fmt.Errorf("search: %w", err).Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
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
		writeError(w, http.StatusInternalServerError, fmt.Errorf("get source: %w", err).Error())
		return
	}

	var pf pendingFlow

	if src.Type == "github_wiki" {
		clientID := githubClientID()
		ghFlow, err := auth.NewGitHubDeviceFlow("https://github.com", clientID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("start github device flow: %w", err).Error())
			return
		}
		pf = pendingFlow{
			ghFlow:   ghFlow,
			provider: "github",
			clientID: clientID,
		}
		s.flowMu.Lock()
		s.pendingFlows[id] = &pf
		s.flowMu.Unlock()

		slog.Info("auth start: GitHub device flow initiated", "source_id", id)
		writeJSON(w, http.StatusOK, map[string]any{
			"user_code":        ghFlow.UserCode,
			"verification_uri": ghFlow.VerificationURI,
			"device_code":      ghFlow.DeviceCode,
			"expires_in":       int(time.Until(ghFlow.ExpiresAt).Seconds()),
		})
	} else {
		msFlow, err := auth.NewMicrosoftDeviceFlow("https://login.microsoftonline.com", "common")
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("start microsoft device flow: %w", err).Error())
			return
		}
		pf = pendingFlow{
			msFlow:   msFlow,
			provider: "microsoft",
		}
		s.flowMu.Lock()
		s.pendingFlows[id] = &pf
		s.flowMu.Unlock()

		slog.Info("auth start: Microsoft device flow initiated", "source_id", id)
		writeJSON(w, http.StatusOK, map[string]any{
			"user_code":        msFlow.UserCode,
			"verification_uri": msFlow.VerificationURI,
			"device_code":      msFlow.DeviceCode,
			"expires_in":       int(time.Until(msFlow.ExpiresAt).Seconds()),
		})
	}
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

	var token *auth.Token
	var pollErr error

	if pf.ghFlow != nil {
		token, pollErr = pf.ghFlow.Poll(pollCtx, pf.clientID)
	} else {
		token, pollErr = pf.msFlow.Poll(pollCtx)
	}

	if pollErr != nil {
		// A context deadline exceeded means the poll interval elapsed without a
		// response — the user simply hasn't completed the flow yet.
		if errors.Is(pollErr, context.DeadlineExceeded) {
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
		writeError(w, http.StatusInternalServerError, fmt.Errorf("save token: %w", err).Error())
		return
	}
	s.flowMu.Lock()
	delete(s.pendingFlows, id)
	s.flowMu.Unlock()

	slog.Info("auth poll: token saved", "source_id", id, "provider", pf.provider)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		writeError(w, http.StatusInternalServerError, fmt.Errorf("get source: %w", err).Error())
		return
	}

	// Remove any pending in-memory flow for this source as well.
	s.flowMu.Lock()
	delete(s.pendingFlows, id)
	s.flowMu.Unlock()

	// Delete tokens for both possible providers — ignore "not found" for each.
	for _, provider := range []string{"microsoft", "github"} {
		if err := s.store.DeleteToken(id, provider); err != nil {
			slog.Error("delete token", "source_id", id, "provider", provider, "err", err)
		}
	}

	slog.Info("auth revoke: tokens deleted", "source_id", id)
	w.WriteHeader(http.StatusNoContent)
}
