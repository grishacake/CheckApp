package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/history"
	"app-grabber-backend/internal/model"
)

type HistoryUsers interface {
	GetByID(context.Context, string) (model.User, error)
}

type HistoryHandler struct {
	repo    history.Repository
	users   HistoryUsers
	tokens  TokenVerifier
	timeout time.Duration
}

func NewHistoryHandler(repo history.Repository, users HistoryUsers, tokens TokenVerifier, timeout time.Duration) *HistoryHandler {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HistoryHandler{repo: repo, users: users, tokens: tokens, timeout: timeout}
}

func (h *HistoryHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/history", WithAuth(h.tokens, http.HandlerFunc(h.List)))
}

func (h *HistoryHandler) WrapCard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if len(r.Header.Values("Authorization")) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		WithAuth(h.tokens, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := UserIDFromContext(r.Context())
			if !ok {
				unauthorized(w)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
			defer cancel()
			if _, err := h.users.GetByID(ctx, id); h.handleError(w, r, ctx, err) {
				return
			}
			if r.Context().Err() != nil {
				return
			}
			next.ServeHTTP(w, r)
		})).ServeHTTP(w, r)
	})
}

func (h *HistoryHandler) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	limit, offset, err := favoritePagination(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Некорректные limit или offset")
		return
	}
	id, ok := UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if _, err := h.users.GetByID(ctx, id); h.handleError(w, r, ctx, err) {
		return
	}
	items, err := h.repo.List(ctx, id, limit, offset)
	if h.handleError(w, r, ctx, err) {
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if items == nil {
		items = make([]model.View, 0)
	}
	writeAuthResponse(w, http.StatusOK, struct {
		History []model.View `json:"history"`
		Limit   int          `json:"limit"`
		Offset  int          `json:"offset"`
	}{items, limit, offset})
}

func (h *HistoryHandler) handleError(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) bool {
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
	case errors.Is(err, auth.ErrUserNotFound):
		unauthorized(w)
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Превышено время ожидания")
	default:
		log.Printf("history %s: %v", r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
	return true
}
