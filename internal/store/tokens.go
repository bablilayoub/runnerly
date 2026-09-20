package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token prefixes make a leaked credential identifiable at a glance, and let
// the server reject one that was pasted into the wrong field.
const (
	// EnrollmentPrefix marks a token an operator hands to a machine.
	EnrollmentPrefix = "rnr_enroll_"
	// MachinePrefix marks the credential an enrolled agent uses.
	MachinePrefix = "rnr_machine_"
	// SessionPrefix marks a dashboard session cookie.
	SessionPrefix = "rnr_session_"
)

// tokenBytes is the entropy behind every token. 32 bytes is well past what a
// bearer credential needs to be unguessable.
const tokenBytes = 32

// ErrTokenInvalid means the credential does not match anything usable.
var ErrTokenInvalid = errors.New("the token is not valid")

// ErrTokenExhausted means the token was real but may no longer be used.
var ErrTokenExhausted = errors.New("the token has expired, been revoked, or been used too many times")

// NewToken returns a random token with the given prefix.
func NewToken(prefix string) (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the value stored in the database.
//
// Tokens are hashed, not encrypted. The server never needs to read one back:
// it only needs to recognize one presented to it, and a hash cannot be turned
// back into a working credential by anyone who steals a database dump.
// SHA-256 is the right primitive here rather than a password hash, because
// the input is 32 bytes of randomness and not something guessable.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CreateEnrollmentToken issues a token an operator can hand to a machine. The
// secret is returned once and never stored.
func (s *Store) CreateEnrollmentToken(ctx context.Context, description string, maxUses *int, expiresAt *time.Time) (EnrollmentToken, string, error) {
	secret, err := NewToken(EnrollmentPrefix)
	if err != nil {
		return EnrollmentToken{}, "", err
	}

	var t EnrollmentToken
	row := s.pool.QueryRow(ctx, `
		INSERT INTO enrollment_tokens (id, token_hash, description, max_uses, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, description, max_uses, uses, expires_at, revoked_at, created_at`,
		newID(), HashToken(secret), description, maxUses, expiresAt)

	if err := row.Scan(&t.ID, &t.Description, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
		return EnrollmentToken{}, "", fmt.Errorf("create enrollment token: %w", wrap(err))
	}
	return t, secret, nil
}

// RedeemEnrollmentToken checks a token and counts one use against it.
//
// The check and the increment happen in one statement so two machines
// enrolling at the same moment cannot both slip past a max_uses of one.
func (s *Store) RedeemEnrollmentToken(ctx context.Context, token string) (EnrollmentToken, error) {
	if !strings.HasPrefix(token, EnrollmentPrefix) {
		return EnrollmentToken{}, ErrTokenInvalid
	}

	var t EnrollmentToken
	row := s.pool.QueryRow(ctx, `
		UPDATE enrollment_tokens SET uses = uses + 1
		WHERE token_hash = $1
		  AND revoked_at IS NULL
		  AND (expires_at IS NULL OR expires_at > now())
		  AND (max_uses IS NULL OR uses < max_uses)
		RETURNING id, description, max_uses, uses, expires_at, revoked_at, created_at`,
		HashToken(token))

	err := row.Scan(&t.ID, &t.Description, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if errors.Is(wrap(err), ErrNotFound) {
		// The row may be missing, revoked, expired or used up. Distinguish
		// the last three so an operator is not left guessing, but say nothing
		// at all about a token that does not exist.
		if s.enrollmentTokenExists(ctx, token) {
			return EnrollmentToken{}, ErrTokenExhausted
		}
		return EnrollmentToken{}, ErrTokenInvalid
	}
	if err != nil {
		return EnrollmentToken{}, fmt.Errorf("redeem enrollment token: %w", err)
	}
	return t, nil
}

func (s *Store) enrollmentTokenExists(ctx context.Context, token string) bool {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM enrollment_tokens WHERE token_hash = $1)`,
		HashToken(token)).Scan(&exists)
	return err == nil && exists
}

// ListEnrollmentTokens returns the tokens an operator has issued. Secrets are
// not included, because they were never stored.
func (s *Store) ListEnrollmentTokens(ctx context.Context) ([]EnrollmentToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, description, max_uses, uses, expires_at, revoked_at, created_at
		FROM enrollment_tokens ORDER BY created_at DESC LIMIT $1`, DefaultListLimit)
	if err != nil {
		return nil, fmt.Errorf("list enrollment tokens: %w", err)
	}
	defer rows.Close()

	out := []EnrollmentToken{}
	for rows.Next() {
		var t EnrollmentToken
		if err := rows.Scan(&t.ID, &t.Description, &t.MaxUses, &t.Uses, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("list enrollment tokens: %w", err)
		}
		out = append(out, t)
	}
	return out, wrap(rows.Err())
}

// RevokeEnrollmentToken stops a token being used again.
func (s *Store) RevokeEnrollmentToken(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE enrollment_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoke enrollment token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IssueMachineToken creates the credential an agent uses after enrolling, and
// revokes any it already had.
//
// Reissuing rather than reusing means a rotation is a visible event: the old
// row keeps its revoked_at, so an operator can see when a machine's
// credential changed.
func (s *Store) IssueMachineToken(ctx context.Context, runnerID string) (string, error) {
	secret, err := NewToken(MachinePrefix)
	if err != nil {
		return "", err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("issue machine token: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE machine_tokens SET revoked_at = now() WHERE runner_id = $1 AND revoked_at IS NULL`,
		runnerID); err != nil {
		return "", fmt.Errorf("revoke previous machine tokens: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO machine_tokens (id, runner_id, token_hash) VALUES ($1, $2, $3)`,
		newID(), runnerID, HashToken(secret)); err != nil {
		return "", fmt.Errorf("issue machine token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("issue machine token: %w", err)
	}
	return secret, nil
}

// AuthenticateMachine resolves a machine token to its runner and records that
// it was used.
func (s *Store) AuthenticateMachine(ctx context.Context, token string) (Runner, error) {
	if !strings.HasPrefix(token, MachinePrefix) {
		return Runner{}, ErrTokenInvalid
	}

	var runnerID string
	err := s.pool.QueryRow(ctx, `
		UPDATE machine_tokens SET last_used_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL
		RETURNING runner_id`, HashToken(token)).Scan(&runnerID)
	if errors.Is(wrap(err), ErrNotFound) {
		return Runner{}, ErrTokenInvalid
	}
	if err != nil {
		return Runner{}, fmt.Errorf("authenticate machine: %w", err)
	}
	return s.Runner(ctx, runnerID)
}

// RevokeMachineTokens invalidates every credential a runner holds.
func (s *Store) RevokeMachineTokens(ctx context.Context, runnerID string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE machine_tokens SET revoked_at = now() WHERE runner_id = $1 AND revoked_at IS NULL`,
		runnerID); err != nil {
		return fmt.Errorf("revoke machine tokens: %w", err)
	}
	return nil
}

// SecureCompare reports whether two secrets are equal, without leaking how
// far they matched through timing.
func SecureCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
