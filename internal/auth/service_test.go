package auth_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/model"
)

type userRepositoryStub struct {
	create     func(context.Context, string, string, string) (model.User, error)
	getByEmail func(context.Context, string) (model.User, error)
}

func (r userRepositoryStub) Create(ctx context.Context, email, nickname, hash string) (model.User, error) {
	return r.create(ctx, email, nickname, hash)
}
func (r userRepositoryStub) GetByEmail(ctx context.Context, email string) (model.User, error) {
	return r.getByEmail(ctx, email)
}
func (r userRepositoryStub) GetByID(context.Context, string) (model.User, error) {
	panic("unexpected GetByID")
}

func TestRegisterAndLogin(t *testing.T) {
	const password = "  пароль пользователя 🔐  "
	var stored model.User
	service := auth.NewService(userRepositoryStub{
		create: func(_ context.Context, email, nickname, hash string) (model.User, error) {
			stored = model.User{ID: "e94b6fc3-0c20-4f57-817c-856e55297e59", Email: email, Nickname: nickname, PasswordHash: hash, CreatedAt: time.Now().UTC()}
			return stored, nil
		},
		getByEmail: func(_ context.Context, email string) (model.User, error) {
			if email != stored.Email {
				return model.User{}, auth.ErrUserNotFound
			}
			return stored, nil
		},
	})
	user, err := service.Register(context.Background(), "  ARTEM@Example.COM ", " Артём ", password)
	if err != nil {
		t.Fatal(err)
	}
	if user.ID != stored.ID || user.Email != "artem@example.com" || user.Nickname != "Артём" || user.CreatedAt.IsZero() {
		t.Fatalf("unexpected user: id=%s email=%s nickname=%s", user.ID, user.Email, user.Nickname)
	}
	if user.PasswordHash != "" {
		t.Fatal("registration exposed password hash")
	}
	matches, err := auth.CheckPassword(password, stored.PasswordHash)
	if err != nil || !matches || stored.PasswordHash == password {
		t.Fatal("repository did not receive a valid password hash")
	}
	loggedIn, err := service.Login(context.Background(), " ARTEM@example.com ", password)
	if err != nil || loggedIn != user {
		t.Fatalf("login failed: %v", err)
	}
	_, err = service.Login(context.Background(), user.Email, strings.TrimSpace(password))
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("password whitespace was discarded: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	for _, tt := range []struct {
		name, email, nickname, password string
		want                            error
	}{
		{"empty email", "", "", "a sufficiently long password", auth.ErrInvalidEmail},
		{"malformed email", "no-at-sign", "", "a sufficiently long password", auth.ErrInvalidEmail},
		{"display name", "Artem <artem@example.com>", "", "a sufficiently long password", auth.ErrInvalidEmail},
		{"invalid email UTF8", "user\xff@example.com", "", "a sufficiently long password", auth.ErrInvalidEmail},
		{"long email", strings.Repeat("a", 250) + "@example.com", "", "a sufficiently long password", auth.ErrInvalidEmail},
		{"short password", "a@example.com", "artem", strings.Repeat("я", 14), auth.ErrInvalidPassword},
		{"long password", "a@example.com", "artem", strings.Repeat("я", 129), auth.ErrInvalidPassword},
		{"invalid password UTF8", "a@example.com", "artem", "a long password\xff", auth.ErrInvalidPassword},
		{"empty nickname", "a@example.com", "", "a sufficiently long password", auth.ErrInvalidNickname},
		{"blank nickname", "a@example.com", " \t\n\u00a0 ", "a sufficiently long password", auth.ErrInvalidNickname},
		{"long nickname", "a@example.com", strings.Repeat("я", 51), "a sufficiently long password", auth.ErrInvalidNickname},
		{"nickname control", "a@example.com", "ar\ntem", "a sufficiently long password", auth.ErrInvalidNickname},
		{"nickname UTF8", "a@example.com", "ar\xfftem", "a sufficiently long password", auth.ErrInvalidNickname},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := auth.NewService(userRepositoryStub{create: func(context.Context, string, string, string) (model.User, error) {
				t.Error("invalid input reached repository")
				return model.User{}, nil
			}})
			user, err := service.Register(context.Background(), tt.email, tt.nickname, tt.password)
			if !errors.Is(err, tt.want) || user != (model.User{}) {
				t.Fatalf("error = %v, want %v and no user", err, tt.want)
			}
		})
	}
}

