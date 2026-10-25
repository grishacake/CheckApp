package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/auth"
)

type HistoryRecorder interface {
	Record(context.Context, string, string, string) error
}

type Handler struct {
	provider app.Provider
	timeout  time.Duration
	history  HistoryRecorder
	store    string
}

func NewHandler(provider app.Provider, timeout time.Duration) *Handler {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Handler{provider: provider, timeout: timeout}
}

func (h *Handler) WithHistory(recorder HistoryRecorder, store string) *Handler {
	h.history = recorder
	h.store = store
	return h
}

func (h *Handler) GetApp(w http.ResponseWriter, r *http.Request) {
	packageName := r.PathValue("id")
	if !app.ValidPackageName(packageName) {
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
	if ctx.Err() != nil && !errors.Is(err, app.ErrPersistence) && !errors.Is(err, app.ErrCacheRead) {
		err = ctx.Err()
	}

	if err != nil {
		var networkError net.Error
		switch {
		case errors.Is(err, app.ErrCacheRead):
			log.Printf("read cached app %s: %v", packageName, err)
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать сохранённые данные приложения")
		case errors.Is(err, app.ErrPersistence):
			log.Printf("save app %s: %v", packageName, err)
			writeError(w, http.StatusInternalServerError, "Не удалось сохранить данные приложения")
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
	if r.Method == http.MethodGet && h.history != nil {
		userID, _ := UserIDFromContext(ctx)
		if r.Context().Err() != nil {
			return
		}
		if ctx.Err() != nil {
			writeError(w, http.StatusGatewayTimeout, "Превышено время ожидания")
			return
		}
		err := h.history.Record(ctx, userID, h.store, packageName)
		if r.Context().Err() != nil {
			return
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrUserNotFound):
				unauthorized(w)
			case errors.Is(err, context.DeadlineExceeded):
				writeError(w, http.StatusGatewayTimeout, "Превышено время ожидания")
			default:
				log.Printf("record app view %s: %v", packageName, err)
				writeError(w, http.StatusInternalServerError, "Не удалось записать просмотр приложения")
			}
			return
		}
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
