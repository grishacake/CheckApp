package rustore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/model"
	"github.com/PuerkitoBio/goquery"
)

var ErrInvalidPage = errors.New("invalid rustore page")

const maxPageSize = 8 << 20

type schemaTypes []string

func (t *schemaTypes) UnmarshalJSON(data []byte) error {
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		*t = []string{single}
		return nil
	}
	return json.Unmarshal(data, (*[]string)(t))
}

type schemaNode struct {
	Type          schemaTypes     `json:"@type"`
	ID            string          `json:"@id"`
	Name          string          `json:"name"`
	URL           string          `json:"url"`
	Image         string          `json:"image"`
	ContentRating string          `json:"contentRating"`
	Rating        *schemaRating   `json:"aggregateRating"`
	Offers        json.RawMessage `json:"offers"`
	Author        struct {
		ID   string `json:"@id"`
		Name string `json:"name"`
	} `json:"author"`
	Items []struct {
		Position int             `json:"position"`
		Name     string          `json:"name"`
		Item     json.RawMessage `json:"item"`
	} `json:"itemListElement"`
}

type schemaRating struct {
	Count *int64   `json:"ratingCount"`
	Value *float64 `json:"ratingValue"`
}

// Parse разбирает распакованный HTML карточки без сетевых запросов.
func Parse(r io.Reader, packageName string) (model.App, error) {
	if !app.ValidPackageName(packageName) {
		return model.App{}, fmt.Errorf("%w: invalid package name", ErrInvalidPage)
	}
	if r == nil {
		return model.App{}, fmt.Errorf("%w: missing HTML reader", ErrInvalidPage)
	}
	body, err := io.ReadAll(io.LimitReader(r, maxPageSize+1))
	if err != nil {
		return model.App{}, fmt.Errorf("%w: read HTML: %w", ErrInvalidPage, err)
	}
	if len(body) > maxPageSize {
		return model.App{}, fmt.Errorf("%w: HTML exceeds %d bytes", ErrInvalidPage, maxPageSize)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return model.App{}, fmt.Errorf("%w: read HTML: %w", ErrInvalidPage, err)
	}
	storeURL := "https://www.rustore.ru/catalog/app/" + packageName
	canonical := doc.Find(`link[rel="canonical"]`)
	actualURL, _ := canonical.Attr("href")
	if canonical.Length() != 1 || actualURL != storeURL {
		return model.App{}, fmt.Errorf("%w: unexpected canonical URL", ErrInvalidPage)
	}
	nodes, err := readSchema(doc)
	if err != nil {
		return model.App{}, err
	}
	heading := normalizeText(doc.Find("h1").First().Text())
	var details *schemaNode
	for i := range nodes {
		node := &nodes[i]
		if !slices.Contains(node.Type, "SoftwareApplication") || normalizeText(node.Name) != heading {
			continue
		}
		if node.URL != "" && node.URL != storeURL {
			continue
		}
		if details != nil {
			return model.App{}, fmt.Errorf("%w: ambiguous application data", ErrInvalidPage)
		}
		details = node
	}
	if details == nil || heading == "" {
		return model.App{}, fmt.Errorf("%w: application data missing", ErrInvalidPage)
	}
	var offer struct {
		Availability string `json:"availability"`
	}
	if json.Unmarshal(details.Offers, &offer) == nil &&
		offer.Availability == "https://schema.org/OutOfStock" &&
		doc.Find(`[data-testid="HiddenAppRequestBtn"]`).Length() > 0 {
		// RuStore also serves unavailable catalog stubs with HTTP 200.
		return model.App{}, fmt.Errorf("%w: RuStore catalog stub for %s", app.ErrNotFound, packageName)
	}
	card := model.App{
		PackageName: packageName,
		StoreURL:    storeURL,
		Name:        normalizeText(details.Name),
		Developer:   strings.TrimSpace(details.Author.Name),
		IconURL:     strings.TrimSpace(details.Image),
		AgeRating:   strings.TrimSpace(details.ContentRating),
	}
	for _, node := range nodes {
		if slices.Contains(node.Type, "Organization") && details.Author.ID != "" && node.ID == details.Author.ID {
			if card.Developer != "" && card.Developer != strings.TrimSpace(node.Name) {
				return model.App{}, fmt.Errorf("%w: ambiguous developer", ErrInvalidPage)
			}
			card.Developer = strings.TrimSpace(node.Name)
		}
	}
	card.Category, err = readCategory(nodes)
	if err != nil {
		return model.App{}, err
	}
	card.Rating, err = readRating(doc, heading, details.Rating)
	if err != nil {
		return model.App{}, err
	}
	if card.Developer == "" || card.Category == "" || card.AgeRating == "" || !validImageURL(card.IconURL) {
		return model.App{}, fmt.Errorf("%w: incomplete application data", ErrInvalidPage)
	}
	return card, nil
}

func readSchema(doc *goquery.Document) ([]schemaNode, error) {
	var nodes []schemaNode
	var parseErr error
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, script *goquery.Selection) {
		if parseErr != nil {
			return
		}
		parsed, err := decodeSchema([]byte(script.Text()), 0)
		if err != nil {
			parseErr = fmt.Errorf("%w: decode JSON-LD: %w", ErrInvalidPage, err)
			return
		}
		nodes = append(nodes, parsed...)
	})
	return nodes, parseErr
}

