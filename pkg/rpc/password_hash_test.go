package rpc

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordHash(t *testing.T) {
	t.Run("returns bcrypt hash that round-trips", func(t *testing.T) {
		const pw = "thepuppetmaster2018"
		h, err := passwordHash(pw)
		if err != nil {
			t.Fatalf("passwordHash: %v", err)
		}
		if h == "" || h == pw {
			t.Fatalf("hash must not be empty or equal plaintext, got %q", h)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(h), []byte(pw)); err != nil {
			t.Fatalf("CompareHashAndPassword: %v", err)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(h), []byte("wrong")); err == nil {
			t.Fatalf("compare with wrong password must succeed-fail, got nil")
		}
	})

	t.Run("salts each hash so the same password yields different output", func(t *testing.T) {
		const pw = "same-input"
		h1, err := passwordHash(pw)
		if err != nil {
			t.Fatalf("passwordHash: %v", err)
		}
		h2, err := passwordHash(pw)
		if err != nil {
			t.Fatalf("passwordHash: %v", err)
		}
		if h1 == h2 {
			t.Fatalf("expected different salts: %q == %q", h1, h2)
		}
	})

	t.Run("rejects passwords longer than bcrypt's 72-byte input ceiling", func(t *testing.T) {
		_, err := passwordHash(strings.Repeat("x", 73))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}
