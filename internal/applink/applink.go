// Package applink достаёт package name из ссылки на карточку приложения в Google Play или RuStore.
package applink

import (
	"net/url"
	"strings"

	"app-grabber-backend/internal/app"
)

// PackageName возвращает package name из ссылки вида
// https://play.google.com/store/apps/details?id=com.uchi.app или https://apps.rustore.ru/app/com.uchi.app.
// Схему можно не указывать. Не ссылка на карточку — false.
func PackageName(raw string) (string, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw // Кладем хост в путь
	}
	u, err := url.Parse(raw) // Парсим
	if err != nil {
		return "", false
	}

	var id string
	switch strings.ToLower(u.Hostname()) {
	case "play.google.com":
		if u.Path != "/store/apps/details" {
			return "", false
		}
		id = u.Query().Get("id")
	case "apps.rustore.ru":
		path, ok := strings.CutPrefix(u.Path, "/app/")
		if !ok {
			return "", false
		}
		id = strings.TrimSuffix(path, "/")
	default:
		return "", false
	}

	if !app.ValidPackageName(id) {
		return "", false
	}
	return id, true
}
