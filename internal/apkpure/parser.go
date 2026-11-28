package apkpure

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/applink"
	"app-grabber-backend/internal/model"

	"github.com/PuerkitoBio/goquery"
)

var ErrInvalidPage = errors.New("invalid APKPure card")

const maxPageSize = 8 << 20

type mobileApplication struct {
	Type      string `json:"@type"`
	Name      string `json:"name"`
	Category  string `json:"applicationSubCategory"`
	AgeRating string `json:"contentRating"`
	Publisher struct {
		Name string `json:"name"`
	} `json:"publisher"`
	Rating *struct {
		Value json.Number `json:"ratingValue"`
		Best  json.Number `json:"bestRating"`
	} `json:"aggregateRating"`
}

// Parse reads the catalog card and checks that its canonical URL matches the requested package.
func Parse(r io.Reader, packageName string) (model.App, error) {
	if !app.ValidPackageName(packageName) {
		return model.App{}, fmt.Errorf("%w: invalid package name", ErrInvalidPage)
	}
	if r == nil {
		return model.App{}, fmt.Errorf("%w: missing HTML reader", ErrInvalidPage)
	}
	body, err := io.ReadAll(io.LimitReader(r, maxPageSize+1))
	if err != nil {
		return model.App{}, fmt.Errorf("read APKPure page: %w", err)
	}
	if len(body) > maxPageSize {
		return model.App{}, fmt.Errorf("%w: page exceeds size limit", ErrInvalidPage)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return model.App{}, fmt.Errorf("%w: read HTML: %w", ErrInvalidPage, err)
	}
	canonical := doc.Find("link[rel=canonical]")
	storeURL, _ := canonical.Attr("href")
	link, ok := applink.Parse(storeURL)
	if canonical.Length() != 1 || !ok || link.Store != "apkpure" || link.PackageName != packageName || !httpsURL(storeURL) {
		return model.App{}, fmt.Errorf("%w: unexpected canonical URL", ErrInvalidPage)
	}
	heading := doc.Find(".app-info h1").First()
	name := strings.TrimSpace(heading.Text())
	if name == "" {
		return model.App{}, fmt.Errorf("%w: application name missing", ErrInvalidPage)
	}
	details, err := findApplication(doc, name)
	if err != nil {
		return model.App{}, err
	}

	icon, _ := heading.Closest(".app-info").Find(".app-icon img").First().Attr("src")
	card := model.App{
		PackageName: packageName,
		StoreURL:    storeURL,
		Name:        name,
		Developer:   strings.TrimSpace(details.Publisher.Name),
		IconURL:     strings.TrimSpace(icon),
		Category:    strings.TrimSpace(details.Category),
		AgeRating:   strings.TrimSpace(details.AgeRating),
	}
	if card.AgeRating == "" {
		card.AgeRating = strings.TrimSpace(doc.Find(".content-ratting .desc").First().Text())
	}
	if card.Developer == "" || card.Category == "" || !httpsURL(card.IconURL) {
		return model.App{}, fmt.Errorf("%w: application metadata missing", ErrInvalidPage)
	}
	if details.Rating != nil {
		value, valueErr := details.Rating.Value.Float64()
		best, bestErr := details.Rating.Best.Float64()
		if valueErr != nil || bestErr != nil || best <= 0 || value < 0 || value > best {
			return model.App{}, fmt.Errorf("%w: invalid rating", ErrInvalidPage)
		}
		// Normalize the source rating to the API's 0–5 scale.
		card.Rating = math.Round(value/best*50) / 10
	}
	return card, nil
}

func findApplication(doc *goquery.Document, name string) (mobileApplication, error) {
	var application mobileApplication
	for _, script := range doc.Find(`script[type="application/ld+json"]`).EachIter() {
		raw := strings.TrimSpace(script.Text())
		nodes := []json.RawMessage{json.RawMessage(raw)}
		if strings.HasPrefix(raw, "[") && json.Unmarshal([]byte(raw), &nodes) != nil {
			continue
		}
		for _, node := range nodes {
			var entry mobileApplication
			if json.Unmarshal(node, &entry) != nil || entry.Type != "MobileApplication" || strings.TrimSpace(entry.Name) != name {
				continue
			}
			if application.Name != "" {
				return mobileApplication{}, fmt.Errorf("%w: ambiguous metadata", ErrInvalidPage)
			}
			application = entry
		}
	}
	if application.Name == "" {
		return mobileApplication{}, fmt.Errorf("%w: application metadata missing", ErrInvalidPage)
	}
	return application, nil
}

func httpsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
