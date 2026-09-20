// Package store is the control plane's PostgreSQL layer.
//
// It owns the schema, the migrations and every query. Nothing above it writes
// SQL, so the shape of the database stays one package's problem.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound means no row matched.
var ErrNotFound = errors.New("not found")

// Store holds the connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Options configures a Store.
type Options struct {
	// URL is the PostgreSQL connection string.
	URL string
	// Schema is the PostgreSQL schema Runnerly uses. Empty uses the
	// connection's own search path, which is normally "public".
	//
	// Setting it lets Runnerly share a database with something else, and it
	// is what gives each test its own isolated schema.
	Schema string
	// MaxConns caps the pool. Zero uses pgx's default.
	MaxConns int32
	// ConnectTimeout bounds the first connection attempt.
	ConnectTimeout time.Duration
}

// DefaultConnectTimeout bounds the initial connection so a misconfigured
// server fails fast with a clear message rather than hanging at boot.
const DefaultConnectTimeout = 10 * time.Second

// Open connects to PostgreSQL and verifies the connection.
func Open(ctx context.Context, opts Options) (*Store, error) {
	if opts.URL == "" {
		return nil, errors.New("no database URL.\n" +
			"Set database.url in the configuration or RUNNERLY_DATABASE_URL, for example\n" +
			"  postgres://runnerly:secret@localhost:5432/runnerly?sslmode=disable")
	}

	cfg, err := pgxpool.ParseConfig(opts.URL)
	if err != nil {
		// The URL may carry a password, so the parse error is reported
		// without echoing it back.
		return nil, fmt.Errorf("the database URL could not be parsed: %w", err)
	}
	if opts.MaxConns > 0 {
		cfg.MaxConns = opts.MaxConns
	}
	if opts.Schema != "" {
		if err := validateSchemaName(opts.Schema); err != nil {
			return nil, err
		}
		// A runtime parameter applies to every connection the pool opens,
		// including ones created later, which setting it once after connect
		// would miss.
		if cfg.ConnConfig.RuntimeParams == nil {
			cfg.ConnConfig.RuntimeParams = map[string]string{}
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = opts.Schema
	}

	timeout := opts.ConnectTimeout
	if timeout <= 0 {
		timeout = DefaultConnectTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL did not answer: %w.\n"+
			"Check that it is running and that the credentials in the URL are right", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool.
func (s *Store) Close() {
	if s.pool != nil {
		s.pool.Close()
	}
}

// Ping reports whether the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return nil
}

// Pool exposes the underlying pool for tests and migrations.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// schemaNamePattern is what may be used as a schema name. It is deliberately
// narrow: the name goes into SQL that cannot be parameterized, so it is
// validated rather than escaped.
var schemaNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func validateSchemaName(name string) error {
	if !schemaNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not a usable schema name.\n"+
			"Use lowercase letters, digits and underscores, starting with a letter or underscore", name)
	}
	return nil
}

// CreateSchema creates the configured schema if it is missing.
func (s *Store) CreateSchema(ctx context.Context, name string) error {
	if err := validateSchemaName(name); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+name); err != nil {
		return fmt.Errorf("create schema %s: %w", name, err)
	}
	return nil
}

// DropSchema removes a schema and everything in it.
func (s *Store) DropSchema(ctx context.Context, name string) error {
	if err := validateSchemaName(name); err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, `DROP SCHEMA IF EXISTS `+name+` CASCADE`); err != nil {
		return fmt.Errorf("drop schema %s: %w", name, err)
	}
	return nil
}

// wrap turns pgx's no-rows sentinel into the package's own, so callers do not
// have to import pgx to check for a miss.
func wrap(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// newID returns a random UUID (version 4).
//
// Runnerly generates its own rather than taking a dependency for it: a v4
// UUID is sixteen random bytes with six of them fixed, and crypto/rand is in
// the standard library.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on any platform Runnerly supports, and a
		// server that cannot produce random identifiers must not keep going
		// and hand out predictable ones.
		panic("runnerly: no randomness available: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out[:])
}
