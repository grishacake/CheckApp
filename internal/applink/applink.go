// Package applink достаёт package name из ссылки на карточку приложения в Google Play или RuStore.
package applink

import (
	"net/url"
	"strings"

	"app-grabber-backend/internal/app"
)

type Link struct {
	Store       string
	PackageName string
}

func PackageName(raw string) (string, bool) {
	link, ok := Parse(raw)
	return link.PackageName, ok
}

func Parse(raw string) (Link, bool) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return Link{}, false
	}

	var id string
	var link Link
	switch strings.ToLower(u.Hostname()) {
	case "play.google.com":
		link.Store = "google_play"
		if u.Path != "/store/apps/details" {
			return link, false
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(query["id"]) != 1 {
			return link, false
		}
		id = query.Get("id")
	case "apps.rustore.ru":
		link.Store = "rustore"
		path, ok := strings.CutPrefix(u.Path, "/app/")
		if !ok {
			return link, false
		}
		id = strings.TrimSuffix(path, "/")
	case "www.rustore.ru", "rustore.ru":
		link.Store = "rustore"
		path, ok := strings.CutPrefix(u.Path, "/catalog/app/")
		if !ok {
			return link, false
		}
		id = strings.TrimSuffix(path, "/")
	default:
		return Link{}, false
	}

	if !app.ValidPackageName(id) {
		return link, false
	}
	link.PackageName = id
	return link, true
}
