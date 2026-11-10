package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/applink"
	"app-grabber-backend/internal/model"
)

const (
	maxQueryLength = 100
	searchLimit = 5
)

type SearchHandler struct {
	searcher app.Searcher
	provider app.Provider // карточка по ссылке
	ruStore  app.Provider
	timeout  time.Duration
}

func NewSearchHandler(searcher app.Searcher, provider app.Provider, timeout time.Duration) *SearchHandler {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &SearchHandler{searcher: searcher, provider: provider, timeout: timeout}
}

func (h *SearchHandler) WithRuStore(provider app.Provider) *SearchHandler {
	h.ruStore = provider
	return h
}

type searchResponse struct {
	Query   string      `json:"query"`
	Results []model.App `json:"results"`
}

// Search — GET /api/search?q=google chrome: до 5 карточек в порядке выдачи Google Play.
// Если q — ссылка на карточку в Google Play или RuStore, возвращает эту одну карточку.
func (h *SearchHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeError(w, http.StatusBadRequest, "Пустой поисковый запрос")
		return
	}
	if !utf8.ValidString(query) {
		// иначе Google получит мусор и вернёт случайные приложения
		writeError(w, http.StatusBadRequest, "Поисковый запрос должен быть в UTF-8")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	// Ссылку проверяем до ограничения длины: с utm-метками она бывает длиннее 100 символов.
	link, ok := applink.Parse(query)
	if ok {
		h.searchByLink(ctx, w, r, query, link)
		return
	}
	if link.Store != "" || strings.Contains(query, "://") {
		writeError(w, http.StatusBadRequest, "Некорректная или неподдерживаемая ссылка на приложение")
		return
	}

	if utf8.RuneCountInString(query) > maxQueryLength {
		writeError(w, http.StatusBadRequest, "Слишком длинный поисковый запрос")
		return
	}

	results, err := h.searcher.Search(ctx, query, searchLimit)
	if r.Context().Err() != nil {
		return // клиент ушёл
	}
	if err != nil {
		if errors.Is(err, app.ErrCacheRead) {
			log.Printf("read cached search results %q: %v", query, err)
			writeError(w, http.StatusInternalServerError, "Не удалось прочитать сохранённые данные приложения")
			return
		}
		if errors.Is(err, app.ErrPersistence) {
			log.Printf("save search results %q: %v", query, err)
			writeError(w, http.StatusInternalServerError, "Не удалось сохранить результаты поиска")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			writeError(w, http.StatusBadGateway, "Превышено время ожидания Google Play")
			return
		}
		log.Printf("search %q: %v", query, err)
		writeError(w, http.StatusBadGateway, "Не удалось выполнить поиск в Google Play")
		return
	}
	writeSearchResults(w, query, results)
}

// searchByLink отвечает карточкой приложения из ссылки. Не найдено — пустой results, как у поиска.
func (h *SearchHandler) searchByLink(ctx context.Context, w http.ResponseWriter, r *http.Request, query string, link applink.Link) {
	provider, storeName := h.provider, "Google Play"
	if link.Store == "rustore" {
		provider, storeName = h.ruStore, "RuStore"
	}
	if provider == nil {
		writeError(w, http.StatusNotImplemented, fmt.Sprintf("Получение данных из %s ещё не подключено", storeName))
		return
	}
	card, err := provider.GetApp(ctx, link.PackageName)
	if r.Context().Err() != nil {
		return // клиент ушёл
	}
	switch {
	case errors.Is(err, app.ErrNotFound):
		writeSearchResults(w, query, nil)
	case errors.Is(err, app.ErrCacheRead):
		log.Printf("read cached app for link %q: %v", query, err)
		writeError(w, http.StatusInternalServerError, "Не удалось прочитать сохранённые данные приложения")
	case errors.Is(err, app.ErrPersistence):
		log.Printf("save app for link %q: %v", query, err)
		writeError(w, http.StatusInternalServerError, "Не удалось сохранить данные приложения")
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		writeError(w, http.StatusBadGateway, "Превышено время ожидания "+storeName)
	case err != nil:
		log.Printf("search by link %q: %v", query, err)
		writeError(w, http.StatusBadGateway, "Не удалось получить данные из "+storeName)
	default:
		writeSearchResults(w, query, []model.App{card})
	}
}

func writeSearchResults(w http.ResponseWriter, query string, results []model.App) {
	if results == nil {
		results = []model.App{} // в JSON — [], а не null
	}
	body, err := json.Marshal(searchResponse{Query: query, Results: results})
	if err != nil {
		log.Printf("encode search %q: %v", query, err)
		writeError(w, http.StatusBadGateway, "Не удалось выполнить поиск в Google Play")
		return
	}
	writeJSON(w, http.StatusOK, body)
}
