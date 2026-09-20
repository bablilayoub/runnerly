// Package storetest gives tests an isolated PostgreSQL schema.
//
// Go runs test packages in parallel. Sharing one schema between them means
// one package's setup truncates another's tables mid-test, which is exactly
// what happened before this existed: each suite passed alone and the three of
// them together did not. Every test here gets a schema of its own instead.
package storetest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/bablilayoub/runnerly/internal/store"
)

// DatabaseURLEnv points the tests at a PostgreSQL instance. Without it they
// skip, so `go test ./...` still works on a machine with no database.
const DatabaseURLEnv = "RUNNERLY_TEST_DATABASE_URL"

// skipMessage tells someone who has not set it up how to.
//
// The password in it belongs to a throwaway container the reader is being
// told to create, not to anything real.
const skipMessage = `set %s to run the tests that need PostgreSQL, for example` + //nolint:gosec // G101: example setup, not a credential
	`

  docker run -d --name runnerly-test-pg \
    -e POSTGRES_USER=runnerly -e POSTGRES_PASSWORD=runnerly \
    -e POSTGRES_DB=runnerly_test -p 55432:5432 postgres:16-alpine

  export %s='postgres://runnerly:runnerly@localhost:55432/runnerly_test?sslmode=disable'`

// New returns a store with its own freshly migrated schema, dropped when the
// test ends.
func New(t *testing.T) *store.Store {
	t.Helper()

	url := os.Getenv(DatabaseURLEnv)
	if url == "" {
		t.Skipf(skipMessage, DatabaseURLEnv, DatabaseURLEnv)
	}

	schema := SchemaName(t)
	ctx := context.Background()

	db, err := store.Open(ctx, store.Options{URL: url, Schema: schema})
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	// CREATE SCHEMA does not need the schema to exist on the search path, so
	// one connection can both create it and then work inside it.
	if err := db.CreateSchema(ctx, schema); err != nil {
		db.Close()
		t.Fatalf("create the test schema: %v", err)
	}

	t.Cleanup(func() {
		if err := db.DropSchema(context.Background(), schema); err != nil {
			t.Errorf("could not drop the test schema %s: %v", schema, err)
		}
		db.Close()
	})

	if _, err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// SchemaName returns a schema name unique to this test.
//
// The test's name makes a failure easy to trace back; the random suffix keeps
// two runs, or two subtests with the same name, from colliding.
func SchemaName(t *testing.T) string {
	t.Helper()

	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generate a schema name: %v", err)
	}

	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())

	// Postgres truncates identifiers at 63 bytes, so leave room for the
	// prefix and the suffix.
	if len(cleaned) > 40 {
		cleaned = cleaned[:40]
	}
	return "t_" + cleaned + "_" + hex.EncodeToString(b[:])
}
