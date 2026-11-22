package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"app-grabber-backend/internal/auth"
	"app-grabber-backend/internal/comparison"
	"app-grabber-backend/internal/httpapi"
	"app-grabber-backend/internal/model"
)

type comparisonItemsMemory struct {
	users map[string][]comparison.SavedItem
	err   error
}

func (m *comparisonItemsMemory) AddItems(_ context.Context, user string, items []comparison.Item) error {
	if m.err != nil {
		return m.err
	}
	for _, item := range items {
		found := false
		for _, stored := range m.users[user] {
			if item == stored.Item {
				found = true
			}
		}
		if !found {
			m.users[user] = append(m.users[user], comparison.SavedItem{Item: item, AddedAt: time.Now()})
		}
	}
	return nil
}
func (m *comparisonItemsMemory) ListItems(_ context.Context, user string) ([]comparison.SavedItem, error) {
	return m.users[user], m.err
}
func (m *comparisonItemsMemory) RemoveItem(_ context.Context, user string, item comparison.Item) error {
	if m.err != nil {
		return m.err
	}
	for i, stored := range m.users[user] {
		if stored.Item == item {
			m.users[user] = append(m.users[user][:i], m.users[user][i+1:]...)
			break
		}
	}
	return nil
}
func (m *comparisonItemsMemory) ClearItems(_ context.Context, user string) error {
	if m.err != nil {
		return m.err
	}
	delete(m.users, user)
	return nil
}

func itemsRouter(t *testing.T, repo *comparisonItemsMemory) (*http.ServeMux, *auth.TokenManager) {
	t.Helper()
	tokens := newHandlerTokens(t)
	provider := providerFunc(func(_ context.Context, id string) (model.App, error) {
		return model.App{PackageName: id, Name: "card"}, nil
	})
	h := httpapi.ComparisonHandler{
		Items: repo, History: httpapi.NewHistoryHandler(nil, favoriteUsersStub{}, tokens, time.Second),
		Service: &comparison.Service{GooglePlay: provider, RuStore: provider, Cache: comparisonCache{}, TTL: time.Hour, StoreTimeout: time.Second},
	}
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux, tokens
}

func itemRequest(t *testing.T, mux http.Handler, tokens *auth.TokenManager, user, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		token, err := tokens.Issue(user)
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: httpapi.AuthCookieName, Value: token})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestUserComparisonLifecycle(t *testing.T) {
	repo := &comparisonItemsMemory{users: make(map[string][]comparison.SavedItem)}
	mux, tokens := itemsRouter(t, repo)
	request := func(user, method, path, body string, code int) *httptest.ResponseRecorder {
		w := itemRequest(t, mux, tokens, user, method, path, body)
		if w.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		return w
	}
	body := `{"items":[{"url":"https://www.rustore.ru/catalog/app/ru.ivi.client"},{"store":"google_play","package_name":"ru.ivi.client"},{"url":"https://apps.rustore.ru/app/ru.ivi.client"},{"store":"rustore","package_name":"com.other.app"}]}`
	request("alice", "POST", "/api/compare", body, 204)
	request("alice", "POST", "/api/compare", body, 204)
	request("bob", "POST", "/api/compare", body, 204)
	w := request("alice", "GET", "/api/compare", "", 200)
	var result struct {
		Items []comparison.ItemResult `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Items) != 3 {
		t.Fatal(w.Body.String())
	}
	for _, item := range result.Items {
		if item.Status != "ok" || item.App == nil || item.App.PackageName != item.PackageName || item.AddedAt.IsZero() {
			t.Fatal(item)
		}
	}
	request("alice", "DELETE", "/api/compare/rustore/ru.ivi.client", "", 204)
	request("alice", "DELETE", "/api/compare/rustore/ru.ivi.client", "", 204)
	if len(repo.users["alice"]) != 2 || len(repo.users["bob"]) != 3 {
		t.Fatal(repo.users)
	}
	request("alice", "DELETE", "/api/compare/all", "", 204)
	if w := request("alice", "GET", "/api/compare", "", 200); strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatal(w.Body.String())
	}
	if len(repo.users["bob"]) != 3 {
		t.Fatal("cleared another user's comparison")
	}
	request("bob", "DELETE", "/api/compare", "", 204)
	if len(repo.users["bob"]) != 0 {
		t.Fatal("canonical clear failed")
	}
}

func TestComparisonItemsAuthenticationAndValidation(t *testing.T) {
	repo := &comparisonItemsMemory{users: make(map[string][]comparison.SavedItem)}
	mux, tokens := itemsRouter(t, repo)
	for _, r := range []struct{ method, path string }{{"GET", "/api/compare"}, {"POST", "/api/compare"}, {"DELETE", "/api/compare"}, {"DELETE", "/api/compare/all"}, {"DELETE", "/api/compare/rustore/com.example"}} {
		if w := itemRequest(t, mux, tokens, "", r.method, r.path, `{}`); w.Code != 401 {
			t.Fatalf("%+v: %d", r, w.Code)
		}
	}
	for _, body := range []string{`{}`, `{"items":[]}`, `{"items":null}`, `{"items":[null]}`, `{"items":[{"package_name":"com.example"}]}`, `{"items":[{"store":"unknown","package_name":"com.example"}]}`, `{"items":[{"url":"https://example.com/app"}]}`, `{"items":[{"url":"https://www.rustore.ru/catalog/app/com.example","store":"google_play"}]}`, `{"items":[{"store":"rustore","package_name":"com.valid"},{"store":"rustore","package_name":"bad"}]}`, `{"items":[{"url":"https://www.rustore.ru/catalog/app/com.example","extra":true}]}`, `{"items":[]} {}`} {
		if w := itemRequest(t, mux, tokens, "alice", "POST", "/api/compare", body); w.Code != 400 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body)
		}
		if len(repo.users["alice"]) != 0 {
			t.Fatal("invalid batch partially saved")
		}
	}
	if w := itemRequest(t, mux, tokens, "alice", "POST", "/api/compare", `{"items":[],"pad":"`+strings.Repeat("x", 1<<20)+`"}`); w.Code != 413 {
		t.Fatal(w.Code)
	}
	if w := itemRequest(t, mux, tokens, "alice", "DELETE", "/api/compare?store=rustore&package_name=com.example", ""); w.Code != 400 {
		t.Fatal("ambiguous delete cleared comparison")
	}
	repo.err = errors.New("private database error")
	for _, method := range []string{"GET", "POST", "DELETE"} {
		w := itemRequest(t, mux, tokens, "alice", method, "/api/compare", `{"items":[{"store":"rustore","package_name":"com.example"}]}`)
		if w.Code != 500 || strings.Contains(w.Body.String(), "private") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
