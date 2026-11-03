// Package counted keeps which game sessions were already reported to rewards, so a restart
// neither forgets a counted session nor reports it twice. Postgres when a DSN is given (a pool
// of two short connections, the database is shared and capped); otherwise an append-only file
// on the state volume. Never memory only.
package counted

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store remembers counted (title, key) pairs.
type Store interface {
	// Load returns every key counted for the title within the last 30 days.
	Load(title string) (map[string]bool, error)
	// Mark records a key as counted.
	Mark(title, key string) error
	Close()
}

const schema = `
CREATE SCHEMA IF NOT EXISTS npln;
CREATE TABLE IF NOT EXISTS npln.counted_sessions (
  title text NOT NULL,
  key text NOT NULL,
  counted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (title, key)
);`

type pg struct{ pool *pgxpool.Pool }

// Open returns the Postgres store for dsn, or the file store under dir when dsn is empty.
func Open(ctx context.Context, dsn, dir string) (Store, error) {
	if dsn == "" {
		return &file{path: filepath.Join(dir, "counted-sessions.log")}, nil
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns, cfg.MinConns, cfg.MaxConnIdleTime = 2, 0, 30*time.Second
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
	return &pg{pool}, nil
}

func (p *pg) Load(title string) (map[string]bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = p.pool.Exec(ctx, `DELETE FROM npln.counted_sessions WHERE title=$1 AND counted_at < now() - interval '30 days'`, title)
	rows, err := p.pool.Query(ctx, `SELECT key FROM npln.counted_sessions WHERE title=$1`, title)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out[k] = true
	}
	return out, rows.Err()
}

func (p *pg) Mark(title, key string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := p.pool.Exec(ctx, `INSERT INTO npln.counted_sessions (title, key) VALUES ($1,$2) ON CONFLICT DO NOTHING`, title, key)
	return err
}

func (p *pg) Close() { p.pool.Close() }

// file is one "title<TAB>key" line per counted session.
type file struct {
	mu   sync.Mutex
	path string
}

func (f *file) Load(title string) (map[string]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]bool{}
	fh, err := os.Open(f.path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		if t, k, ok := strings.Cut(sc.Text(), "\t"); ok && t == title {
			out[k] = true
		}
	}
	return out, sc.Err()
}

func (f *file) Mark(title, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(f.path), 0o755); err != nil {
		return err
	}
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.WriteString(title + "\t" + key + "\n")
	return err
}

func (f *file) Close() {}
