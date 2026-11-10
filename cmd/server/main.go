package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"app-grabber-backend/docs"
	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/comparison"
	"app-grabber-backend/internal/googleplay"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/postgres"
	"app-grabber-backend/internal/rustore"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Print(err)
		stop()
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cacheTTL, err := app.ParseCacheTTL(os.Getenv("APP_CACHE_TTL"))
	if err != nil {
		return err
	}
	searchTTL := app.DefaultSearchTTL
	if val := os.Getenv("APP_SEARCH_TTL"); val != "" {
		parsed, err := app.ParseCacheTTL(val)
		if err != nil {
			return fmt.Errorf("APP_SEARCH_TTL: %w", err)
		}
		searchTTL = parsed
	}
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	gp := googleplay.New()
	if front, ok := os.LookupEnv("GP_FRONT_HOST"); ok {
		gp.FrontHost = front
	}
	if proxy := os.Getenv("GP_PROXY_URL"); proxy != "" {
		if err := gp.UseProxy(proxy); err != nil {
			return fmt.Errorf("GP_PROXY_URL: %w", err)
		}
		u, _ := url.Parse(proxy)
		log.Printf("google play via proxy %s", u.Redacted())
	}
	tokens, err := auth.NewTokenManagerFromEnv()
	if err != nil {
		return fmt.Errorf("configure JWT: %w", err)
	}
	pool, err := postgres.Open(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	log.Print("PostgreSQL connection established")
	cards := postgres.NewAppRepository(pool)
	catalog := app.NewCatalog(gp, gp, cards, "google_play", cacheTTL).WithSearchTTL(searchTTL)
	ru := rustore.Client{HTTP: &http.Client{Timeout: 10 * time.Second}}
	ruCatalog := app.NewCatalog(ru, nil, cards, "rustore", cacheTTL)
	handler := httpapi.NewHandler(catalog, 10*time.Second)
	searchHandler := httpapi.NewSearchHandler(catalog, catalog, 12*time.Second).WithRuStore(ruCatalog)

	users := postgres.NewUserRepository(pool)
	authService := auth.NewService(users)
	authHandler := httpapi.NewAuthHandler(authService, tokens, 5*time.Second)
	secure := false
	if value := os.Getenv("COOKIE_SECURE"); value != "" {
		secure, err = strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("COOKIE_SECURE: %w", err)
		}
	}
	sameSite := http.SameSiteLaxMode
	switch strings.ToLower(os.Getenv("COOKIE_SAMESITE")) {
	case "", "lax":
	case "strict":
		sameSite = http.SameSiteStrictMode
	case "none":
		if !secure {
			return errors.New("COOKIE_SAMESITE=none requires COOKIE_SECURE=true")
		}
		sameSite = http.SameSiteNoneMode
	default:
		return errors.New("COOKIE_SAMESITE must be lax, strict or none")
	}
	authHandler.WithCookieOptions(secure, sameSite)
	favoritesHandler := httpapi.NewFavoriteHandler(postgres.NewFavoritesRepository(pool), authService, tokens, 5*time.Second)
	historyRepo := postgres.NewHistoryRepository(pool)
	historyHandler := httpapi.NewHistoryHandler(historyRepo, authService, tokens, 5*time.Second)
	handler.WithHistory(historyRepo, "google_play")
	ruHandler := httpapi.NewHandler(ruCatalog, 10*time.Second).WithHistory(historyRepo, "rustore")

	mux := http.NewServeMux()
	docs.Register(mux)
	authHandler.RegisterRoutes(mux)
	favoritesHandler.RegisterRoutes(mux)
	historyHandler.RegisterRoutes(mux)
	compare := httpapi.ComparisonHandler{
		Service: &comparison.Service{GooglePlay: gp, RuStore: ru, Cache: cards, TTL: cacheTTL, StoreTimeout: 9 * time.Second},
		Store:   postgres.NewComparisonRepository(pool), History: historyHandler,
	}
	compare.RegisterRoutes(mux)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := w.Write([]byte("ok")); err != nil {
			log.Printf("write health response: %v", err)
		}
	})
	mux.Handle("GET /api/apps/{id}", historyHandler.WrapCard(http.HandlerFunc(handler.GetApp)))
	mux.Handle("GET /api/apps/{store}/{id}", historyHandler.WrapCard(httpapi.StoreAppHandler(map[string]*httpapi.Handler{
		"google_play": handler,
		"rustore":     ruHandler,
	})))
	mux.HandleFunc("GET /api/search", searchHandler.Search)

	server := &http.Server{
		Addr:              addr,
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return serve(ctx, server)
}

func serve(ctx context.Context, server *http.Server) error {
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", server.Addr)
		serverErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		log.Print("shutting down HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	}
}

func withCORS(next http.Handler) http.Handler {
	origins := os.Getenv("CORS_ORIGINS")
	if origins == "" {
		origins = "http://localhost:3000,http://localhost:5173"
	}
	allowed := make(map[string]bool)
	for _, origin := range strings.Split(origins, ",") {
		origin = strings.TrimSpace(origin)
		u, err := url.Parse(origin)
		if err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
			allowed[origin] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Vary", "Origin")
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		trusted := func(origin string) bool { return allowed[origin] || origin == scheme+"://"+r.Host }
		origin := r.Header.Get("Origin")
		deny := func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"Недопустимый источник запроса"}`))
		}
		if len(r.Header.Values("Origin")) > 1 || (origin != "" && !trusted(origin)) {
			deny()
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		unsafe := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if unsafe && origin == "" {
			referer, err := url.Parse(r.Referer())
			if err != nil || referer == nil || !trusted(referer.Scheme+"://"+referer.Host) {
				deny()
				return
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
