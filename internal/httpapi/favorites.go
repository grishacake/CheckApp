package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/favorites"
	"app-grabber-backend/internal/model"
)

var storeCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,49}$`)

type FavoriteUsers interface {
	GetByID(context.Context, string) (model.User, error)
}

type FavoriteHandler struct {
	repo    favorites.Repository
	users   FavoriteUsers
	tokens  TokenVerifier
	timeout time.Duration
}

func NewFavoriteHandler(repo favorites.Repository, users FavoriteUsers, tokens TokenVerifier, timeout time.Duration) *FavoriteHandler {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &FavoriteHandler{repo: repo, users: users, tokens: tokens, timeout: timeout}
}

func (h *FavoriteHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/favorites", WithAuth(h.tokens, http.HandlerFunc(h.List)))
	mux.Handle("PUT /api/favorites/{store}/{packageName}", WithAuth(h.tokens, http.HandlerFunc(h.Add)))
	mux.Handle("DELETE /api/favorites/{store}/{packageName}", WithAuth(h.tokens, http.HandlerFunc(h.Remove)))
}

func (h *FavoriteHandler) List(w http.ResponseWriter, r *http.Request) {
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
		items = make([]model.Favorite, 0)
	}
	writeAuthResponse(w, http.StatusOK, struct {
		Favorites []model.Favorite `json:"favorites"`
		Limit     int              `json:"limit"`
		Offset    int              `json:"offset"`
	}{items, limit, offset})
}

func (h *FavoriteHandler) Add(w http.ResponseWriter, r *http.Request)    { h.mutate(w, r, true) }
func (h *FavoriteHandler) Remove(w http.ResponseWriter, r *http.Request) { h.mutate(w, r, false) }

func (h *FavoriteHandler) mutate(w http.ResponseWriter, r *http.Request, add bool) {
	w.Header().Set("Cache-Control", "no-store")
	store, packageName := r.PathValue("store"), r.PathValue("packageName")
	if !storeCodePattern.MatchString(store) || !app.ValidPackageName(packageName) {
		writeError(w, http.StatusBadRequest, "Некорректный store или package_name")
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
	var err error
	if add {
		err = h.repo.Add(ctx, id, store, packageName)
	} else {
		err = h.repo.Remove(ctx, id, store, packageName)
	}
	if h.handleError(w, r, ctx, err) {
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func favoritePagination(r *http.Request) (int, int, error) {
	values := r.URL.Query()
	limit, offset := 20, 0
	for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
		items, exists := values[key]
		if !exists {
			continue
		}
		if len(items) != 1 || items[0] == "" {
			return 0, 0, fmt.Errorf("invalid %s", key)
		}
		value, err := strconv.Atoi(items[0])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid %s", key)
		}
		*target = value
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return 0, 0, errors.New("pagination out of range")
	}
	return limit, offset, nil
}

func (h *FavoriteHandler) handleError(w http.ResponseWriter, r *http.Request, ctx context.Context, err error) bool {
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
	case errors.Is(err, favorites.ErrCardNotFound):
		writeError(w, http.StatusNotFound, "Карточка приложения не найдена")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Превышено время ожидания")
	default:
		log.Printf("favorites %s: %v", r.URL.Path, err)
		writeError(w, http.StatusInternalServerError, "Внутренняя ошибка сервера")
	}
	return true
}
