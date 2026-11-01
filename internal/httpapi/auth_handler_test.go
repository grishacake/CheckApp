package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type authHandlerServiceStub struct {
	register func(context.Context, string, string, string) (model.User, error)
	login    func(context.Context, string, string) (model.User, error)
	getByID  func(context.Context, string) (model.User, error)
}

func (s authHandlerServiceStub) Register(ctx context.Context, email, nickname, password string) (model.User, error) {
	return s.register(ctx, email, nickname, password)
}
func (s authHandlerServiceStub) Login(ctx context.Context, email, password string) (model.User, error) {
	return s.login(ctx, email, password)
}
func (s authHandlerServiceStub) GetByID(ctx context.Context, id string) (model.User, error) {
	return s.getByID(ctx, id)
}

type authHandlerRepository struct{ user model.User }

func (r *authHandlerRepository) Create(_ context.Context, email, nickname, hash string) (model.User, error) {
	if r.user.ID != "" {
		return model.User{}, auth.ErrEmailAlreadyExists
	}
	r.user = model.User{ID: "user-123", Email: email, Nickname: nickname, PasswordHash: hash, CreatedAt: time.Now().UTC()}
	return r.user, nil
}
func (r *authHandlerRepository) GetByEmail(_ context.Context, email string) (model.User, error) {
	if r.user.ID == "" || r.user.Email != email {
		return model.User{}, auth.ErrUserNotFound
	}
	return r.user, nil
}
func (r *authHandlerRepository) GetByID(_ context.Context, id string) (model.User, error) {
	if r.user.ID == "" || r.user.ID != id {
		return model.User{}, auth.ErrUserNotFound
	}
	return r.user, nil
}

