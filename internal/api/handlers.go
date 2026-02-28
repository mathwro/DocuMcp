package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/documcp/documcp/internal/db"
	"github.com/documcp/documcp/internal/search"
)

// writeJSON writes v as JSON to w with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Headers already sent; best-effort log only.
		_ = fmt.Errorf("write json: %w", err)
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
				_ = fmt.Errorf("background crawl source %d: %w", id, err)
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
