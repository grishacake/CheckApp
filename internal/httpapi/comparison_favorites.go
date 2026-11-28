package httpapi

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
)

var comparisonIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (h ComparisonHandler) favorites(w http.ResponseWriter, r *http.Request) {
	user, ok := UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx := r.Context()
	id := r.PathValue("id")
	if id != "" && !comparisonIDPattern.MatchString(id) {
		writeError(w, 400, "Некорректный id сравнения")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var request struct {
			Items []comparison.Input `json:"items"`
		}
		if !decodeJSONRequest(w, r, &request, 1<<20) {
			return
		}
		items, err := comparison.Normalize(request.Items)
		if err != nil {
			writeError(w, 400, err.Error())
			return
		}
		for _, item := range items {
			if !h.Service.Supports(item.Store) {
				writeError(w, 400, "Магазин не подключён: "+item.Store)
				return
			}
		}
		favorite, err := h.Favorites.SaveFavorite(ctx, user, items)
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		writeAuthResponse(w, 200, favorite)
	case http.MethodGet, http.MethodHead:
		if id == "" {
			limit, offset, err := favoritePagination(r)
			if err != nil {
				writeError(w, 400, "Некорректные limit или offset")
				return
			}
			items, err := h.Favorites.ListFavorites(ctx, user, limit, offset)
			if h.History.handleError(w, r, ctx, err) {
				return
			}
			writeAuthResponse(w, 200, struct {
				Items  []comparison.Favorite `json:"items"`
				Limit  int                   `json:"limit"`
				Offset int                   `json:"offset"`
			}{items, limit, offset})
			return
		}
		favorite, err := h.Favorites.GetFavorite(ctx, user, id)
		if errors.Is(err, app.ErrNotFound) {
			writeError(w, 404, "Сравнение не найдено")
			return
		}
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		saved := make([]comparison.SavedItem, len(favorite.Items))
		for i, item := range favorite.Items {
			saved[i] = comparison.SavedItem{Item: item, AddedAt: favorite.SavedAt}
		}
		results, err := h.Service.Resolve(ctx, saved)
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		writeAuthResponse(w, 200, struct {
			ID      string                  `json:"id"`
			SavedAt time.Time               `json:"saved_at"`
			Items   []comparison.ItemResult `json:"items"`
		}{favorite.ID, favorite.SavedAt, results})
	case http.MethodDelete:
		if h.History.handleError(w, r, ctx, h.Favorites.DeleteFavorite(ctx, user, id)) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
