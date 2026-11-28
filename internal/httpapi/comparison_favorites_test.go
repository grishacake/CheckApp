package httpapi_test

import (
	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/comparison"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type favoriteSetsMemory struct {
	users map[string][]comparison.Favorite
}

func (m *favoriteSetsMemory) SaveFavorite(_ context.Context, user string, items []comparison.Item) (comparison.Favorite, error) {
	canonical, err := comparison.CanonicalItems(items)
	if err != nil {
		return comparison.Favorite{}, err
	}
	for _, f := range m.users[user] {
		if reflect.DeepEqual(f.Items, canonical) {
			return f, nil
		}
	}
	f := comparison.Favorite{ID: "11111111-1111-4111-8111-111111111111", Items: canonical, SavedAt: time.Now().UTC()}
	m.users[user] = append(m.users[user], f)
	return f, nil
}
func (m *favoriteSetsMemory) ListFavorites(_ context.Context, user string, limit, offset int) ([]comparison.Favorite, error) {
	items := m.users[user]
	if items == nil {
		items = []comparison.Favorite{}
	}
	start := min(offset, len(items))
	return items[start:min(start+limit, len(items))], nil
}
func (m *favoriteSetsMemory) GetFavorite(_ context.Context, user, id string) (comparison.Favorite, error) {
	for _, f := range m.users[user] {
		if f.ID == id {
			return f, nil
		}
	}
	return comparison.Favorite{}, app.ErrNotFound
}
func (m *favoriteSetsMemory) DeleteFavorite(_ context.Context, user, id string) error {
	for i, f := range m.users[user] {
		if f.ID == id {
			m.users[user] = append(m.users[user][:i], m.users[user][i+1:]...)
			break
		}
	}
	return nil
}
func TestComparisonFavoritesLifecycle(t *testing.T) {
	repo := &favoriteSetsMemory{users: make(map[string][]comparison.Favorite)}
	tokens := newHandlerTokens(t)
	provider := providerFunc(func(_ context.Context, id string) (model.App, error) {
		return model.App{PackageName: id, Name: "card"}, nil
	})
	mux := http.NewServeMux()
	httpapi.ComparisonHandler{Favorites: repo, History: httpapi.NewHistoryHandler(nil, favoriteUsersStub{}, tokens, time.Second), Service: &comparison.Service{Providers: map[string]app.Provider{"google_play": provider, "rustore": provider, "third_store": provider}, Cache: comparisonCache{}, StoreTimeout: time.Second}}.RegisterRoutes(mux)
	base := "/api/compare/favorites"
	request := func(user, method, path, body string, code int) []byte {
		t.Helper()
		w := itemRequest(t, mux, tokens, user, method, path, body)
		if w.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	body := `{"items":[{"store":"rustore","package_name":"com.one"},{"store":"google_play","package_name":"com.two"},{"store":"third_store","package_name":"com.three"}]}`
	var saved comparison.Favorite
	if err := json.Unmarshal(request("alice", "PUT", base, body, 200), &saved); err != nil {
		t.Fatal(err)
	}
	request("alice", "PUT", base, body, 200)
	if len(repo.users["alice"]) != 1 || len(saved.Items) != 3 {
		t.Fatal(repo.users)
	}
	path := base + "/" + saved.ID
	var detail struct {
		Items []comparison.ItemResult `json:"items"`
	}
	if err := json.Unmarshal(request("alice", "GET", path, "", 200), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Items) != 3 {
		t.Fatal(detail)
	}
	for _, item := range detail.Items {
		if item.Status != "ok" || item.App == nil {
			t.Fatal(item)
		}
	}
	request("bob", "GET", path, "", 404)
	request("bob", "DELETE", path, "", 204)
	request("alice", "GET", path, "", 200)
	for _, r := range []struct{ method, path string }{{"PUT", base}, {"GET", base}, {"GET", path}, {"DELETE", path}} {
		request("", r.method, r.path, body, 401)
	}
	request("alice", "PUT", base, `{"items":[]}`, 400)
	request("alice", "PUT", base, `{"items":[{"store":"unknown","package_name":"com.example"}]}`, 400)
	request("alice", "GET", base+"/bad", "", 400)
	request("alice", "GET", base+"?limit=-1", "", 400)
	request("alice", "DELETE", path, "", 204)
	request("alice", "DELETE", path, "", 204)
	request("alice", "GET", path, "", 404)
	var listing struct {
		Items []comparison.Favorite `json:"items"`
	}
	if err := json.Unmarshal(request("alice", "GET", base, "", 200), &listing); err != nil || listing.Items == nil || len(listing.Items) != 0 {
		t.Fatal(listing, err)
	}
}
