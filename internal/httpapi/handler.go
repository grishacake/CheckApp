package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"regexp"
	"time"

	"app-grabber-backend/internal/app"
)

var packageNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

type Handler struct {
	provider app.Provider
	timeout  time.Duration
}

func NewHandler(provider app.Provider, timeout time.Duration) *Handler {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Handler{provider: provider, timeout: timeout}
}

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	packageName := r.PathValue("id")
	if !packageNamePattern.MatchString(packageName) {
		writeError(w, http.StatusBadRequest, "Некорректный формат package_name")
		return
	}
	if r.Context().Err() != nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	card, err := h.provider.GetApp(ctx, packageName)
	if r.Context().Err() != nil {
		return
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}

	if err != nil {
		var networkError net.Error
		switch {
		case errors.Is(err, context.DeadlineExceeded),
			errors.As(err, &networkError) && networkError.Timeout():
			writeError(w, http.StatusBadGateway, "Превышено время ожидания Google Play")
		case errors.Is(err, app.ErrNotFound):
			writeError(w, http.StatusNotFound, "Приложение не найдено в Google Play")
		case errors.Is(err, app.ErrNotConfigured):
			writeError(w, http.StatusNotImplemented, "Получение данных из Google Play ещё не подключено")
		default:
			log.Printf("get app %s: %v", packageName, err)
			writeError(w, http.StatusBadGateway, "Не удалось получить данные из Google Play")
		}
		return
	}

	body, err := json.Marshal(card)
	if err != nil {
		log.Printf("encode app %s: %v", packageName, err)
		writeError(w, http.StatusBadGateway, "Не удалось получить данные из Google Play")
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	body, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: message})
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
