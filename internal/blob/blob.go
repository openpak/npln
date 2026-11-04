// Package blob keeps a title's durable records (cloud saves, user-generated documents, fest
// entries) in Postgres at NPLN_DATABASE_URL, so one database backup covers them. One table,
// keyed by (title, kind, key). A pool of one connection: the database is shared and capped.
package blob

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schema = `
CREATE SCHEMA IF NOT EXISTS npln;
CREATE TABLE IF NOT EXISTS npln.blobs (
  title text NOT NULL,
  kind text NOT NULL,
  key text NOT NULL,
  data bytea NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (title, kind, key)
);`

// Store is one title's view of npln.blobs.
type Store struct {
	pool  *pgxpool.Pool
	title string
}

// Open connects to dsn and creates the table. Empty dsn returns (nil, nil): callers keep their files.
func Open(ctx context.Context, dsn, title string) (*Store, error) {
	if dsn == "" {
		return nil, nil
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns, cfg.MinConns, cfg.MaxConnIdleTime = 1, 0, 30*time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	tx, err := pool.Begin(ctx)
	if err == nil {
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7011)`); err == nil {
			if _, err = tx.Exec(ctx, schema); err == nil {
				err = tx.Commit(ctx)
			}
		}
	}
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool, title}, nil
}

var (
	once sync.Once
	def  *Store
)

// Default is the process-wide store for NPLN_TITLE at NPLN_DATABASE_URL, or nil without a DSN
// (or when Postgres is unreachable: the caller falls back to files and the log says so).
func Default() *Store {
	once.Do(func() {
		s, err := Open(context.Background(), os.Getenv("NPLN_DATABASE_URL"), os.Getenv("NPLN_TITLE"))
		if err != nil {
			log.Printf("[Blob] Postgres unavailable, durable records stay in files: %v", err)
			return
		}
		if s != nil {
			log.Printf("[Blob] durable records in Postgres (npln.blobs, title %s)", s.title)
		}
		def = s
	})
	return def
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// Get returns the record, or ok=false when there is none.
func (s *Store) Get(kind, key string) (data []byte, ok bool, err error) {
	c, cancel := ctx()
	defer cancel()
	err = s.pool.QueryRow(c, `SELECT data FROM npln.blobs WHERE title=$1 AND kind=$2 AND key=$3`, s.title, kind, key).Scan(&data)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	return data, err == nil, err
}

// Put writes the record, replacing any earlier one.
func (s *Store) Put(kind, key string, data []byte) error {
	c, cancel := ctx()
	defer cancel()
	_, err := s.pool.Exec(c, `INSERT INTO npln.blobs (title, kind, key, data) VALUES ($1,$2,$3,$4)
		ON CONFLICT (title, kind, key) DO UPDATE SET data=EXCLUDED.data, updated_at=now()`, s.title, kind, key, data)
	return err
}

// Import stores the record only when none exists yet (migration from a file, idempotent).
// It reports whether it inserted.
func (s *Store) Import(kind, key string, data []byte) (bool, error) {
	c, cancel := ctx()
	defer cancel()
	tag, err := s.pool.Exec(c, `INSERT INTO npln.blobs (title, kind, key, data) VALUES ($1,$2,$3,$4)
		ON CONFLICT DO NOTHING`, s.title, kind, key, data)
	return tag.RowsAffected() == 1, err
}

// DeleteKeys removes this title's records of the given kind under the given keys and reports how
// many rows went. Idempotent.
func (s *Store) DeleteKeys(kind string, keys []string) (int, error) {
	c, cancel := ctx()
	defer cancel()
	tag, err := s.pool.Exec(c, `DELETE FROM npln.blobs WHERE title=$1 AND kind=$2 AND key = ANY($3)`, s.title, kind, keys)
	return int(tag.RowsAffected()), err
}

// List returns every record of the kind for this title, by key.
func (s *Store) List(kind string) (map[string][]byte, error) {
	c, cancel := ctx()
	defer cancel()
	rows, err := s.pool.Query(c, `SELECT key, data FROM npln.blobs WHERE title=$1 AND kind=$2`, s.title, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var d []byte
		if err := rows.Scan(&k, &d); err != nil {
			return nil, err
		}
		out[k] = d
	}
	return out, rows.Err()
}

func (s *Store) Close() { s.pool.Close() }
