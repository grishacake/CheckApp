package googleplay

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Каждая сохранённая карточка (RU и US) должна разбираться во все поля.
func TestParseDetails_AllFixtures(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		name := filepath.Base(path)
		if strings.HasPrefix(name, "search.") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			page, err := readGzip(path)
			if err != nil {
				t.Fatal(err)
			}
			d := parseDetails(page)
			for field, v := range map[string]string{
				"title": d.Title, "developer": d.Developer, "icon": d.Icon,
				"genre": d.Genre, "content_rating": d.ContentRating,
			} {
				if v == "" {
					t.Errorf("%s is empty", field)
				}
			}
			if d.Score <= 0 || d.Score > 5 {
				t.Errorf("score = %v, want 0..5", d.Score)
			}
		})
	}
}

// Ровная оценка без дробной части не должна превращаться в 0.
func TestParseDetails_IntegerScore(t *testing.T) {
	page := fakePage(map[string]any{
		pathDeveloperURL: "/store/apps/dev?id=1",
		pathTitle:        "App",
		pathScore:        5,
	})
	if d := parseDetails(page); d.Title != "App" || d.Score != 5 {
		t.Fatalf("details = %+v, want title App and score 5", d)
	}
}

// Карточку берём из блока со ссылкой на разработчика, остальные блоки пропускаем.
func TestParseDetails_SkipsOtherBlocks(t *testing.T) {
	page := append(
		fakePage(map[string]any{pathTitle: "Similar app"}),
		fakePage(map[string]any{pathDeveloperURL: "/store/apps/dev?id=1", pathTitle: "App"})...,
	)
	if d := parseDetails(page); d.Title != "App" {
		t.Fatalf("title = %q, want App", d.Title)
	}
}

func TestParseDetails_NoCard(t *testing.T) {
	for _, page := range [][]byte{
		[]byte("<html>captcha</html>"),
		fakePage(map[string]any{pathTitle: "Similar app"}),
	} {
		if d := parseDetails(page); d != (details{}) {
			t.Errorf("details = %+v, want empty", d)
		}
	}
}

// fakePage собирает блок AF_initDataCallback, в котором по JSON-путям лежат заданные значения.
func fakePage(values map[string]any) []byte {
	var root any
	for path, v := range values {
		root = setPath(root, strings.Split(path, "."), v)
	}
	data, _ := json.Marshal(root)
	return []byte(fmt.Sprintf(
		"<script nonce=\"x\">AF_initDataCallback({key: 'ds:5', hash: '1', data:%s, sideChannel: {}});</script>", data))
}

// setPath кладёт v в массив node по индексам path, расширяя массивы null-ами.
func setPath(node any, path []string, v any) any {
	if len(path) == 0 {
		return v
	}
	i, _ := strconv.Atoi(path[0])
	arr, _ := node.([]any)
	for len(arr) <= i {
		arr = append(arr, nil)
	}
	arr[i] = setPath(arr[i], path[1:], v)
	return arr
}
