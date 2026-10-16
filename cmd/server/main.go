package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"app-grabber-backend/docs"
	"app-grabber-backend/internal/googleplay"
	"app-grabber-backend/internal/httpapi"
)

func main() {
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	// Библиотека Google Play ходит через http.DefaultClient и не принимает контекст:
	// без тайм-аута зависший запрос к Google висел бы в фоне бесконечно.
	http.DefaultClient.Timeout = 15 * time.Second

	// Один клиент на карточку и поиск, чтобы пауза между запросами к Google была общей.
	gp := googleplay.New()
	handler := httpapi.NewHandler(gp, 10*time.Second)
	searchHandler := httpapi.NewSearchHandler(gp, 12*time.Second)

	mux := http.NewServeMux()
	docs.Register(mux)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := w.Write([]byte("ok")); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	mux.HandleFunc("GET /api/apps/{id}", handler.GetApp)
	mux.HandleFunc("GET /api/search", searchHandler.Search)

	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
