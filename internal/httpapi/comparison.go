package httpapi

import (
	"context"
	"net/http"
	"time"

	"app-grabber-backend/internal/comparison"
)

type ComparisonHandler struct {
	Service *comparison.Service
	Items   comparison.Repository
	History *HistoryHandler
}

func (h ComparisonHandler) RegisterRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.Handler {
		authenticated := h.History.WrapCard(handler)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
			defer cancel()
			authenticated.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	for _, pattern := range []string{"GET /api/compare", "POST /api/compare", "DELETE /api/compare/all", "DELETE /api/compare/{store}/{package_name}"} {
		mux.Handle(pattern, wrap(h.items))
	}
}
