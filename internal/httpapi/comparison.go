package httpapi

import (
	"context"
	"net/http"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
)

type ComparisonStore interface {
	Record(context.Context, string, comparison.Pair) error
	SetFavorite(context.Context, string, comparison.Pair, bool) error
	List(context.Context, string, bool, int, int) ([]comparison.Entry, error)
}

type ComparisonHandler struct {
	Service *comparison.Service
	Store   ComparisonStore
	History *HistoryHandler
}

func (h ComparisonHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/compare", h.History.WrapCard(http.HandlerFunc(h.compare)))
	for _, pattern := range []string{"GET /api/compare/history", "GET /api/compare/favorites", "PUT /api/compare/favorites", "DELETE /api/compare/favorites"} {
		mux.Handle(pattern, h.History.WrapCard(http.HandlerFunc(h.collection)))
	}
}

func comparisonPair(w http.ResponseWriter, r *http.Request) (comparison.Pair, bool) {
	q := r.URL.Query()
	p := comparison.Pair{GooglePlay: q.Get("google_play"), RuStore: q.Get("rustore")}
	for _, key := range []string{"google_play", "rustore"} {
		if len(q[key]) != 1 || len(q.Get(key)) > 255 || !app.ValidPackageName(q.Get(key)) {
			writeError(w, 400, "Укажите корректные google_play и rustore")
			return comparison.Pair{}, false
		}
	}
	return p, true
}

func (h ComparisonHandler) compare(w http.ResponseWriter, r *http.Request) {
	p, ok := comparisonPair(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	result, err := h.Service.Compare(ctx, p)
	if h.History.handleError(w, r, ctx, err) {
		return
	}
	user, authenticated := UserIDFromContext(ctx)
	if r.Method == http.MethodGet && authenticated && (result.GooglePlay.App != nil || result.RuStore.App != nil) {
		if h.History.handleError(w, r, ctx, h.Store.Record(ctx, user, p)) {
			return
		}
	}
	status := http.StatusOK
	if result.GooglePlay.Status == "unavailable" && result.RuStore.Status == "unavailable" {
		status = http.StatusServiceUnavailable
	}
	writeAuthResponse(w, status, result)
}

func (h ComparisonHandler) collection(w http.ResponseWriter, r *http.Request) {
	user, ok := UserIDFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.History.timeout)
	defer cancel()
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		limit, offset, err := favoritePagination(r)
		if err != nil {
			writeError(w, 400, "Некорректные limit или offset")
			return
		}
		entries, err := h.Store.List(ctx, user, r.URL.Path == "/api/compare/favorites", limit, offset)
		if h.History.handleError(w, r, ctx, err) {
			return
		}
		writeAuthResponse(w, 200, struct {
			Items  []comparison.Entry `json:"items"`
			Limit  int                `json:"limit"`
			Offset int                `json:"offset"`
		}{entries, limit, offset})
		return
	}
	p, ok := comparisonPair(w, r)
	if !ok {
		return
	}
	err := h.Store.SetFavorite(ctx, user, p, r.Method == http.MethodPut)
	if h.History.handleError(w, r, ctx, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
