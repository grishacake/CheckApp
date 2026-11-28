package apkpure

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"app-grabber-backend/internal/app"
	"app-grabber-backend/internal/applink"
	"app-grabber-backend/internal/model"
)

const cardHost = "apkpure.com"

type Client struct {
	HTTP *http.Client
}

var _ app.Provider = Client{}

func (c Client) GetApp(ctx context.Context, packageName string) (model.App, error) {
	if !app.ValidPackageName(packageName) {
		return model.App{}, fmt.Errorf("invalid package name")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+cardHost+"/p/"+packageName, nil)
	if err != nil {
		return model.App{}, err
	}
	client := http.Client{Timeout: 10 * time.Second}
	if c.HTTP != nil {
		client = *c.HTTP
	}
	checkRedirect := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		link, ok := applink.Parse(next.URL.String())
		if len(via) >= 5 || next.URL.Scheme != "https" || !ok || link.Store != "apkpure" || link.PackageName != packageName {
			return http.ErrUseLastResponse
		}
		if checkRedirect != nil {
			return checkRedirect(next, via)
		}
		return nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return model.App{}, fmt.Errorf("load APKPure page: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return Parse(resp.Body, packageName)
	case http.StatusNotFound, http.StatusGone:
		return model.App{}, app.ErrNotFound
	default:
		return model.App{}, fmt.Errorf("APKPure returned HTTP %d", resp.StatusCode)
	}
}
