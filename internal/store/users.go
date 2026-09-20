package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SessionLifetime is how long a dashboard session lasts before the operator
// has to sign in again.
const SessionLifetime = 7 * 24 * time.Hour

// UpsertUser records a GitHub account signing in, storing its token
// encrypted.
//
// encryptedToken may be nil when the caller does not want the server holding
// a user's GitHub credential at all.
func (s *Store) UpsertUser(ctx context.Context, u User, encryptedToken []byte) (User, error) {
	if u.GitHubID == 0 || u.Login == "" {
		return User{}, errors.New("a user needs a GitHub id and login")
	}

	var out User
	row := s.pool.QueryRow(ctx, `
		INSERT INTO users (id, github_id, login, name, avatar_url, access_token)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (github_id) DO UPDATE SET
			login         = EXCLUDED.login,
			name          = EXCLUDED.name,
			avatar_url    = EXCLUDED.avatar_url,
			access_token  = COALESCE(EXCLUDED.access_token, users.access_token),
			last_login_at = now()
		RETURNING id, github_id, login, name, avatar_url, created_at, last_login_at`,
		newID(), u.GitHubID, u.Login, u.Name, u.AvatarURL, encryptedToken)

	if err := row.Scan(&out.ID, &out.GitHubID, &out.Login, &out.Name, &out.AvatarURL,
		&out.CreatedAt, &out.LastLoginAt); err != nil {
		return User{}, fmt.Errorf("record user %q: %w", u.Login, wrap(err))
	}
	return out, nil
}

// User returns one account by id.
func (s *Store) User(ctx context.Context, id string) (User, error) {
	var u User
	row := s.pool.QueryRow(ctx, `
		SELECT id, github_id, login, name, avatar_url, created_at, last_login_at
		FROM users WHERE id = $1`, id)
	if err := row.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Name, &u.AvatarURL,
		&u.CreatedAt, &u.LastLoginAt); err != nil {
		return User{}, fmt.Errorf("get user %s: %w", id, wrap(err))
	}
	return u, nil
}

// UserToken returns a user's stored GitHub token, still encrypted. Decrypting
// it is the caller's job, because the store has no key.
func (s *Store) UserToken(ctx context.Context, userID string) ([]byte, error) {
	var token []byte
	err := s.pool.QueryRow(ctx, `SELECT access_token FROM users WHERE id = $1`, userID).Scan(&token)
	if err != nil {
		return nil, fmt.Errorf("get user token: %w", wrap(err))
	}
	return token, nil
}

// CreateSession starts a dashboard session and returns the cookie value.
func (s *Store) CreateSession(ctx context.Context, userID string) (string, time.Time, error) {
	secret, err := NewToken(SessionPrefix)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := time.Now().UTC().Add(SessionLifetime)

	if _, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES ($1, $2, $3, $4)`,
		newID(), userID, HashToken(secret), expires); err != nil {
		return "", time.Time{}, fmt.Errorf("create session: %w", err)
	}
	return secret, expires, nil
}

// AuthenticateSession resolves a session cookie to its user.
func (s *Store) AuthenticateSession(ctx context.Context, token string) (User, error) {
	if !strings.HasPrefix(token, SessionPrefix) {
		return User{}, ErrTokenInvalid
	}

	var u User
	row := s.pool.QueryRow(ctx, `
		SELECT u.id, u.github_id, u.login, u.name, u.avatar_url, u.created_at, u.last_login_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, HashToken(token))

	err := row.Scan(&u.ID, &u.GitHubID, &u.Login, &u.Name, &u.AvatarURL, &u.CreatedAt, &u.LastLoginAt)
	if errors.Is(wrap(err), ErrNotFound) {
		return User{}, ErrTokenInvalid
	}
	if err != nil {
		return User{}, fmt.Errorf("authenticate session: %w", err)
	}
	return u, nil
}

// DeleteSession ends one session.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, HashToken(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// PruneSessions deletes expired sessions and returns how many went.
func (s *Store) PruneSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= now()`)
	if err != nil {
		return 0, fmt.Errorf("prune sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}
