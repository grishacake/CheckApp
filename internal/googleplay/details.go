package googleplay

// Разбор страницы карточки перенесён из github.com/Pius-x/google-play-scraper v0.0.2
// (форк n0madic/google-play-scraper, LGPL-3.0): регулярки для блоков AF_initDataCallback
// и JSON-пути до полей. Из библиотеки взяты только поля, которые нужны model.App.

import (
	"regexp"

	"github.com/tidwall/gjson"
)

// Данные карточки лежат в <script>AF_initDataCallback({key: 'ds:N', ..., data: [...], sideChannel: {}});</script>.
var (
	initDataRe  = regexp.MustCompile(`>AF_initDataCallback[\s\S]*?</script`)
	initValueRe = regexp.MustCompile(`data:([\s\S]*?), sideChannel: {}}\);</`)
)

// JSON-пути внутри блока с карточкой.
const (
	pathDeveloperURL  = "1.2.68.1.4.2" // ссылка на страницу разработчика: по ней узнаём блок с карточкой
	pathTitle         = "1.2.0.0"
	pathDeveloper     = "1.2.68.0"
	pathIcon          = "1.2.95.0.3.2"
	pathGenre         = "1.2.79.0.0.0"
	pathScore         = "1.2.51.0.1"
	pathContentRating = "1.2.9.0"
)

// details — поля карточки в том виде, в каком их отдаёт Google Play.
type details struct {
	Title         string
	Developer     string
	Icon          string
	Genre         string
	Score         float64
	ContentRating string
}

// parseDetails достаёт карточку из HTML страницы details?id=....
// Блока с карточкой нет (капча, новая разметка) — пустой details, это проверяет вызывающий.
func parseDetails(page []byte) details {
	for _, script := range initDataRe.FindAll(page, -1) {
		m := initValueRe.FindSubmatch(script)
		if m == nil {
			continue
		}
		data := string(m[1])
		if gjson.Get(data, pathDeveloperURL).String() == "" {
			continue // другой блок страницы: похожие приложения, отзывы и т. п.
		}
		return details{
			Title:         gjson.Get(data, pathTitle).String(),
			Developer:     gjson.Get(data, pathDeveloper).String(),
			Icon:          gjson.Get(data, pathIcon).String(),
			Genre:         gjson.Get(data, pathGenre).String(),
			Score:         gjson.Get(data, pathScore).Float(), // в библиотеке ровная оценка 5 читалась как 0
			ContentRating: gjson.Get(data, pathContentRating).String(),
		}
	}
	return details{}
}
