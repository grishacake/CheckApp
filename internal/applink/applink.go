// Package applink определяет магазин и package name по ссылке на карточку.
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
	case "apkpure.com", "www.apkpure.com", "apkpure.net", "www.apkpure.net":
		link.Store = "apkpure"
		path := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), "/")
		// APKPure serves /slug/package and /p/package, optionally with a locale prefix.
		if len(path) == 3 && isLocale(path[0]) {
			path = path[1:]
		}
		if len(path) != 2 || path[0] == "" || path[0] == "." || path[0] == ".." || u.Host != u.Hostname() {
			return link, false
		}
		id = path[1]
	default:
		return Link{}, false
	}

	if !app.ValidPackageName(id) {
		return link, false
	}
	link.PackageName = id
	return link, true
}

func isLocale(segment string) bool {
	language, region, hasRegion := strings.Cut(strings.ToLower(segment), "-")
	isCode := func(code string) bool {
		return len(code) == 2 && code[0] >= 'a' && code[0] <= 'z' && code[1] >= 'a' && code[1] <= 'z'
	}
	return isCode(language) && (!hasRegion || isCode(region))
}
