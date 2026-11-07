package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"time"
	"unicode/utf8"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/model"
)

type AuthService interface {
	Register(ctx context.Context, email, nickname, password string) (model.User, error)
	Login(ctx context.Context, email, password string) (model.User, error)
	GetByID(ctx context.Context, id string) (model.User, error)
}

type AuthTokens interface {
	TokenVerifier
	Issue(userID string) (string, error)
	Lifetime() time.Duration
}

type AuthHandler struct {
	service        AuthService
	tokens         AuthTokens
	timeout        time.Duration
	cookieSecure   bool
	cookieSameSite http.SameSite
}

type registerRequest struct {
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	User model.User `json:"user"`
}

func NewAuthHandler(service AuthService, tokens AuthTokens, timeout time.Duration) *AuthHandler {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &AuthHandler{service: service, tokens: tokens, timeout: timeout, cookieSecure: false, cookieSameSite: http.SameSiteLaxMode}
}

func (h *AuthHandler) WithCookieOptions(secure bool, sameSite http.SameSite) *AuthHandler {
	h.cookieSecure, h.cookieSameSite = secure, sameSite
	return h
}

func (h *AuthHandler) sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{Name: AuthCookieName, Value: value, Path: "/", HttpOnly: true,
		Secure: h.cookieSecure, SameSite: h.cookieSameSite, MaxAge: maxAge}
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cookie := h.sessionCookie("", -1)
	cookie.Expires = time.Unix(1, 0)
	http.SetCookie(w, cookie)
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", h.Register)
	mux.HandleFunc("POST /api/auth/login", h.Login)
	mux.HandleFunc("POST /api/auth/logout", h.Logout)
	mux.Handle("GET /api/auth/me", WithAuth(h.tokens, http.HandlerFunc(h.Me)))
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		return
	}
	var input registerRequest
	if !decodeAuthRequest(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	user, err := h.service.Register(ctx, input.Email, input.Nickname, input.Password)
	if handleAuthError(w, r, ctx, err) {
		return
	}
	writeAuthResponse(w, http.StatusCreated, user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		return
	}
	var input loginRequest
	if !decodeAuthRequest(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	user, err := h.service.Login(ctx, input.Email, input.Password)
	if handleAuthError(w, r, ctx, err) {
		return
	}
	token, err := h.tokens.Issue(user.ID)
	if handleAuthError(w, r, ctx, err) {
		return
	}
	body, err := json.Marshal(loginResponse{User: user})
	if handleAuthError(w, r, ctx, err) {
		return
	}
	cookie := h.sessionCookie(token, int(h.tokens.Lifetime()/time.Second))
	cookie.Expires = time.Now().Add(h.tokens.Lifetime()).UTC()
	http.SetCookie(w, cookie)
	writeJSON(w, http.StatusOK, body)
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Context().Err() != nil {
		return
	}
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	user, err := h.service.GetByID(ctx, id)
	if handleAuthError(w, r, ctx, err) {
		return
	}
	writeAuthResponse(w, http.StatusOK, user)
}

func decodeAuthRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Ожидается Content-Type: application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "Тело запроса превышает 8 КиБ")
		} else {
			writeError(w, http.StatusBadRequest, "Не удалось прочитать тело запроса")
		}
		return false
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 || body[0] != '{' || !utf8.Valid(body) {
		writeError(w, http.StatusBadRequest, "Ожидается JSON-объект")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Некорректный JSON или неизвестные поля")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "Ожидается один JSON-объект")
		return false
	}
	return true
}

func handleAuthError(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) bool {
	if r.Context().Err() != nil {
		return true
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(w, http.StatusBadRequest, "Некорректный email")
	case errors.Is(err, auth.ErrInvalidNickname):
		writeError(w, http.StatusBadRequest, "Ник обязателен: от 1 до 50 символов, без управляющих символов")
	case errors.Is(err, auth.ErrInvalidPassword):
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Пароль должен содержать от %d до %d символов", auth.MinPasswordLength, auth.MaxPasswordLength))
	case errors.Is(err, auth.ErrEmailAlreadyExists):
		writeError(w, http.StatusConflict, "Пользователь с таким email уже существует")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "Неверный email или пароль")
	case errors.Is(err, auth.ErrUserNotFound):
		unauthorized(w)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Превышено время ожидания")
	default:
		log.Printf("auth %s: %v", r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
	return true
}

func writeAuthResponse(w http.ResponseWriter, status int, response any) {
	body, err := json.Marshal(response)
	if err != nil {
		log.Printf("encode auth response: %v", err)
		writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
		return
	}
	writeJSON(w, status, body)
}