func decodeSchema(data []byte, depth int) ([]schemaNode, error) {
	if depth > 32 {
		return nil, errors.New("JSON-LD nesting too deep")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var children []json.RawMessage
	var nodes []schemaNode
	if data[0] == '[' {
		if err := json.Unmarshal(data, &children); err != nil {
			return nil, err
		}
	} else {
		var header struct {
			Type  schemaTypes     `json:"@type"`
			Graph json.RawMessage `json:"@graph"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return nil, err
		}
		node := schemaNode{Type: header.Type}
		fields := make(map[string]any)
		if slices.Contains(header.Type, "SoftwareApplication") {
			fields = map[string]any{
				"name": &node.Name, "url": &node.URL, "image": &node.Image,
				"contentRating": &node.ContentRating, "author": &node.Author,
				"aggregateRating": &node.Rating,
				"offers":          &node.Offers,
			}
		}
		if slices.Contains(header.Type, "Organization") {
			fields["@id"], fields["name"] = &node.ID, &node.Name
		}
		if slices.Contains(header.Type, "BreadcrumbList") {
			fields["itemListElement"] = &node.Items
		}
		if len(fields) > 0 {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				return nil, err
			}
			for key, target := range fields {
				if value, ok := raw[key]; ok {
					if err := json.Unmarshal(value, target); err != nil {
						return nil, fmt.Errorf("decode %s: %w", key, err)
					}
				}
			}
			nodes = append(nodes, node)
		}
		if len(header.Graph) > 0 {
			children = append(children, header.Graph)
		}
	}
	for _, child := range children {
		parsed, err := decodeSchema(child, depth+1)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, parsed...)
	}
	return nodes, nil
}

func readCategory(nodes []schemaNode) (string, error) {
	var category string
	for _, node := range nodes {
		if !slices.Contains(node.Type, "BreadcrumbList") {
			continue
		}
		var current string
		var position int
		for _, item := range node.Items {
			var link string
			if err := json.Unmarshal(item.Item, &link); err != nil {
				var ref struct {
					ID string `json:"@id"`
				}
				if json.Unmarshal(item.Item, &ref) != nil {
					continue
				}
				link = ref.ID
			}
			u, err := url.Parse(link)
			if err != nil || u.Scheme != "https" || u.Host != "www.rustore.ru" || !strings.HasPrefix(u.Path, "/catalog/") || strings.HasPrefix(u.Path, "/catalog/app/") || strings.HasPrefix(u.Path, "/catalog/developer/") {
				continue
			}
			name := strings.TrimSpace(item.Name)
			if name == "" || item.Position <= 2 || item.Position < position {
				continue
			}
			if item.Position == position && current != name {
				return "", fmt.Errorf("%w: ambiguous category", ErrInvalidPage)
			}
			current, position = name, item.Position
		}
		if current != "" {
			if category != "" && category != current {
				return "", fmt.Errorf("%w: ambiguous breadcrumbs", ErrInvalidPage)
			}
			category = current
		}
	}
	return category, nil
}

func readRating(doc *goquery.Document, name string, metadata *schemaRating) (float64, error) {
	noRatings := false
	hasRatings := false
	if metadata != nil {
		if metadata.Count != nil {
			if *metadata.Count < 0 {
				return 0, fmt.Errorf("%w: invalid rating count", ErrInvalidPage)
			}
			noRatings = *metadata.Count == 0
			hasRatings = *metadata.Count > 0
		}
		if metadata.Value != nil {
			value := *metadata.Value
			if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 5 || (noRatings && value > 0) || (hasRatings && value == 0) {
				return 0, fmt.Errorf("%w: inconsistent rating data", ErrInvalidPage)
			}
		}
	}
	const appScope = `[itemscope][itemtype~="https://schema.org/SoftwareApplication"]`
	scope := doc.Find(appScope).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return normalizeText(s.Find("h1").First().Text()) == name
	})
	if scope.Length() != 1 {
		return 0, fmt.Errorf("%w: application scope missing or ambiguous", ErrInvalidPage)
	}
	groups := scope.Find(`[itemprop~="aggregateRating"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.ParentsFiltered(`[itemscope]`).First().Get(0) == scope.Get(0)
	})
	if groups.Length() > 1 {
		return 0, fmt.Errorf("%w: ambiguous rating blocks", ErrInvalidPage)
	}
	orphaned := scope.Find(`[itemprop~="ratingValue"], [itemprop~="ratingCount"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.Closest(`[itemscope]`).Get(0) == scope.Get(0)
	})
	if orphaned.Length() > 0 {
		return 0, fmt.Errorf("%w: rating data outside aggregateRating", ErrInvalidPage)
	}
	value := scope.Find(`[itemprop~="aggregateRating"] [itemprop~="ratingValue"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		aggregate := s.Closest(`[itemprop~="aggregateRating"][itemscope]`)
		owner := aggregate.ParentsFiltered(`[itemscope]`).First()
		return aggregate.Length() == 1 && owner.Get(0) == scope.Get(0) && s.Closest(`[itemscope]`).Get(0) == aggregate.Get(0)
	})
	if value.Length() == 0 && (noRatings || (metadata == nil && groups.Length() == 0)) {
		// Ноль сохраняет общий числовой контракт карточки для приложения без оценок.
		return 0, nil
	}
	if value.Length() != 1 {
		return 0, fmt.Errorf("%w: application rating missing or ambiguous", ErrInvalidPage)
	}
	raw := strings.TrimSpace(value.Text())
	if raw == "" {
		raw, _ = value.Attr("content")
	}
	raw = strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	rating, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(rating) || math.IsInf(rating, 0) || rating < 0 || rating > 5 || (noRatings && rating > 0) || (hasRatings && rating == 0) {
		return 0, fmt.Errorf("%w: invalid rating", ErrInvalidPage)
	}
	return math.Round(rating*10) / 10, nil
}

func normalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func validImageURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
