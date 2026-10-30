// Package googleplay получает карточку приложения из Google Play.
//
// Страницу качаем сами (запрос отменяется вместе с ctx), разбор — в details.go.
// Здесь — выбор региона, приведение ответа к model.App и перевод ошибок:
// app.ErrNotFound (хендлер отвечает 404) или ErrUnavailable (502).
package googleplay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

const (
	playHost  = "play.google.com"
	detailURL = "https://" + playHost + "/store/apps/details"
)

// DefaultFrontHost — хост, через который ходим к Google Play. Из России play.google.com режут
// по имени сайта в начале TLS (SNI), а www.google.com — нет. Сервер у Google общий:
// соединяемся с www.google.com, а в заголовке Host просим play.google.com — отдаётся та же страница.
const DefaultFrontHost = "www.google.com"

// maxPageSize — максимальный размер страницы. Обычная карточка весит около 1,3 МБ.
const maxPageSize = 10 << 20

// ErrUnavailable — Google Play не ответил, ответил ошибкой или страница не разобралась (капча, новая разметка).
// Хендлер отвечает на неё 502.
var ErrUnavailable = errors.New("google play unavailable")

// Client реализует app.Provider для Google Play.
type Client struct {
	// HTTP — клиент для запросов к Google. Время на запрос задаёт ctx вызывающего,
	// Timeout клиента — запасной, на случай ctx без дедлайна.
	HTTP *http.Client
	// Language — язык карточки (hl): название категории, возрастной рейтинг.
	Language string
	// Countries — регионы (gl) по порядку. Следующий пробуем, только если в предыдущем 404:
	// например, ВКонтакте нет в российском Google Play, но он есть в американском.
	Countries []string
	// MinInterval — минимальная пауза между запросами к Google, чтобы не получить капчу.
	MinInterval time.Duration
	// FrontHost — хост для соединения вместо play.google.com (см. DefaultFrontHost).
	// Пусто — ходим на play.google.com напрямую.
	FrontHost string

	mu   sync.Mutex
	next time.Time // раньше этого момента новый запрос не отправляем
}

var _ app.Provider = (*Client)(nil)

// New возвращает клиент с настройками по умолчанию: русский язык, сначала RU, потом US.
func New() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 15 * time.Second},
		Language:    "ru",
		Countries:   []string{"ru", "us"},
		MinInterval: 300 * time.Millisecond,
		FrontHost:   DefaultFrontHost,
	}
}

// UseProxy направляет запросы к Google через прокси, например http://login:password@1.2.3.4:10000.
// Подходят схемы http, https и socks5. Пустая строка — без прокси.
func (c *Client) UseProxy(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("proxy url: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return fmt.Errorf("proxy url: unsupported scheme %q, want http, https or socks5", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("proxy url: no host")
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = http.ProxyURL(u)
	c.HTTP.Transport = t
	return nil
}

// GetApp возвращает карточку приложения. Тайм-аут задаёт вызывающий через ctx:
// при отмене запрос к Google обрывается.
func (c *Client) GetApp(ctx context.Context, id string) (model.App, error) {
	var lastErr error
	for _, country := range c.Countries {
		card, err := c.load(ctx, id, country)
		if err == nil {
			return card, nil
		}
		lastErr = err
		if !isNotFound(err) {
			break // магазин не ответил — другой регион не поможет
		}
	}
	if isNotFound(lastErr) {
		return model.App{}, fmt.Errorf("%w: %s", app.ErrNotFound, id)
	}
	return model.App{}, fmt.Errorf("%w: %w", ErrUnavailable, lastErr)
}

// load запрашивает одну страницу. 404 возвращает как *statusError, его проверяет isNotFound.
func (c *Client) load(ctx context.Context, id, country string) (model.App, error) {
	if err := c.wait(ctx); err != nil {
		return model.App{}, err
	}

	q := url.Values{"id": {id}, "gl": {country}, "hl": {c.Language}}
	page, err := c.get(ctx, detailURL+"?"+q.Encode())
	if err != nil {
		return model.App{}, err
	}
	d := parseDetails(page)
	if d.Title == "" {
		// 200, но данных нет: капча или Google поменял разметку
		return model.App{}, fmt.Errorf("no app data on page (gl=%s)", country)
	}
	return toModel(id, d, c.Language, country), nil
}

// statusError — Google ответил кодом, отличным от 200.
type statusError struct {
	Code   int
	Status string
}

func (e *statusError) Error() string {
	return "google play responded " + e.Status
}

// get скачивает страницу. Код ответа не 200 — *statusError.
func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.FrontHost != "" && req.URL.Host == playHost {
		// соединение (и SNI) — с FrontHost, а Host — play.google.com
		req.Host = playHost
		req.URL.Host = c.FrontHost
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{Code: resp.StatusCode, Status: resp.Status}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageSize+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxPageSize {
		return nil, fmt.Errorf("google play page exceeds %d bytes", maxPageSize)
	}
	return body, nil
}

// wait выдерживает MinInterval между запросами.
func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	now := time.Now()
	start := c.next
	if start.Before(now) {
		start = now
	}
	c.next = start.Add(c.MinInterval)
	c.mu.Unlock()

	t := time.NewTimer(time.Until(start))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// isNotFound — Google ответил 404: в этом регионе приложения нет.
func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

func toModel(id string, d details, language, country string) model.App {
	return model.App{
		PackageName: id,
		StoreURL:    storeURL(id, language, country),
		Name:        d.Title,
		Developer:   d.Developer,
		IconURL:     d.Icon,
		Category:    d.Genre,
		Rating:      math.Round(d.Score*10) / 10,
		AgeRating:   normalizeAge(d.ContentRating),
	}
}

// storeURL — ссылка на карточку. gl добавляем, только если приложение нашлось не в RU:
// иначе из России по ссылке без gl откроется «не найдено».
func storeURL(id, language, country string) string {
	u := detailURL + "?id=" + url.QueryEscape(id) + "&hl=" + url.QueryEscape(language)
	if country != "ru" {
		u += "&gl=" + url.QueryEscape(country)
	}
	return u
}

var ageRe = regexp.MustCompile(`(\d+)\s*\+`)

// normalizeAge приводит возрастной рейтинг к виду "13+".
// В RU Google Play отдаёт "3+", в US — ESRB: "T (13+)", "Для всех" (без цифр, оставляем как есть).
func normalizeAge(s string) string {
	if m := ageRe.FindStringSubmatch(s); m != nil {
		return m[1] + "+"
	}
	return strings.TrimSpace(s)
}
