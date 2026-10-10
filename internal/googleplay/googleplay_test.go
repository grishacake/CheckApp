package googleplay

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"app-grabber-backend/internal/app"
)

// Сохранённые страницы Google Play лежат в testdata/<id>.<gl>.html.gz (сжаты: без сжатия ~1,3 МБ каждая).
// Обновить их с живого Google Play:
//
//	go test ./internal/googleplay -update
var update = flag.Bool("update", false, "скачать страницы из Google Play в testdata/")

// fixtureIDs — приложения, страницы которых храним в testdata/.
var fixtureIDs = []string{
	"com.uchi.app",
	"com.vkontakte.android", // нет в RU, есть в US
	"org.telegram.messenger",
	"com.whatsapp",
	"ru.ozon.app.android",
	"com.wildberries.ru",
	"com.google.android.youtube",
	"ru.yandex.searchplugin",
	"com.spotify.music",
	"com.duolingo",
	"com.supercell.brawlstars",
	"ru.sberbankmobile", // удалён из Google Play — страниц нет
}

func TestMain(m *testing.M) {
	flag.Parse()
	if *update {
		if err := downloadFixtures(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	// Дальше все запросы библиотеки идут в testdata/, а не в сеть.
	http.DefaultClient.Transport = fixtureTransport{}
	os.Exit(m.Run())
}

func downloadFixtures() error {
	for _, id := range fixtureIDs {
		for _, gl := range []string{"ru", "us"} {
			u := detailURL + "?id=" + id + "&gl=" + gl + "&hl=ru"
			resp, err := http.Get(u)
			if err != nil {
				return err
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return err
			}
			path := fixturePath(id, gl)
			if resp.StatusCode == http.StatusNotFound {
				os.Remove(path)
				fmt.Printf("%s gl=%s: 404\n", id, gl)
			} else if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("%s gl=%s: %s", id, gl, resp.Status)
			} else if err := writeGzip(path, body); err != nil {
				return err
			} else {
				fmt.Printf("%s gl=%s: saved\n", id, gl)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func fixturePath(id, gl string) string {
	return filepath.Join("testdata", id+"."+gl+".html.gz")
}

func writeGzip(path string, data []byte) error {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(data)
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func readGzip(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}

// fixtureTransport отвечает страницей из testdata/ или 404, если её нет.
type fixtureTransport struct{}

func (fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	q := r.URL.Query()
	body, err := readGzip(fixturePath(q.Get("id"), q.Get("gl")))
	status := http.StatusOK
	if errors.Is(err, os.ErrNotExist) {
		status, body = http.StatusNotFound, nil
	} else if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func newTestClient() *Client {
	c := New()
	c.MinInterval = 0
	return c
}

func TestGetApp_Uchi(t *testing.T) {
	app, err := newTestClient().GetApp(context.Background(), "com.uchi.app")
	if err != nil {
		t.Fatal(err)
	}
	checks := []struct{ field, got, want string }{
		{"package_name", app.PackageName, "com.uchi.app"},
		{"store_url", app.StoreURL, "https://play.google.com/store/apps/details?id=com.uchi.app&hl=ru"},
		{"name", app.Name, "Учи.ру"},
		{"developer", app.Developer, `ООО "Учи.ру"`},
		{"category", app.Category, "Образование"},
		{"age_rating", app.AgeRating, "3+"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
	if app.Rating != 4.2 {
		t.Errorf("rating = %v, want 4.2", app.Rating)
	}
}

// ВКонтакте нет в российском Google Play: должны найти его в US и дать ссылку с gl=us.
func TestGetApp_FallbackToUS(t *testing.T) {
	app, err := newTestClient().GetApp(context.Background(), "com.vkontakte.android")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://play.google.com/store/apps/details?id=com.vkontakte.android&hl=ru&gl=us"; app.StoreURL != want {
		t.Errorf("store_url = %q, want %q", app.StoreURL, want)
	}
	if app.AgeRating != "13+" {
		t.Errorf("age_rating = %q, want 13+", app.AgeRating)
	}
}

func TestGetApp_NotFound(t *testing.T) {
	for _, id := range []string{"ru.sberbankmobile", "com.example.none"} {
		_, err := newTestClient().GetApp(context.Background(), id)
		if !errors.Is(err, app.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", id, err)
		}
	}
}

// Все сохранённые страницы должны разбираться: если Google поменяет разметку,
// после -update этот тест покажет, какие поля перестали заполняться.
func TestGetApp_AllFixturesHaveFields(t *testing.T) {
	c := newTestClient()
	for _, id := range fixtureIDs {
		_, errRU := os.Stat(fixturePath(id, "ru"))
		_, errUS := os.Stat(fixturePath(id, "us"))
		if errRU != nil && errUS != nil {
			continue // приложения нет в Google Play
		}
		t.Run(id, func(t *testing.T) {
			app, err := c.GetApp(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			for field, v := range map[string]string{
				"package_name": app.PackageName, "store_url": app.StoreURL, "name": app.Name, "developer": app.Developer,
				"icon_url": app.IconURL, "category": app.Category, "age_rating": app.AgeRating,
			} {
				if v == "" {
					t.Errorf("%s is empty", field)
				}
			}
			if app.Rating < 0 || app.Rating > 5 {
				t.Errorf("rating = %v, want 0..5", app.Rating)
			}
		})
	}
}

// Google не отвечает дольше тайм-аута → ErrUnavailable, а не зависание.
func TestGetApp_Timeout(t *testing.T) {
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = slowTransport{}
	defer func() { http.DefaultClient.Transport = old }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := newTestClient().GetApp(ctx, "com.uchi.app")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

type slowTransport struct{}

func (slowTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	time.Sleep(time.Second)
	return nil, errors.New("too slow")
}

// Страница открылась, но данных нет (капча, новая разметка) → ErrUnavailable.
func TestGetApp_EmptyPage(t *testing.T) {
	old := http.DefaultClient.Transport
	http.DefaultClient.Transport = emptyTransport{}
	defer func() { http.DefaultClient.Transport = old }()

	_, err := newTestClient().GetApp(context.Background(), "com.uchi.app")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

type emptyTransport struct{}

func (emptyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader([]byte("<html>captcha</html>"))),
		Header:     make(http.Header),
		Request:    r,
	}, nil
}

func TestNormalizeAge(t *testing.T) {
	for in, want := range map[string]string{
		"3+":         "3+",
		"18+":        "18+",
		"T (13+)":    "13+",
		"M (17+)":    "17+",
		"Для всех":   "Для всех",
		" Everyone ": "Everyone",
		"":           "",
	} {
		if got := normalizeAge(in); got != want {
			t.Errorf("normalizeAge(%q) = %q, want %q", in, got, want)
		}
	}
}
