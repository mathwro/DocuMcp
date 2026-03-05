package db

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	vec.Auto() // registers the sqlite-vec extension for all new connections
}

// ErrNotFound is returned when a requested record does not exist.
var ErrNotFound = errors.New("not found")

// Store wraps a SQLite database and exposes DocuMcp data operations.
type Store struct {
	db *sql.DB
}

// Source represents a configured documentation source.
type Source struct {
	ID            int64
	Name          string
	Type          string
	URL           string
	Repo          string
	BaseURL       string
	SpaceKey      string
	Auth          string
	CrawlSchedule string
	LastCrawled   *time.Time
	PageCount     int
	CrawlTotal    int
}

// Page represents an indexed documentation page.
type Page struct {
	ID       int64
	SourceID int64
	URL      string
	Title    string
	Content  string
	Path     []string
}

// Open opens (or creates) a SQLite database at dsn and applies the schema.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite3", dsn+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	// Migrations for columns added after initial release.
	_, _ = db.Exec(`ALTER TABLE sources ADD COLUMN crawl_total INTEGER NOT NULL DEFAULT 0`)
	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying *sql.DB for packages that need direct access (e.g. search).
func (s *Store) DB() *sql.DB { return s.db }

// InsertSource inserts a new source and returns its ID.
func (s *Store) InsertSource(src Source) (int64, error) {
	res, err := s.db.Exec(
		`INSERT INTO sources (name, type, url, repo, base_url, space_key, auth, crawl_schedule)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		src.Name, src.Type, src.URL, src.Repo, src.BaseURL, src.SpaceKey, src.Auth, src.CrawlSchedule,
	)
	if err != nil {
		return 0, fmt.Errorf("insert source: %w", err)
	}
	return res.LastInsertId()
}

// ListSources returns all configured sources.
func (s *Store) ListSources() ([]Source, error) {
	rows, err := s.db.Query(
		`SELECT id, name, type, url, repo, base_url, space_key, auth, crawl_schedule, page_count, last_crawled, crawl_total
		 FROM sources ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	sources := make([]Source, 0)
	for rows.Next() {
		var src Source
		if err := rows.Scan(
			&src.ID, &src.Name, &src.Type, &src.URL, &src.Repo,
			&src.BaseURL, &src.SpaceKey, &src.Auth, &src.CrawlSchedule, &src.PageCount, &src.LastCrawled, &src.CrawlTotal,
		); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// GetSource returns a single source by ID.
func (s *Store) GetSource(id int64) (*Source, error) {
	var src Source
	err := s.db.QueryRow(
		`SELECT id, name, type, url, repo, base_url, space_key, auth, crawl_schedule, page_count, last_crawled, crawl_total
		 FROM sources WHERE id = ?`, id,
	).Scan(
		&src.ID, &src.Name, &src.Type, &src.URL, &src.Repo,
		&src.BaseURL, &src.SpaceKey, &src.Auth, &src.CrawlSchedule, &src.PageCount, &src.LastCrawled, &src.CrawlTotal,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("source %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get source %d: %w", id, err)
	}
	return &src, nil
}

// GetSourceIDByName returns the ID of a source with the given name.
func (s *Store) GetSourceIDByName(name string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM sources WHERE name = ?`, name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("source %q: %w", name, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("get source by name %q: %w", name, err)
	}
	return id, nil
}

// DeleteSource deletes a source and all its pages (cascade).
func (s *Store) DeleteSource(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sources WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete source %d: %w", id, err)
	}
	return nil
}

// UpdateSourcePageCount updates the page_count for a source.
func (s *Store) UpdateSourcePageCount(id int64, count int) error {
	_, err := s.db.Exec(
		`UPDATE sources SET page_count = ?, last_crawled = CURRENT_TIMESTAMP WHERE id = ?`,
		count, id,
	)
	if err != nil {
		return fmt.Errorf("update source page count %d: %w", id, err)
	}
	return nil
}

// UpdateSourceCrawlTotal sets the total number of URLs discovered for a crawl run.
func (s *Store) UpdateSourceCrawlTotal(id int64, total int) error {
	_, err := s.db.Exec(
		`UPDATE sources SET crawl_total = ? WHERE id = ?`,
		total, id,
	)
	if err != nil {
		return fmt.Errorf("update source crawl total %d: %w", id, err)
	}
	return nil
}

// UpsertPage inserts or updates a page by URL and returns the page's row ID.
func (s *Store) UpsertPage(p Page) (int64, error) {
	pathJSON, err := json.Marshal(p.Path)
	if err != nil {
		return 0, fmt.Errorf("marshal path: %w", err)
	}
	var id int64
	err = s.db.QueryRow(
		`INSERT INTO pages (source_id, url, title, content, path)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(url) DO UPDATE SET
		   title      = excluded.title,
		   content    = excluded.content,
		   path       = excluded.path,
		   updated_at = CURRENT_TIMESTAMP
		 RETURNING id`,
		p.SourceID, p.URL, p.Title, p.Content, string(pathJSON),
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert page %q: %w", p.URL, err)
	}
	return id, nil
}

// GetPageByURL returns a page by its URL.
func (s *Store) GetPageByURL(url string) (*Page, error) {
	var p Page
	var pathJSON string
	err := s.db.QueryRow(
		`SELECT id, source_id, url, title, content, path FROM pages WHERE url = ?`, url,
	).Scan(&p.ID, &p.SourceID, &p.URL, &p.Title, &p.Content, &pathJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("page %q: %w", url, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("get page %q: %w", url, err)
	}
	if err := json.Unmarshal([]byte(pathJSON), &p.Path); err != nil {
		return nil, fmt.Errorf("unmarshal path: %w", err)
	}
	return &p, nil
}

// UpsertToken stores encrypted token data for a source+provider pair.
func (s *Store) UpsertToken(sourceID int64, provider string, data []byte, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO tokens (source_id, provider, data, expires_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(source_id, provider) DO UPDATE SET
		   data = excluded.data, expires_at = excluded.expires_at`,
		sourceID, provider, data, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("upsert token (source=%d, provider=%s): %w", sourceID, provider, err)
	}
	return nil
}

// GetToken retrieves encrypted token data for a source+provider pair.
// Returns ErrNotFound if no token exists for the given source+provider.
func (s *Store) GetToken(sourceID int64, provider string) ([]byte, error) {
	var data []byte
	err := s.db.QueryRow(
		`SELECT data FROM tokens WHERE source_id = ? AND provider = ?`, sourceID, provider,
	).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}
	return data, nil
}

// DeleteToken removes the stored token for the given source+provider pair.
// Returns nil if the token did not exist.
func (s *Store) DeleteToken(sourceID int64, provider string) error {
	_, err := s.db.Exec(
		`DELETE FROM tokens WHERE source_id = ? AND provider = ?`, sourceID, provider,
	)
	if err != nil {
		return fmt.Errorf("delete token (source=%d, provider=%s): %w", sourceID, provider, err)
	}
	return nil
}

// Float32ToBlob serialises a float32 slice as little-endian bytes for sqlite-vec.
// Exported so the search package can use it for query vectors too.
func Float32ToBlob(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(b[i*4:], bits)
	}
	return b
}

// UpsertEmbedding inserts or replaces the embedding vector for a page.
// sqlite-vec vec0 virtual tables do not support INSERT OR REPLACE, so we
// delete any existing row first and then insert.
func (s *Store) UpsertEmbedding(pageID int64, embedding []float32) error {
	if _, err := s.db.Exec(
		`DELETE FROM page_embeddings WHERE page_id = ?`, pageID,
	); err != nil {
		return fmt.Errorf("upsert embedding (delete): %w", err)
	}
	if _, err := s.db.Exec(
		`INSERT INTO page_embeddings(page_id, embedding) VALUES (?, ?)`,
		pageID, Float32ToBlob(embedding),
	); err != nil {
		return fmt.Errorf("upsert embedding (insert): %w", err)
	}
	return nil
}