func newHandlerTokens(t *testing.T) *auth.TokenManager {
	t.Helper()
	tokens, err := auth.NewTokenManager(strings.Repeat("s", 32), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func authRequest(handler http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestAuthHandlerFullFlow(t *testing.T) {
	repository := &authHandlerRepository{}
	tokens := newHandlerTokens(t)
	mux := http.NewServeMux()
	httpapi.NewAuthHandler(auth.NewService(repository), tokens, 0).RegisterRoutes(mux)
	register := `{"email":" ARTEM@example.com ","nickname":" Артём ","password":"a sufficiently long password"}`
	w := authRequest(mux, "POST", "/api/auth/register", register, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", w.Code, w.Body.String())
	}
	var user model.User
	if err := json.Unmarshal(w.Body.Bytes(), &user); err != nil {
		t.Fatal(err)
	}
	if user.ID != "user-123" || user.Nickname != "Артём" || user.Email != "artem@example.com" || user.CreatedAt.IsZero() {
		t.Fatalf("unexpected registered user: %+v", user)
	}
	if w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "assword") {
		t.Fatal("unsafe registration response")
	}
	if repository.user.PasswordHash == "" {
		t.Fatal("password was not stored as a hash")
	}
	w = authRequest(mux, "POST", "/api/auth/register", register, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate email: %d", w.Code)
	}
	w = authRequest(mux, "POST", "/api/auth/login", `{"email":"ARTEM@example.com","password":"a sufficiently long password"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var login struct {
		AccessToken string     `json:"access_token"`
		TokenType   string     `json:"token_type"`
		User        model.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	if login.TokenType != "Bearer" || login.User != user || strings.Contains(w.Body.String(), "assword") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unexpected login response")
	}
	if id, err := tokens.Verify(login.AccessToken); err != nil || id != user.ID {
		t.Fatalf("issued token: id=%q err=%v", id, err)
	}
	w = authRequest(mux, "GET", "/api/auth/me", "", login.AccessToken)
	var profile model.User
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || profile != user || strings.Contains(w.Body.String(), "assword") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("profile: %d %s", w.Code, w.Body.String())
	}
	for _, credentials := range []string{
		`{"email":"artem@example.com","password":"wrong password"}`,
		`{"email":"unknown@example.com","password":"a sufficiently long password"}`,
	} {
		w = authRequest(mux, "POST", "/api/auth/login", credentials, "")
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "Неверный email или пароль") {
			t.Fatalf("invalid credentials: %d %s", w.Code, w.Body.String())
		}
	}
	for _, token := range []string{"", "broken"} {
		w = authRequest(mux, "GET", "/api/auth/me", "", token)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("unauthorized profile: %d", w.Code)
		}
	}
	repository.user = model.User{}
	w = authRequest(mux, "GET", "/api/auth/me", "", login.AccessToken)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user: %d", w.Code)
	}
	w = authRequest(mux, "GET", "/api/auth/login", "", "")
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method: %d", w.Code)
	}
}

func TestAuthHandlerRejectsInvalidBody(t *testing.T) {
	handler := httpapi.NewAuthHandler(authHandlerServiceStub{}, newHandlerTokens(t), 0)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	for _, path := range []string{"/api/auth/register", "/api/auth/login"} {
		for _, tt := range []struct {
			name, body, contentType string
			status                  int
		}{
			{"missing content type", `{}`, "", 415},
			{"wrong content type", `{}`, "text/plain", 415},
			{"empty body", "", "application/json", 400},
			{"null", "null", "application/json", 400},
			{"array", "[]", "application/json", 400},
			{"malformed", `{"email":`, "application/json", 400},
			{"unknown field", `{"extra":true}`, "application/json", 400},
			{"wrong type", `{"email":123}`, "application/json", 400},
			{"multiple objects", `{} {}`, "application/json", 400},
			{"trailing data", `{} trailing`, "application/json", 400},
			{"invalid UTF8", "{\"email\":\"\xff\"}", "application/json", 400},
			{"too large", `{"email":"` + strings.Repeat("a", 8192) + `"}`, "application/json", 413},
		} {
			t.Run(path+"/"+tt.name, func(t *testing.T) {
				r := httptest.NewRequest("POST", path, strings.NewReader(tt.body))
				r.Header.Set("Content-Type", tt.contentType)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != tt.status {
					t.Fatalf("status=%d want=%d body=%s", w.Code, tt.status, w.Body.String())
				}
			})
		}
	}
}

func TestAuthHandlerRegistrationValidation(t *testing.T) {
	mux := http.NewServeMux()
	httpapi.NewAuthHandler(auth.NewService(&authHandlerRepository{}), newHandlerTokens(t), 0).RegisterRoutes(mux)
	for _, body := range []string{
		`{}`,
		`{"email":"a@example.com","password":"a sufficiently long password"}`,
		`{"email":"a@example.com","nickname":"   ","password":"a sufficiently long password"}`,
		`{"email":"bad","nickname":"Артём","password":"a sufficiently long password"}`,
		`{"email":"a@example.com","nickname":"Артём","password":"short"}`,
	} {
		w := authRequest(mux, "POST", "/api/auth/register", body, "")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid registration: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestAuthHandlerInternalErrorsAndTimeout(t *testing.T) {
	for _, tt := range []struct {
		name   string
		err    error
		status int
	}{
		{"database error", errors.New("private database detail"), 500},
		{"timeout", context.DeadlineExceeded, 504},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service := authHandlerServiceStub{
				register: func(context.Context, string, string, string) (model.User, error) {
					return model.User{}, fmt.Errorf("storage: %w", tt.err)
				},
				login:   func(context.Context, string, string) (model.User, error) { return model.User{}, tt.err },
				getByID: func(context.Context, string) (model.User, error) { return model.User{}, tt.err },
			}
			tokens := newHandlerTokens(t)
			token, err := tokens.Issue("user-123")
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			httpapi.NewAuthHandler(service, tokens, 0).RegisterRoutes(mux)
			for _, path := range []string{"register", "login", "me"} {
				method := "POST"
				if path == "me" {
					method = "GET"
				}
				w := authRequest(mux, method, "/api/auth/"+path, `{}`, token)
				if w.Code != tt.status || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestAuthHandlerDeadlineAndCancellation(t *testing.T) {
	t.Run("deadline prevents token issuance", func(t *testing.T) {
		service := authHandlerServiceStub{login: func(ctx context.Context, _, _ string) (model.User, error) {
			<-ctx.Done()
			return model.User{ID: "user-123"}, nil
		}}
		handler := httpapi.NewAuthHandler(service, nil, time.Millisecond)
		w := authRequest(http.HandlerFunc(handler.Login), "POST", "/api/auth/login", `{}`, "")
		if w.Code != http.StatusGatewayTimeout {
			t.Fatalf("deadline: %d", w.Code)
		}
	})
	t.Run("canceled request", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		handler := httpapi.NewAuthHandler(authHandlerServiceStub{}, nil, 0)
		r := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{}`)).WithContext(ctx)
		w := httptest.NewRecorder()
		handler.Login(w, r)
		if w.Body.Len() != 0 {
			t.Fatal("response written for canceled request")
		}
	})
}

type failingAuthTokens struct{ *auth.TokenManager }

func (failingAuthTokens) Issue(string) (string, error) {
	return "", errors.New("private signing error")
}

func TestAuthHandlerTokenFailure(t *testing.T) {
	service := authHandlerServiceStub{login: func(context.Context, string, string) (model.User, error) { return model.User{ID: "user-123"}, nil }}
	handler := httpapi.NewAuthHandler(service, failingAuthTokens{newHandlerTokens(t)}, 0)
	w := authRequest(http.HandlerFunc(handler.Login), "POST", "/api/auth/login", `{}`, "")
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "private") {
		t.Fatalf("signing failure: %d %s", w.Code, w.Body.String())
	}
}
