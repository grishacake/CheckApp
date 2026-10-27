package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testTokens(t *testing.T) *TokenManager {
	t.Helper()
	m, err := NewTokenManager(strings.Repeat("s", 32), 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	m.now = func() time.Time { return time.Unix(1800000000, 0) }
	return m
}

func TestTokenRoundTripAndExpiration(t *testing.T) {
	m := testTokens(t)
	raw, err := m.Issue("user-123")
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Verify(raw)
	if err != nil || id != "user-123" {
		t.Fatalf("id=%q err=%v", id, err)
	}
	issued := m.now()
	m.now = func() time.Time { return issued.Add(m.ttl - time.Second) }
	if _, err := m.Verify(raw); err != nil {
		t.Fatalf("unexpired token: %v", err)
	}
	m.now = func() time.Time { return issued.Add(m.ttl) }
	if id, err := m.Verify(raw); !errors.Is(err, ErrInvalidToken) || id != "" {
		t.Fatalf("expired token: id=%q err=%v", id, err)
	}
}

func TestTokenRejectsInvalidClaimsAndSignatures(t *testing.T) {
	m := testTokens(t)
	for _, tt := range []struct {
		name   string
		change func(*jwt.RegisteredClaims)
		method jwt.SigningMethod
		key    any
	}{
		{name: "missing expiration", change: func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }},
		{name: "expired", change: func(c *jwt.RegisteredClaims) { c.ExpiresAt = jwt.NewNumericDate(m.now()) }},
		{name: "wrong issuer", change: func(c *jwt.RegisteredClaims) { c.Issuer = "other" }},
		{name: "missing issuer", change: func(c *jwt.RegisteredClaims) { c.Issuer = "" }},
		{name: "wrong audience", change: func(c *jwt.RegisteredClaims) { c.Audience = jwt.ClaimStrings{"other"} }},
		{name: "missing audience", change: func(c *jwt.RegisteredClaims) { c.Audience = nil }},
		{name: "missing subject", change: func(c *jwt.RegisteredClaims) { c.Subject = "" }},
		{name: "blank subject", change: func(c *jwt.RegisteredClaims) { c.Subject = "  " }},
		{name: "missing issued at", change: func(c *jwt.RegisteredClaims) { c.IssuedAt = nil }},
		{name: "future issued at", change: func(c *jwt.RegisteredClaims) { c.IssuedAt = jwt.NewNumericDate(m.now().Add(time.Minute)) }},
		{name: "not yet valid", change: func(c *jwt.RegisteredClaims) { c.NotBefore = jwt.NewNumericDate(m.now().Add(time.Minute)) }},
		{name: "different key", key: []byte(strings.Repeat("x", 32))},
		{name: "different algorithm", method: jwt.SigningMethodHS384},
		{name: "unsigned", method: jwt.SigningMethodNone, key: jwt.UnsafeAllowNoneSignatureType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			claims := jwt.RegisteredClaims{Subject: "user-123", Issuer: tokenIssuer,
				Audience: jwt.ClaimStrings{tokenAudience}, IssuedAt: jwt.NewNumericDate(m.now()),
				ExpiresAt: jwt.NewNumericDate(m.now().Add(m.ttl))}
			if tt.change != nil {
				tt.change(&claims)
			}
			method := tt.method
			if method == nil {
				method = jwt.SigningMethodHS256
			}
			key := tt.key
			if key == nil {
				key = m.secret
			}
			raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
			if err != nil {
				t.Fatal(err)
			}
			if id, err := m.Verify(raw); id != "" || !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("invalid token accepted: id=%q err=%v", id, err)
			}
		})
	}
}

func TestTokenRejectsMalformedInput(t *testing.T) {
	m := testTokens(t)
	for _, raw := range []string{"", "abc", "a.b.c", strings.Repeat("a", maxTokenLength+1)} {
		if _, err := m.Verify(raw); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("error=%v", err)
		}
	}
	for _, id := range []string{"", " \t", strings.Repeat("a", maxTokenLength)} {
		if _, err := m.Issue(id); err == nil {
			t.Fatal("invalid ID accepted")
		}
	}
}

func TestTokenConfiguration(t *testing.T) {
	for _, tt := range []struct {
		name, secret string
		ttl          time.Duration
	}{
		{"missing key", "", time.Minute},
		{"short key", strings.Repeat("a", 31), time.Minute},
		{"blank key", strings.Repeat(" ", 32), time.Minute},
		{"zero TTL", strings.Repeat("a", 32), 0},
		{"negative TTL", strings.Repeat("a", 32), -time.Minute},
		{"short TTL", strings.Repeat("a", 32), time.Millisecond},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTokenManager(tt.secret, tt.ttl); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestTokenConfigurationFromEnv(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("JWT_TTL", "")
	m, err := NewTokenManagerFromEnv()
	if err != nil || m.ttl != 24*time.Hour {
		t.Fatalf("default configuration: %v", err)
	}
	t.Setenv("JWT_TTL", "30m")
	m, err = NewTokenManagerFromEnv()
	if err != nil || m.ttl != 30*time.Minute {
		t.Fatalf("custom configuration: %v", err)
	}
	for _, ttl := range []string{"bad", "0s", "-1m"} {
		t.Setenv("JWT_TTL", ttl)
		if _, err := NewTokenManagerFromEnv(); err == nil {
			t.Fatalf("accepted TTL %q", ttl)
		}
	}
	t.Setenv("JWT_TTL", "15m")
	t.Setenv("JWT_SECRET", "")
	if _, err := NewTokenManagerFromEnv(); err == nil {
		t.Fatal("missing secret accepted")
	}
}
