package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewTokenIsPrefixedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		token, err := NewToken(EnrollmentPrefix)
		if err != nil {
			t.Fatalf("NewToken() error = %v", err)
		}
		if !strings.HasPrefix(token, EnrollmentPrefix) {
			t.Fatalf("token %q has no prefix", token)
		}
		if seen[token] {
			t.Fatalf("token %q was generated twice", token)
		}
		seen[token] = true
	}
}

func TestHashTokenIsStableAndNotReversible(t *testing.T) {
	token := "rnr_enroll_example"
	first, second := HashToken(token), HashToken(token)
	if string(first) != string(second) {
		t.Error("HashToken() is not stable")
	}
	if len(first) != 32 {
		t.Errorf("hash is %d bytes, want 32", len(first))
	}
	if strings.Contains(string(first), token) {
		t.Error("the hash contains the token")
	}
	if string(HashToken("rnr_enroll_other")) == string(first) {
		t.Error("two different tokens hashed the same")
	}
}

func TestSecureCompare(t *testing.T) {
	if !SecureCompare("abc", "abc") {
		t.Error("SecureCompare() = false for equal values")
	}
	if SecureCompare("abc", "abd") || SecureCompare("abc", "ab") {
		t.Error("SecureCompare() = true for different values")
	}
}

func TestEnrollmentTokenRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	token, secret, err := s.CreateEnrollmentToken(ctx, "laptop", nil, nil)
	if err != nil {
		t.Fatalf("CreateEnrollmentToken() error = %v", err)
	}
	if !strings.HasPrefix(secret, EnrollmentPrefix) {
		t.Errorf("secret = %q", secret)
	}
	if token.Uses != 0 || token.Description != "laptop" {
		t.Errorf("token = %+v", token)
	}

	// The secret must not be recoverable from the database.
	var stored []byte
	if err := s.pool.QueryRow(ctx, `SELECT token_hash FROM enrollment_tokens WHERE id = $1`, token.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), secret) {
		t.Error("the plaintext token was stored")
	}

	redeemed, err := s.RedeemEnrollmentToken(ctx, secret)
	if err != nil {
		t.Fatalf("RedeemEnrollmentToken() error = %v", err)
	}
	if redeemed.ID != token.ID || redeemed.Uses != 1 {
		t.Errorf("redeemed = %+v", redeemed)
	}
}

func TestEnrollmentTokenRejectsUnknownAndMalformed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.RedeemEnrollmentToken(ctx, "not-a-runnerly-token"); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want ErrTokenInvalid for a malformed token", err)
	}
	unknown, err := NewToken(EnrollmentPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemEnrollmentToken(ctx, unknown); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want ErrTokenInvalid for an unknown token", err)
	}
}

func TestEnrollmentTokenRespectsMaxUses(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	once := 1
	_, secret, err := s.CreateEnrollmentToken(ctx, "single use", &once, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemEnrollmentToken(ctx, secret); err != nil {
		t.Fatalf("first redemption failed: %v", err)
	}
	// The second attempt must be refused, and say why.
	if _, err := s.RedeemEnrollmentToken(ctx, secret); !errors.Is(err, ErrTokenExhausted) {
		t.Errorf("error = %v, want ErrTokenExhausted", err)
	}
}

func TestEnrollmentTokenRespectsExpiry(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	_, secret, err := s.CreateEnrollmentToken(ctx, "expired", nil, &past)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RedeemEnrollmentToken(ctx, secret); !errors.Is(err, ErrTokenExhausted) {
		t.Errorf("error = %v, want ErrTokenExhausted for an expired token", err)
	}
}

func TestEnrollmentTokenRevocation(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	token, secret, err := s.CreateEnrollmentToken(ctx, "revoke me", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeEnrollmentToken(ctx, token.ID); err != nil {
		t.Fatalf("RevokeEnrollmentToken() error = %v", err)
	}
	if _, err := s.RedeemEnrollmentToken(ctx, secret); !errors.Is(err, ErrTokenExhausted) {
		t.Errorf("error = %v, want ErrTokenExhausted for a revoked token", err)
	}
	if err := s.RevokeEnrollmentToken(ctx, token.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoking twice = %v, want ErrNotFound", err)
	}
}

func TestUsable(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	two, zero := 2, 0

	tests := []struct {
		name  string
		token EnrollmentToken
		want  bool
	}{
		{"fresh", EnrollmentToken{}, true},
		{"revoked", EnrollmentToken{RevokedAt: &past}, false},
		{"expired", EnrollmentToken{ExpiresAt: &past}, false},
		{"not yet expired", EnrollmentToken{ExpiresAt: &future}, true},
		{"uses left", EnrollmentToken{MaxUses: &two, Uses: 1}, true},
		{"used up", EnrollmentToken{MaxUses: &two, Uses: 2}, false},
		{"zero uses allowed", EnrollmentToken{MaxUses: &zero}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.token.Usable(now); got != tt.want {
				t.Errorf("Usable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListEnrollmentTokensHidesSecrets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	_, secret, err := s.CreateEnrollmentToken(ctx, "one", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := s.ListEnrollmentTokens(ctx)
	if err != nil {
		t.Fatalf("ListEnrollmentTokens() error = %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens, want 1", len(tokens))
	}
	// The struct has no field for it, which is the point; assert the value is
	// nowhere in what a caller receives.
	if strings.Contains(tokens[0].Description+tokens[0].ID, secret) {
		t.Error("the secret leaked into the listing")
	}
}

func TestMachineTokenAuthenticates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.IssueMachineToken(ctx, r.ID)
	if err != nil {
		t.Fatalf("IssueMachineToken() error = %v", err)
	}
	if !strings.HasPrefix(secret, MachinePrefix) {
		t.Errorf("token = %q", secret)
	}

	got, err := s.AuthenticateMachine(ctx, secret)
	if err != nil {
		t.Fatalf("AuthenticateMachine() error = %v", err)
	}
	if got.ID != r.ID {
		t.Errorf("authenticated as %s, want %s", got.ID, r.ID)
	}
}

func TestMachineTokenRejectsTheWrongKind(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// An enrollment token must not work as a machine credential, even though
	// both are bearer tokens.
	_, enrollment, err := s.CreateEnrollmentToken(ctx, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateMachine(ctx, enrollment); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("error = %v, want ErrTokenInvalid", err)
	}
}

func TestIssuingAMachineTokenRevokesThePreviousOne(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.IssueMachineToken(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := s.IssueMachineToken(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AuthenticateMachine(ctx, fresh); err != nil {
		t.Errorf("the new token does not work: %v", err)
	}
	if _, err := s.AuthenticateMachine(ctx, old); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("the rotated-out token still works: %v", err)
	}
}

func TestRevokeMachineTokens(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	r, err := s.UpsertRunner(ctx, sampleRunner("runnerly-01"))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.IssueMachineToken(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RevokeMachineTokens(ctx, r.ID); err != nil {
		t.Fatalf("RevokeMachineTokens() error = %v", err)
	}
	if _, err := s.AuthenticateMachine(ctx, secret); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("a revoked token still authenticates: %v", err)
	}
}
