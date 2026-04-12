package cli

import (
	"context"
	"time"

	"github.com/steipete/ordercli/internal/browserauth"
	"github.com/steipete/ordercli/internal/browserhistory"
	"github.com/steipete/ordercli/internal/browserpage"
	"github.com/steipete/ordercli/internal/chromecookies"
	"github.com/steipete/ordercli/internal/deliveroo"
	"github.com/steipete/ordercli/internal/foodora"
)

var chromeLoadCookieHeader = chromecookies.LoadCookieHeader

var browserOAuthTokenPassword = func(ctx context.Context, req foodora.OAuthPasswordRequest, opts browserauth.PasswordOptions) (foodora.AuthToken, *foodora.MfaChallenge, browserauth.Session, error) {
	return browserauth.OAuthTokenPassword(ctx, req, opts)
}

var deliverooResolveLatestStatusURL = browserhistory.ResolveLatestDeliverooStatusURL

var deliverooFetchPublicStatus = func(ctx context.Context, targetURL string, timeout time.Duration) (deliveroo.PublicStatus, error) {
	return deliveroo.FetchPublicStatus(ctx, targetURL, timeout)
}

type browserLoginResult struct {
	FinalURL  string
	UserAgent string
}

var uberEatsLoginBrowser = func(ctx context.Context, targetURL string, profileDir string, timeout time.Duration) (browserLoginResult, error) {
	res, err := browserpage.ReadText(ctx, targetURL, browserpage.Options{
		Timeout:              timeout,
		Headless:             false,
		ProfileDir:           profileDir,
		WaitForURLSubstrings: []string{"/orders"},
	})
	if err != nil {
		return browserLoginResult{}, err
	}
	return browserLoginResult{
		FinalURL:  res.FinalURL,
		UserAgent: res.UserAgent,
	}, nil
}

var uberEatsReadBrowserPage = browserpage.ReadText
