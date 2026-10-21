package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"app-grabber-backend/internal/model"
)

const (
	MinPasswordLength = 15
	MaxPasswordLength = 128
	MaxNicknameLength = 50
	maxEmailLength    = 254

	// Нужен только для проверки пароля при неизвестном email; аккаунту не принадлежит.
	dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("invalid password")
	ErrInvalidNickname    = errors.New("invalid nickname")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

type Service struct {
	users UserRepository
}

func NewService(users UserRepository) *Service {
	return &Service{users: users}
}

func (s *Service) Register(ctx context.Context, email, nickname, password string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return model.User{}, err
	}
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || !utf8.ValidString(nickname) || utf8.RuneCountInString(nickname) > MaxNicknameLength || strings.ContainsFunc(nickname, unicode.IsControl) {
		return model.User{}, ErrInvalidNickname
	}
	if !validPasswordInput(password) || utf8.RuneCountInString(password) < MinPasswordLength {
		return model.User{}, ErrInvalidPassword
	}
	hash, err := HashPassword(password)
	if err != nil {
		return model.User{}, fmt.Errorf("hash password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	user, err := s.users.Create(ctx, email, nickname, hash)
	if err != nil {
		return model.User{}, fmt.Errorf("create user: %w", err)
	}
	user.PasswordHash = ""
	return user, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (model.User, error) {
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	email, err := normalizeEmail(email)
	if err != nil || !validPasswordInput(password) {
		return model.User{}, ErrInvalidCredentials
	}
	user, err := s.users.GetByEmail(ctx, email)
	notFound := errors.Is(err, ErrUserNotFound)
	if err != nil && !notFound {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	hash := user.PasswordHash
	if notFound {
		hash = dummyPasswordHash
	}
	matches, err := CheckPassword(password, hash)
	if err != nil {
		return model.User{}, fmt.Errorf("check password: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return model.User{}, err
	}
	if notFound || !matches {
		return model.User{}, ErrInvalidCredentials
	}
	user.PasswordHash = ""
	return user, nil
}

func normalizeEmail(email string) (string, error) {
	if !utf8.ValidString(email) {
		return "", ErrInvalidEmail
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || len(email) > maxEmailLength || !utf8.ValidString(email) {
		return "", ErrInvalidEmail
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Name != "" || address.Address != email {
		return "", ErrInvalidEmail
	}
	return email, nil
}

func validPasswordInput(password string) bool {
	return len(password) <= MaxPasswordLength*utf8.UTFMax && utf8.ValidString(password) && utf8.RuneCountInString(password) <= MaxPasswordLength
}