func TestRegisterLengthBoundaries(t *testing.T) {
	for _, length := range []int{auth.MinPasswordLength, auth.MaxPasswordLength} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			nickname := "я"
			if length == auth.MaxPasswordLength {
				nickname = strings.Repeat("я", auth.MaxNicknameLength)
			}
			password := strings.Repeat("я", length)
			calls := 0
			service := auth.NewService(userRepositoryStub{create: func(_ context.Context, email, nick, hash string) (model.User, error) {
				calls++
				if nick != nickname {
					t.Error("nickname was changed")
				}
				ok, err := auth.CheckPassword(password, hash)
				if err != nil || !ok {
					t.Error("password was changed")
				}
				return model.User{ID: "id", Email: email, Nickname: nick, PasswordHash: hash}, nil
			}})
			_, err := service.Register(context.Background(), "a@example.com", nickname, password)
			if err != nil || calls != 1 {
				t.Fatalf("valid boundary rejected: %v, calls=%d", err, calls)
			}
		})
	}
}

func TestRegisterRepositoryErrors(t *testing.T) {
	for _, want := range []error{auth.ErrEmailAlreadyExists, errors.New("database unavailable")} {
		t.Run(want.Error(), func(t *testing.T) {
			service := auth.NewService(userRepositoryStub{create: func(context.Context, string, string, string) (model.User, error) {
				return model.User{}, fmt.Errorf("storage: %w", want)
			}})
			user, err := service.Register(context.Background(), "a@example.com", "artem", "a sufficiently long password")
			if !errors.Is(err, want) || user != (model.User{}) {
				t.Fatalf("error = %v, want %v and no user", err, want)
			}
		})
	}
}

func TestLoginErrors(t *testing.T) {
	const password = "a sufficiently long password"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	databaseError := errors.New("database unavailable")
	for _, tt := range []struct {
		name, email, password, hash string
		lookupError, want           error
		calls                       int
	}{
		{"unknown user", "a@example.com", password, "", fmt.Errorf("storage: %w", auth.ErrUserNotFound), auth.ErrInvalidCredentials, 1},
		{"wrong password", "a@example.com", "another long password", hash, nil, auth.ErrInvalidCredentials, 1},
		{"empty password", "a@example.com", "", hash, nil, auth.ErrInvalidCredentials, 1},
		{"bad email", "bad email", password, hash, nil, auth.ErrInvalidCredentials, 0},
		{"long password", "a@example.com", strings.Repeat("a", 129), hash, nil, auth.ErrInvalidCredentials, 0},
		{"corrupt hash", "a@example.com", password, "broken hash", nil, auth.ErrInvalidPasswordHash, 1},
		{"database unavailable", "a@example.com", password, "", databaseError, databaseError, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			service := auth.NewService(userRepositoryStub{getByEmail: func(context.Context, string) (model.User, error) {
				calls++
				return model.User{ID: "id", PasswordHash: tt.hash}, tt.lookupError
			}})
			user, err := service.Login(context.Background(), tt.email, tt.password)
			if !errors.Is(err, tt.want) || user != (model.User{}) || calls != tt.calls {
				t.Fatalf("error = %v, want %v; calls=%d, want %d; returnedUser=%v", err, tt.want, calls, tt.calls, user.ID != "")
			}
		})
	}
}

func TestAuthAlreadyCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := auth.NewService(userRepositoryStub{})
	if _, err := service.Register(ctx, "a@example.com", "artem", "a sufficiently long password"); !errors.Is(err, context.Canceled) {
		t.Fatalf("register error = %v", err)
	}
	if _, err := service.Login(ctx, "a@example.com", "a sufficiently long password"); !errors.Is(err, context.Canceled) {
		t.Fatalf("login error = %v", err)
	}
}

func TestLoginCanceledDuringLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := auth.NewService(userRepositoryStub{getByEmail: func(context.Context, string) (model.User, error) { cancel(); return model.User{ID: "id"}, nil }})
	user, err := service.Login(ctx, "a@example.com", "a sufficiently long password")
	if !errors.Is(err, context.Canceled) || user != (model.User{}) {
		t.Fatalf("error = %v, want canceled and no user", err)
	}
}
