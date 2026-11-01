package docs

import (
	_ "embed"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger/v2"
)

//go:embed openapi.json
var specification []byte

func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(specification)
	})
	redirect := func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/index.html", http.StatusTemporaryRedirect)
	}
	mux.HandleFunc("GET /docs", redirect)
	mux.HandleFunc("GET /docs/{$}", redirect)
	mux.HandleFunc("GET /docs/", httpSwagger.Handler(httpSwagger.URL("/openapi.json")))
}
