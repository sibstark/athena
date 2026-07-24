package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestSessionValidationRejectsTamperingAndExpiry(t *testing.T) {
	current := time.Unix(1_700_000_000, 0)
	manager := testManager(t, &current)
	token, _, err := manager.Login("admin", "password", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if username, ok := manager.Validate(token); !ok || username != "admin" {
		t.Fatalf("valid session = %q, %v", username, ok)
	}

	parts := strings.Split(token, ".")
	parts[0] = "A" + parts[0][1:]
	if _, ok := manager.Validate(strings.Join(parts, ".")); ok {
		t.Fatal("tampered session accepted")
	}

	current = current.Add(25 * time.Hour)
	if _, ok := manager.Validate(token); ok {
		t.Fatal("expired session accepted")
	}
}

func TestLoginRateLimitAndSuccessfulReset(t *testing.T) {
	current := time.Unix(1_700_000_000, 0)
	manager := testManager(t, &current)
	for range 5 {
		if _, _, err := manager.Login("admin", "wrong", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure error = %v", err)
		}
	}
	if _, _, err := manager.Login("admin", "password", "192.0.2.1"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("limited error = %v", err)
	}

	current = current.Add(16 * time.Minute)
	if _, _, err := manager.Login("admin", "password", "192.0.2.1"); err != nil {
		t.Fatalf("login after window: %v", err)
	}
	if _, _, err := manager.Login("admin", "wrong", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("new failure error = %v", err)
	}
	if _, _, err := manager.Login("admin", "password", "192.0.2.1"); err != nil {
		t.Fatalf("successful login did not reset failures: %v", err)
	}
}

func testManager(t *testing.T, current *time.Time) *Manager {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return New(Config{
		Username:      "admin",
		PasswordHash:  string(hash),
		SessionSecret: strings.Repeat("s", 32),
		SessionTTL:    24 * time.Hour,
	}, func() time.Time { return *current })
}
