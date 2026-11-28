package httpapi

import (
	"fmt"
	"net/http"

	"app-grabber-backend/internal/comparison"
)

func (h ComparisonHandler) items(w http.ResponseWriter, r *http.Request) {
	user, ok := UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		if r.URL.RawQuery != "" {
			writeError(w, 400, "GET /api/compare возвращает сохранённый список без параметров запроса")
			return
		}
		items, err := h.Items.ListItems(ctx, user)
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		results, err := h.Service.Resolve(ctx, items)
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		writeAuthResponse(w, 200, struct {
			Items []comparison.ItemResult `json:"items"`
		}{results})
	case http.MethodPost:
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
		for n, item := range items {
			if !h.Service.Supports(item.Store) {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("items[%d]: магазин %q не подключён", n, item.Store))
				return
			}
		}
		if h.History.handleError(w, r, ctx, h.Items.AddItems(ctx, user, items)) {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		store := r.PathValue("store")
		if store == "" {
			if r.URL.RawQuery != "" {
				writeError(w, 400, "Для удаления приложения укажите store и package_name в пути")
				return
			}
			if h.History.handleError(w, r, ctx, h.Items.ClearItems(ctx, user)) {
				return
			}
		} else {
			item := comparison.Item{Store: store, PackageName: r.PathValue("package_name")}
			if !item.Valid() {
				writeError(w, 400, "Некорректный store или package_name")
				return
			}
			if h.History.handleError(w, r, ctx, h.Items.RemoveItem(ctx, user, item)) {
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
