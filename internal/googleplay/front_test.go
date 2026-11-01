package googleplay

import (
	"context"
	"net/http"
	"net/url"
	"testing"
)

// hostTransport запоминает, куда ушёл запрос, и отвечает страницей из testdata/.
type hostTransport struct {
	urlHost, host string
}

func (h *hostTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	h.urlHost, h.host = r.URL.Host, r.Host
	return fixtureTransport{}.RoundTrip(r)
}

func TestGet_FrontHost(t *testing.T) {
	rt := &hostTransport{}
	if _, err := newTestClientWith(rt).GetApp(context.Background(), "com.uchi.app"); err != nil {
		t.Fatal(err)
	}
	if rt.urlHost != DefaultFrontHost || rt.host != playHost {
		t.Errorf("connect to %q with Host %q, want %q with Host %q", rt.urlHost, rt.host, DefaultFrontHost, playHost)
	}
}

func TestGet_FrontHostOff(t *testing.T) {
	rt := &hostTransport{}
	c := newTestClientWith(rt)
	c.FrontHost = ""
	if _, err := c.GetApp(context.Background(), "com.uchi.app"); err != nil {
		t.Fatal(err)
	}
	if rt.urlHost != playHost || rt.host != playHost {
		t.Errorf("connect to %q with Host %q, want %q", rt.urlHost, rt.host, playHost)
	}
}

func TestUseProxy(t *testing.T) {
	c := New()
	if err := c.UseProxy(""); err != nil || c.HTTP.Transport != nil {
		t.Fatalf("empty url: err %v, transport %v; want no proxy", err, c.HTTP.Transport)
	}

	if err := c.UseProxy("http://login:password@1.2.3.4:10000"); err != nil {
		t.Fatal(err)
	}
	tr, ok := c.HTTP.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T, want *http.Transport", c.HTTP.Transport)
	}
	req, _ := http.NewRequest(http.MethodGet, detailURL, nil)
	got, err := tr.Proxy(req)
	if err != nil || got == nil || got.Host != "1.2.3.4:10000" || got.User.Username() != "login" {
		t.Errorf("proxy for request = %v, %v; want http://login:***@1.2.3.4:10000", got, err)
	}

	for _, bad := range []string{"1.2.3.4:10000", "ftp://1.2.3.4:21", "http://"} {
		if err := New().UseProxy(bad); err == nil {
			t.Errorf("UseProxy(%q): want error", bad)
		}
	}
}

// Ссылка на карточку для пользователя остаётся на play.google.com, FrontHost её не меняет.
func TestStoreURL_KeepsPlayHost(t *testing.T) {
	u, err := url.Parse(storeURL("com.uchi.app", "ru", "ru"))
	if err != nil || u.Host != playHost {
		t.Errorf("store url host = %q, want %q", u.Host, playHost)
	}
}
