package rustore_test

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"app-grabber-backend/internal/model"
	"app-grabber-backend/internal/rustore"
)

func savedPage(t *testing.T, packageName string) []byte {
	t.Helper()
	file, err := os.Open(filepath.Join("testdata", packageName+".html.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	page, err := io.ReadAll(zipped)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestParseSavedPages(t *testing.T) {
	for _, want := range []model.App{
		{
			PackageName: "ru.ozon.app.android",
			StoreURL:    "https://www.rustore.ru/catalog/app/ru.ozon.app.android",
			Name:        "OZON: товары, одежда, билеты",
			Developer:   `ООО "Интернет Решения"`,
			IconURL:     "https://static.rustore.ru/2026/6/30/b6/apk/514239/content/ICON/8a44b15d-a16e-41a4-b926-9088693015ca.png",
			Category:    "Покупки",
			Rating:      4.4,
			AgeRating:   "0+",
		},
		{
			PackageName: "org.telegram.messenger.web",
			StoreURL:    "https://www.rustore.ru/catalog/app/org.telegram.messenger.web",
			Name:        "Telegram",
			Developer:   "Telegram FZ-LLC",
			IconURL:     "https://static.rustore.ru/apk/2063507966/content/ICON/7faf2b87-4975-43f5-803a-d9ae7d180cc1.png",
			Category:    "Общение",
			Rating:      4,
			AgeRating:   "12+",
		},
		{
			PackageName: "com.unicostudio.braintest",
			StoreURL:    "https://www.rustore.ru/catalog/app/com.unicostudio.braintest",
			Name:        "Brain Test: Tricky Puzzles",
			Developer:   "Unico Studio",
			IconURL:     "https://static.rustore.ru/apk/2063550580/content/ICON/7d8dec8e-7c60-41fa-a5fd-ef2ff166fd61.",
			Category:    "Головоломки",
			Rating:      3.7,
			AgeRating:   "0+",
		},
		{
			PackageName: "mobi.avva.lider",
			StoreURL:    "https://www.rustore.ru/catalog/app/mobi.avva.lider",
			Name:        "Тест: Насколько ты лидер?",
			Developer:   "Vhenen",
			IconURL:     "https://static.rustore.ru/apk/2063531041/content/ICON/4f6a6d58-804e-46f2-aadf-13018ebef355.",
			Category:    "Викторины",
			Rating:      0,
			AgeRating:   "12+",
		},
		{
			PackageName: "com.gs.doubler.yourname",
			StoreURL:    "https://www.rustore.ru/catalog/app/com.gs.doubler.yourname",
			Name:        "Тест на твое имя",
			Developer:   "doubleR",
			IconURL:     "https://static.rustore.ru/apk/2063529769/content/ICON/85350d9d-812b-4791-9203-a31e4e84dd4d.",
			Category:    "Викторины",
			Rating:      0,
			AgeRating:   "0+",
		},
		{
			PackageName: "com.example.calculatorKSA1211",
			StoreURL:    "https://www.rustore.ru/catalog/app/com.example.calculatorKSA1211",
			Name:        "Калькулятор",
			Developer:   "A Ks",
			IconURL:     "https://static.rustore.ru/2026/6/1/c8/apk/2063641710/content/ICON/f97666a5-def9-4f5d-8239-f4624cdfc72e.png",
			Category:    "Полезные инструменты",
			Rating:      4.6,
			AgeRating:   "0+",
		},
	} {
		t.Run(want.PackageName, func(t *testing.T) {
			got, err := rustore.Parse(bytes.NewReader(savedPage(t, want.PackageName)), want.PackageName)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("card = %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseRejectsInvalidPages(t *testing.T) {
	const packageName = "ru.ozon.app.android"
	page := string(savedPage(t, packageName))
	replace := func(old, new string) string {
		t.Helper()
		if !strings.Contains(page, old) {
			t.Fatalf("saved page no longer contains %q", old)
		}
		return strings.Replace(page, old, new, 1)
	}
	for _, tc := range []struct {
		name, page, packageName string
	}{
		{"empty HTML", "", packageName},
		{"invalid package name", page, "../ozon"},
		{"different application", page, "com.example.other"},
		{"missing heading", replace("<h1", "<h2"), packageName},
		{"broken JSON-LD", replace(`"@context":"https://schema.org"`, `"@context":`), packageName},
		{"missing rating", replace(`itemProp="ratingValue"`, `data-ignored-rating="true"`), packageName},
		{"invalid rating", replace(`itemProp="ratingValue">4,4`, `itemProp="ratingValue">6,1`), packageName},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, err := rustore.Parse(strings.NewReader(tc.page), tc.packageName)
			if !errors.Is(err, rustore.ErrInvalidPage) || card != (model.App{}) {
				t.Fatalf("Parse returned card=%+v error=%v; want empty card and ErrInvalidPage", card, err)
			}
		})
	}
}

func TestParseReaderErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader io.Reader
	}{
		{"nil reader", nil},
		{"oversized page", strings.NewReader(strings.Repeat(" ", (8<<20)+1))},
		{"read failure", failingReader{err: io.ErrUnexpectedEOF}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card, err := rustore.Parse(tc.reader, "ru.ozon.app.android")
			if !errors.Is(err, rustore.ErrInvalidPage) || card != (model.App{}) {
				t.Fatalf("Parse returned card=%+v error=%v; want empty card and ErrInvalidPage", card, err)
			}
			if tc.name == "read failure" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("read error was lost: %v", err)
			}
		})
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
