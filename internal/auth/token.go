package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	tokenIssuer    = "app-grabber"
	tokenAudience  = "app-grabber-api"
	maxTokenLength = 8192
)

var ErrInvalidToken = errors.New("invalid access token")

type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret string, ttl time.Duration) (*TokenManager, error) {
	if len(secret) < 32 || strings.TrimSpace(secret) == "" {
		return nil, errors.New("JWT secret must contain at least 32 bytes and cannot be blank")
	}
	if ttl < time.Second {
		return nil, errors.New("JWT TTL must be at least one second")
	}
	return &TokenManager{secret: []byte(secret), ttl: ttl, now: time.Now}, nil
}

func NewTokenManagerFromEnv() (*TokenManager, error) {
	ttl := 24 * time.Hour
	if value := os.Getenv("JWT_TTL"); value != "" {
		var err error
		ttl, err = time.ParseDuration(value)
		if err != nil {
			return nil, fmt.Errorf("parse JWT_TTL: %w", err)
		}
	}
	return NewTokenManager(os.Getenv("JWT_SECRET"), ttl)
}

func (m *TokenManager) Issue(userID string) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", errors.New("user ID is required")
	}
	now := m.now()
	claims := jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Audience:  jwt.ClaimStrings{tokenAudience},
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	if len(token) > maxTokenLength {
		return "", errors.New("access token is too large")
	}
	return token, nil
}

func (m *TokenManager) Verify(raw string) (string, error) {
	if raw == "" || len(raw) > maxTokenLength {
		return "", ErrInvalidToken
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer), jwt.WithAudience(tokenAudience),
		jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithTimeFunc(m.now))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid || strings.TrimSpace(claims.Subject) == "" || claims.IssuedAt == nil {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}
