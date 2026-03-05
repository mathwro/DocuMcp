package db

const schema = `
CREATE TABLE IF NOT EXISTS sources (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    name           TEXT NOT NULL,
    type           TEXT NOT NULL,
    url            TEXT NOT NULL DEFAULT '',
    repo           TEXT NOT NULL DEFAULT '',
    base_url       TEXT NOT NULL DEFAULT '',
    space_key      TEXT NOT NULL DEFAULT '',
    auth           TEXT NOT NULL DEFAULT '',
    crawl_schedule TEXT NOT NULL DEFAULT '',
    last_crawled   DATETIME,
    page_count     INTEGER NOT NULL DEFAULT 0,
    crawl_total    INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pages (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    url        TEXT NOT NULL UNIQUE,
    title      TEXT NOT NULL,
    content    TEXT NOT NULL,
    path       TEXT NOT NULL DEFAULT '[]',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE IF NOT EXISTS pages_fts USING fts5(
    title,
    content,
    content='pages',
    content_rowid='id'
);

CREATE TRIGGER IF NOT EXISTS pages_ai AFTER INSERT ON pages BEGIN
    INSERT INTO pages_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

CREATE TRIGGER IF NOT EXISTS pages_au AFTER UPDATE ON pages BEGIN
    INSERT INTO pages_fts(pages_fts, rowid, title, content) VALUES('delete', old.id, old.title, old.content);
    INSERT INTO pages_fts(rowid, title, content) VALUES (new.id, new.title, new.content);
END;

CREATE TRIGGER IF NOT EXISTS pages_ad AFTER DELETE ON pages BEGIN
    INSERT INTO pages_fts(pages_fts, rowid, title, content) VALUES('delete', old.id, old.title, old.content);
END;

CREATE TABLE IF NOT EXISTS tokens (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id  INTEGER REFERENCES sources(id) ON DELETE CASCADE,
    provider   TEXT NOT NULL,
    data       BLOB NOT NULL,
    expires_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(source_id, provider)
);

CREATE TABLE IF NOT EXISTS crawl_jobs (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    source_id      INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    status         TEXT NOT NULL DEFAULT 'pending',
    started_at     DATETIME,
    finished_at    DATETIME,
    pages_crawled  INTEGER NOT NULL DEFAULT 0,
    error          TEXT
);

-- NOTE: all-MiniLM-L6-v2 is trained for cosine similarity, but this version of
-- sqlite-vec does not support distance_metric=cosine in the vec0 constructor.
-- To approximate cosine similarity, L2-normalise all vectors before storing them;
-- L2 distance on unit vectors is monotonically equivalent to cosine distance.
CREATE VIRTUAL TABLE IF NOT EXISTS page_embeddings USING vec0(
    page_id INTEGER PRIMARY KEY,
    embedding FLOAT[384]
);
`
