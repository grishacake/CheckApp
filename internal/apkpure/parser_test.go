package apkpure

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func savedPage(t *testing.T, id string) []byte {
	t.Helper()
	f, err := os.Open("testdata/" + id + ".html.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	page, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestParseSavedCards(t *testing.T) {
	for _, tc := range []struct {
		id, name, developer, age string
		rating                   float64
	}{
		{"org.telegram.messenger", "Telegram", "Telegram FZ-LLC", "Mature 17+", 4.2},
		{"com.whatsapp", "WhatsApp", "WhatsApp LLC", "Everyone", 4.0},
	} {
		t.Run(tc.id, func(t *testing.T) {
			card, err := Parse(bytes.NewReader(savedPage(t, tc.id)), tc.id)
			if err != nil {
				t.Fatal(err)
			}
			if card.PackageName != tc.id || card.Name != tc.name || card.Developer != tc.developer ||
				card.AgeRating != tc.age || card.Category != "Communication" || card.Rating != tc.rating ||
				!strings.HasSuffix(card.StoreURL, "/"+tc.id) || !strings.Contains(card.IconURL, "/icon.png") {
				t.Fatalf("unexpected card: %+v", card)
			}
		})
	}
}

func TestParseRejectsInvalidPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		page io.Reader
	}{
		{"wrong package", bytes.NewReader(savedPage(t, "com.whatsapp"))},
		{"error page", strings.NewReader("<html><title>Temporarily unavailable</title></html>")},
		{"oversized page", strings.NewReader(strings.Repeat(" ", maxPageSize+1))},
		{"missing reader", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.page, "org.telegram.messenger"); !errors.Is(err, ErrInvalidPage) {
				t.Fatalf("expected invalid page, got %v", err)
			}
		})
	}
}

func TestParseUnratedCard(t *testing.T) {
	const page = `
<link rel="canonical" href="https://apkpure.com/example/com.example.MixedCase">
<script type="application/ld+json">
{"@type":"MobileApplication","name":"Example","publisher":{"name":"Example Dev"},"applicationSubCategory":"Tools"}
</script>
<div class="app-info"><h1>Example</h1><div class="app-icon"><img src="https://image.winudf.com/icon.png"></div></div>`
	card, err := Parse(strings.NewReader(page), "com.example.MixedCase")
	if err != nil {
		t.Fatal(err)
	}
	if card.PackageName != "com.example.MixedCase" || card.Rating != 0 || card.AgeRating != "" {
		t.Fatalf("unexpected card: %+v", card)
	}
}
