// Точка входа: HTTP-сервер для фронтенда.
// Сейчас это скелет: /health работает, /api/apps/{id} отвечает заглушкой 501.
//
//	go run ./cmd/server
//	curl localhost:8080/api/apps/com.uchi.app
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func main() {
	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /api/apps/{id}", getApp)

	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, withCORS(mux)))
}

// getApp — GET /api/apps/{id}: карточка приложения.
//
// TODO (хакатон): получить данные из магазина и вернуть model.App.
// Пока заглушка, чтобы фронтенд видел маршрут и формат ошибки.
func getApp(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "not implemented yet"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // чтобы & в ссылках не превращался в &
	enc.Encode(v)
}

// withCORS разрешает фронтенду (другой порт на localhost) ходить к нам из браузера.
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
