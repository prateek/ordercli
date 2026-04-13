package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steipete/ordercli/internal/config"
	"github.com/steipete/ordercli/internal/ubereats"
)

type fakeUberEatsClient struct {
	checkSession func(context.Context) (ubereats.Session, error)
	listOrders   func(context.Context, ubereats.OrderFilter, int) ([]ubereats.Order, error)
	getOrder     func(context.Context, string) (ubereats.Order, error)
}

func (f fakeUberEatsClient) SetCookieHeader(string) {}

func (f fakeUberEatsClient) CheckSession(ctx context.Context) (ubereats.Session, error) {
	return f.checkSession(ctx)
}

func (f fakeUberEatsClient) ListOrders(ctx context.Context, filter ubereats.OrderFilter, limit int) ([]ubereats.Order, error) {
	return f.listOrders(ctx, filter, limit)
}

func (f fakeUberEatsClient) GetOrder(ctx context.Context, ref string) (ubereats.Order, error) {
	return f.getOrder(ctx, ref)
}

func TestUberEatsCLI_Config_Login_Logout_Orders(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	profileDir := filepath.Join(t.TempDir(), "ubereats-profile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}

	origLogin := uberEatsLoginBrowser
	origFactory := uberEatsClientFactory
	t.Cleanup(func() {
		uberEatsLoginBrowser = origLogin
		uberEatsClientFactory = origFactory
	})

	uberEatsLoginBrowser = func(_ context.Context, targetURL string, gotProfileDir string, timeout time.Duration) (browserLoginResult, error) {
		if targetURL != "https://www.ubereats.com/orders/" {
			t.Fatalf("target_url=%q", targetURL)
		}
		if gotProfileDir != profileDir {
			t.Fatalf("profile_dir=%q", gotProfileDir)
		}
		if timeout != 10*time.Minute {
			t.Fatalf("timeout=%s", timeout)
		}
		return browserLoginResult{
			FinalURL:     targetURL,
			UserAgent:    "Mozilla/5.0 Test",
			CookieHeader: "sid=abc; auth=xyz",
		}, nil
	}

	uberEatsClientFactory = func(st *state, cmd uberEatsCommand) uberEatsClient {
		return fakeUberEatsClient{
			checkSession: func(context.Context) (ubereats.Session, error) {
				return ubereats.Session{LoggedIn: true}, nil
			},
			listOrders: func(_ context.Context, filter ubereats.OrderFilter, limit int) ([]ubereats.Order, error) {
				switch filter {
				case ubereats.OrderFilterActive:
					if limit != 20 {
						t.Fatalf("active limit=%d", limit)
					}
					return []ubereats.Order{{
						UUID:        "active-1",
						Merchant:    "Shake Shack",
						Status:      "Preparing your order",
						Total:       "$24.90",
						Active:      true,
						OccurredAt:  "2026-04-11T16:05:00Z",
						Items:       []string{"1x ShackBurger"},
						SessionInfo: ubereats.SessionInfo{LocationSource: "TARGET", LocationRef: "loc-1"},
					}}, nil
				case ubereats.OrderFilterPast:
					if limit != 20 {
						t.Fatalf("past limit=%d", limit)
					}
					return []ubereats.Order{{
						UUID:        "past-1",
						Merchant:    "Chipotle",
						Status:      "Completed",
						Total:       "$18.50",
						OccurredAt:  "2026-04-12T18:00:00Z",
						Items:       []string{"1x Bowl"},
						SessionInfo: ubereats.SessionInfo{LocationSource: "TARGET", LocationRef: "loc-1"},
					}}, nil
				case ubereats.OrderFilterAll:
					if limit != 1 {
						t.Fatalf("all limit=%d", limit)
					}
					return []ubereats.Order{{
						UUID:        "past-1",
						Merchant:    "Chipotle",
						Status:      "Completed",
						Total:       "$18.50",
						OccurredAt:  "2026-04-12T18:00:00Z",
						Items:       []string{"1x Bowl"},
						SessionInfo: ubereats.SessionInfo{LocationSource: "TARGET", LocationRef: "loc-1"},
					}}, nil
				default:
					t.Fatalf("unexpected filter=%q", filter)
					return nil, nil
				}
			},
			getOrder: func(_ context.Context, ref string) (ubereats.Order, error) {
				if ref != "past-1" {
					t.Fatalf("ref=%q", ref)
				}
				return ubereats.Order{
					UUID:        "past-1",
					Merchant:    "Chipotle",
					Status:      "Completed",
					Total:       "$18.50",
					OccurredAt:  "2026-04-12T18:00:00Z",
					Items:       []string{"1x Bowl"},
					SessionInfo: ubereats.SessionInfo{LocationSource: "TARGET", LocationRef: "loc-1"},
				}, nil
			},
		}
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "config", "set", "--browser-profile", profileDir, "--watch-interval", "30s", "--debug", "on"}, "")
	if err != nil {
		t.Fatalf("config set: %v out=%s", err, out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "login"}, "")
	if err != nil {
		t.Fatalf("login: %v out=%s", err, out)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected out=%q", out)
	}
	rawConfig, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(rawConfig), "cookie_header") {
		t.Fatalf("config persisted cookie header: %s", rawConfig)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "config", "show"}, "")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	for _, want := range []string{
		"browser_profile=" + profileDir,
		"http_user_agent=Mozilla/5.0 Test",
		"default_watch_interval=30s",
		"debug=on",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in out=%s", want, out)
		}
	}
	if strings.Contains(out, "cookie_header=") {
		t.Fatalf("config show leaked cookie header: %s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "orders", "list"}, "")
	if err != nil {
		t.Fatalf("orders list: %v out=%s", err, out)
	}
	if !strings.Contains(out, "uuid=active-1") || strings.Contains(out, "past-1") {
		t.Fatalf("unexpected out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "orders", "--json"}, "")
	if err != nil {
		t.Fatalf("orders --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"uuid": "active-1"`) || strings.Contains(out, `"ok": true`) {
		t.Fatalf("unexpected orders --json out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "orders", "list", "--filter", "past", "--limit", "20"}, "")
	if err != nil {
		t.Fatalf("orders list past: %v out=%s", err, out)
	}
	if !strings.Contains(out, "uuid=past-1") || strings.Contains(out, "active-1") {
		t.Fatalf("unexpected out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "history", "--json"}, "")
	if err != nil {
		t.Fatalf("history --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"uuid": "past-1"`) || strings.Contains(out, `"ok": true`) {
		t.Fatalf("unexpected history --json out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "orders", "show", "past-1"}, "")
	if err != nil {
		t.Fatalf("orders show: %v out=%s", err, out)
	}
	for _, want := range []string{"merchant=Chipotle", "uuid=past-1", "status=Completed", "location_source=TARGET", "location_ref=loc-1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in out=%s", want, out)
		}
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "order", "latest"}, "")
	if err != nil {
		t.Fatalf("order latest: %v out=%s", err, out)
	}
	if !strings.Contains(out, "uuid=past-1") {
		t.Fatalf("unexpected latest out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "order", "past-1", "--json"}, "")
	if err != nil {
		t.Fatalf("order --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"uuid": "past-1"`) || strings.Contains(out, `"ok": true`) {
		t.Fatalf("unexpected order --json out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "logout", "--yes"}, "")
	if err != nil {
		t.Fatalf("logout: %v out=%s", err, out)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected out=%q", out)
	}
	if _, statErr := os.Stat(profileDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected profile dir removed, statErr=%v", statErr)
	}
	out, _, err = runCLI(cfgPath, []string{"ubereats", "config", "show"}, "")
	if err != nil {
		t.Fatalf("config show after logout: %v", err)
	}
	if strings.Contains(out, "browser_profile=") || strings.Contains(out, "http_user_agent=") {
		t.Fatalf("unexpected out=%s", out)
	}
}

func TestUberEatsCLI_ConfigSetRejectsNonEmptyUnmanagedProfileDir(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	profileDir := filepath.Join(t.TempDir(), "existing")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "keep.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, _, err := runCLI(cfgPath, []string{"ubereats", "config", "set", "--browser-profile", profileDir}, "")
	if err == nil || !strings.Contains(err.Error(), "refusing to use non-empty unmanaged profile dir") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestUberEatsCLI_ConfigSetAcceptsDefaultHTTPSPort(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	if _, _, err := runCLI(cfgPath, []string{"ubereats", "config", "set", "--base-url", "https://www.ubereats.com:443"}, ""); err != nil {
		t.Fatalf("config set: %v", err)
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "config", "show"}, "")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if !strings.Contains(out, "base_url=https://www.ubereats.com") {
		t.Fatalf("unexpected out=%s", out)
	}
}

func TestUberEatsCLI_OrdersWatchRejectsPastFilter(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if _, _, err := runCLI(cfgPath, []string{"ubereats", "orders", "list", "--filter", "past", "--watch"}, ""); err == nil || !strings.Contains(err.Error(), "only supported for active orders") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestUberEatsCLI_LoginAdoptsExistingConfiguredProfileDir(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	profileDir := filepath.Join(t.TempDir(), "ubereats-profile")
	if err := os.MkdirAll(filepath.Join(profileDir, "Default"), 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Local State"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write local state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Default", "Cookies"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write cookies: %v", err)
	}

	origLogin := uberEatsLoginBrowser
	origFactory := uberEatsClientFactory
	t.Cleanup(func() {
		uberEatsLoginBrowser = origLogin
		uberEatsClientFactory = origFactory
	})

	uberEatsLoginBrowser = func(_ context.Context, targetURL string, gotProfileDir string, timeout time.Duration) (browserLoginResult, error) {
		return browserLoginResult{
			FinalURL:     targetURL,
			UserAgent:    "Mozilla/5.0 Test",
			CookieHeader: "sid=abc; auth=xyz",
		}, nil
	}
	uberEatsClientFactory = func(st *state, cmd uberEatsCommand) uberEatsClient {
		return fakeUberEatsClient{
			checkSession: func(context.Context) (ubereats.Session, error) {
				return ubereats.Session{LoggedIn: true}, nil
			},
			listOrders: func(context.Context, ubereats.OrderFilter, int) ([]ubereats.Order, error) {
				return nil, nil
			},
			getOrder: func(context.Context, string) (ubereats.Order, error) {
				return ubereats.Order{}, nil
			},
		}
	}

	cfg := config.New()
	cfg.UberEats().BaseURL = "https://www.ubereats.com"
	cfg.UberEats().BrowserProfile = profileDir
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "login"}, "")
	if err != nil {
		t.Fatalf("login: %v out=%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(profileDir, uberEatsProfileMarker)); err != nil {
		t.Fatalf("expected marker: %v", err)
	}
}

func TestUberEatsCLI_LoginDoesNotAdoptLegacyProfileDirWhenValidationFails(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	profileDir := filepath.Join(t.TempDir(), "ubereats-profile")
	if err := os.MkdirAll(filepath.Join(profileDir, "Default"), 0o755); err != nil {
		t.Fatalf("mkdir profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Local State"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write local state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profileDir, "Default", "Cookies"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write cookies: %v", err)
	}

	origLogin := uberEatsLoginBrowser
	origFactory := uberEatsClientFactory
	t.Cleanup(func() {
		uberEatsLoginBrowser = origLogin
		uberEatsClientFactory = origFactory
	})

	uberEatsLoginBrowser = func(_ context.Context, targetURL string, gotProfileDir string, timeout time.Duration) (browserLoginResult, error) {
		return browserLoginResult{
			FinalURL:     targetURL,
			UserAgent:    "Mozilla/5.0 Test",
			CookieHeader: "sid=abc; auth=xyz",
		}, nil
	}
	uberEatsClientFactory = func(st *state, cmd uberEatsCommand) uberEatsClient {
		return fakeUberEatsClient{
			checkSession: func(context.Context) (ubereats.Session, error) {
				return ubereats.Session{LoggedIn: false}, nil
			},
			listOrders: func(context.Context, ubereats.OrderFilter, int) ([]ubereats.Order, error) {
				return nil, nil
			},
			getOrder: func(context.Context, string) (ubereats.Order, error) {
				return ubereats.Order{}, nil
			},
		}
	}

	cfg := config.New()
	cfg.UberEats().BaseURL = "https://www.ubereats.com"
	cfg.UberEats().BrowserProfile = profileDir
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, _, err := runCLI(cfgPath, []string{"ubereats", "login"}, "")
	if err == nil || !strings.Contains(err.Error(), "logged-in Uber Eats session") {
		t.Fatalf("unexpected err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, uberEatsProfileMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no marker after failed validation, err=%v", err)
	}
}
