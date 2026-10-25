package httpapi

import (
	"context"
	"encoding/json"
	"errors"
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
	// Каждая карточка — отдельный запрос к Google с паузой против капчи, поэтому немного.
	searchLimit = 5
)

type SearchHandler struct {
	searcher app.Searcher
	provider app.Provider // карточка по ссылке
	timeout  time.Duration
}

func NewSearchHandler(searcher app.Searcher, provider app.Provider, timeout time.Duration) *SearchHandler {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &SearchHandler{searcher: searcher, provider: provider, timeout: timeout}
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
	if packageName, ok := applink.PackageName(query); ok {
		h.searchByLink(ctx, w, r, query, packageName)
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
func (h *SearchHandler) searchByLink(ctx context.Context, w http.ResponseWriter, r *http.Request, query, packageName string) {
	card, err := h.provider.GetApp(ctx, packageName)
	if r.Context().Err() != nil {
		return // клиент ушёл
	}
	switch {
	case errors.Is(err, app.ErrNotFound):
		writeSearchResults(w, query, nil)
	case errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil:
		writeError(w, http.StatusBadGateway, "Превышено время ожидания Google Play")
	case err != nil:
		log.Printf("search by link %q: %v", query, err)
		writeError(w, http.StatusBadGateway, "Не удалось получить данные из Google Play")
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
