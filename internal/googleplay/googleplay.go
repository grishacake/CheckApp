// Package googleplay получает карточку приложения из Google Play.
//
// Страницу скачивает и разбирает библиотека github.com/Pius-x/google-play-scraper
// (форк n0madic/google-play-scraper с тем же API — запасной вариант, если Google сломает разбор).
// Здесь — выбор региона, приведение ответа к model.App и перевод ошибок:
// app.ErrNotFound (хендлер отвечает 404) или ErrUnavailable (502).
package googleplay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	gp "github.com/Pius-x/google-play-scraper"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
)

const detailURL = "https://play.google.com/store/apps/details"

// ErrUnavailable — Google Play не ответил, ответил ошибкой или страница не разобралась (капча, новая разметка).
// Хендлер отвечает на неё 502.
var ErrUnavailable = errors.New("google play unavailable")

// Client реализует app.Provider для Google Play.
type Client struct {
	// Language — язык карточки (hl): название категории, возрастной рейтинг.
	Language string
	// Countries — регионы (gl) по порядку. Следующий пробуем, только если в предыдущем 404:
	// например, ВКонтакте нет в российском Google Play, но он есть в американском.
	Countries []string
	// MinInterval — минимальная пауза между запросами к Google, чтобы не получить капчу.
	MinInterval time.Duration

	mu   sync.Mutex
	next time.Time // раньше этого момента новый запрос не отправляем
}

var _ app.Provider = (*Client)(nil)

// New возвращает клиент с настройками по умолчанию: русский язык, сначала RU, потом US.
func New() *Client {
	return &Client{
		Language:    "ru",
		Countries:   []string{"ru", "us"},
		MinInterval: 300 * time.Millisecond,
	}
}

// GetApp возвращает карточку приложения.
// Тайм-аут задаёт вызывающий через ctx: библиотека контекст не принимает,
// поэтому при отмене мы перестаём ждать, а сам запрос дорабатывает в фоне.
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

// load запрашивает одну страницу и возвращает ошибку библиотеки как есть.
func (c *Client) load(ctx context.Context, id, country string) (model.App, error) {
	if err := c.wait(ctx); err != nil {
		return model.App{}, err
	}

	type result struct {
		app *gp.App
		err error
	}
	done := make(chan result, 1) // с буфером, чтобы горутина не зависла после отмены ctx
	go func() {
		a := gp.New(id, gp.Options{Country: country, Language: c.Language})
		done <- result{a, a.LoadDetails()}
	}()

	select {
	case <-ctx.Done():
		return model.App{}, ctx.Err()
	case r := <-done:
		if r.err != nil {
			return model.App{}, r.err
		}
		if r.app.Title == "" {
			// 200, но данных нет: капча или Google поменял разметку
			return model.App{}, fmt.Errorf("no app data on page (gl=%s)", country)
		}
		return toModel(r.app, c.Language, country), nil
	}
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

// isNotFound: библиотека не отдаёт код ответа, только текст "request error: 404 Not Found".
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

func toModel(a *gp.App, language, country string) model.App {
	return model.App{
		PackageName: a.ID,
		StoreURL:    storeURL(a.ID, language, country),
		Name:        a.Title,
		Developer:   a.Developer,
		IconURL:     a.Icon,
		Category:    a.Genre,
		Rating:      math.Round(a.Score*10) / 10,
		AgeRating:   normalizeAge(a.ContentRating),
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
