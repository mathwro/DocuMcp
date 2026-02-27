package db

// FTS5 support requires compiling with -tags sqlite_fts5 (which enables
// -DSQLITE_ENABLE_FTS5 in go-sqlite3's C layer).
//
// All build and test commands in this project pass -tags sqlite_fts5:
//   CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
//   CGO_ENABLED=1 go test  -tags sqlite_fts5 ./...
//
// See the Makefile for the canonical invocations.
