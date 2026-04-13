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
	checkSession          func(context.Context) (ubereats.Session, error)
	listOrders            func(context.Context, ubereats.OrderFilter, int) ([]ubereats.Order, error)
	getOrder              func(context.Context, string) (ubereats.Order, error)
	listLocations         func(context.Context) ([]ubereats.Location, error)
	defaultLocation       func(context.Context) (ubereats.Location, error)
	getInstructionContext func(context.Context, ubereats.Location) (ubereats.InstructionContext, error)
	getStore              func(context.Context, string) (ubereats.Store, error)
	getStoreMenu          func(context.Context, string) (ubereats.StoreMenu, error)
	searchStores          func(context.Context, string, int) ([]ubereats.Store, error)
	searchItems           func(context.Context, string, int) ([]ubereats.StoreItem, error)
	searchStoreItems      func(context.Context, string, string, int) ([]ubereats.StoreItem, error)
	getMenuItem           func(context.Context, string, string) (ubereats.ItemDetail, error)
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

func (f fakeUberEatsClient) ListLocations(ctx context.Context) ([]ubereats.Location, error) {
	return f.listLocations(ctx)
}

func (f fakeUberEatsClient) DefaultLocation(ctx context.Context) (ubereats.Location, error) {
	return f.defaultLocation(ctx)
}

func (f fakeUberEatsClient) GetInstructionContext(ctx context.Context, location ubereats.Location) (ubereats.InstructionContext, error) {
	return f.getInstructionContext(ctx, location)
}

func (f fakeUberEatsClient) GetStore(ctx context.Context, ref string) (ubereats.Store, error) {
	return f.getStore(ctx, ref)
}

func (f fakeUberEatsClient) GetStoreMenu(ctx context.Context, ref string) (ubereats.StoreMenu, error) {
	return f.getStoreMenu(ctx, ref)
}

func (f fakeUberEatsClient) SearchStores(ctx context.Context, query string, limit int) ([]ubereats.Store, error) {
	return f.searchStores(ctx, query, limit)
}

func (f fakeUberEatsClient) SearchItems(ctx context.Context, query string, limit int) ([]ubereats.StoreItem, error) {
	return f.searchItems(ctx, query, limit)
}

func (f fakeUberEatsClient) SearchStoreItems(ctx context.Context, storeRef, query string, limit int) ([]ubereats.StoreItem, error) {
	return f.searchStoreItems(ctx, storeRef, query, limit)
}

func (f fakeUberEatsClient) GetMenuItem(ctx context.Context, storeRef, itemRef string) (ubereats.ItemDetail, error) {
	return f.getMenuItem(ctx, storeRef, itemRef)
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
			listLocations: func(context.Context) ([]ubereats.Location, error) {
				return []ubereats.Location{
					{
						Ref:         "saved-1",
						Source:      "SAVED",
						Label:       "home",
						Title:       "Home",
						FullAddress: "222 E 39th St, New York, NY 10016, US",
					},
					{
						Ref:         "suggested-1",
						Source:      "SUGGESTED",
						Title:       "Office",
						FullAddress: "1 Bryant Park, New York, NY 10036, US",
					},
				}, nil
			},
			defaultLocation: func(context.Context) (ubereats.Location, error) {
				return ubereats.Location{
					Ref:         "saved-1",
					Source:      "SAVED",
					Label:       "home",
					Title:       "Home",
					FullAddress: "222 E 39th St, New York, NY 10016, US",
				}, nil
			},
			getInstructionContext: func(context.Context, ubereats.Location) (ubereats.InstructionContext, error) {
				return ubereats.InstructionContext{
					AvailableInteractionTypes: []string{"door_to_door", "leave_at_door"},
					DefaultInteractionType:    "door_to_door",
					PreferredInteractionType:  "leave_at_door",
					SelectedInstruction: ubereats.Instruction{
						InteractionType: "leave_at_door",
						DisplayString:   "Leave at my door",
					},
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
	if !strings.Contains(out, `"uuid": "active-1"`) || !strings.Contains(out, `"ok": true`) || !strings.Contains(out, `"items"`) {
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
	if !strings.Contains(out, `"uuid": "past-1"`) || !strings.Contains(out, `"ok": true`) || !strings.Contains(out, `"items"`) {
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

	out, _, err = runCLI(cfgPath, []string{"ubereats", "addresses", "list"}, "")
	if err != nil {
		t.Fatalf("addresses list: %v out=%s", err, out)
	}
	if !strings.Contains(out, "ref=saved-1") || strings.Contains(out, "ref=suggested-1") {
		t.Fatalf("unexpected addresses list out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "addresses", "show", "default"}, "")
	if err != nil {
		t.Fatalf("addresses show default: %v out=%s", err, out)
	}
	if !strings.Contains(out, "ref=saved-1") || !strings.Contains(out, "label=home") || !strings.Contains(out, "selected_interaction_type=leave_at_door") {
		t.Fatalf("unexpected addresses show out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "order", "past-1", "--json"}, "")
	if err != nil {
		t.Fatalf("order --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"uuid": "past-1"`) || !strings.Contains(out, `"ok": true`) || !strings.Contains(out, `"item"`) {
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

func TestUberEatsCLI_StoresAndItems(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	oldFactory := uberEatsClientFactory
	t.Cleanup(func() { uberEatsClientFactory = oldFactory })
	uberEatsClientFactory = func(st *state, _ uberEatsCommand) uberEatsClient {
		return fakeUberEatsClient{
			getStore: func(context.Context, string) (ubereats.Store, error) {
				return ubereats.Store{
					Ref:          "store-1",
					Title:        "Rosa Mexicano",
					CurrencyCode: "USD",
					Orderable:    false,
					Favorite:     false,
					Rating:       4.8,
					RatingCount:  "5,000+",
					ETADisplay:   "20-35 min",
					FeeDisplay:   "$2.49",
				}, nil
			},
			getStoreMenu: func(context.Context, string) (ubereats.StoreMenu, error) {
				return ubereats.StoreMenu{
					Store: ubereats.Store{Ref: "store-1", Title: "Rosa Mexicano", CurrencyCode: "USD"},
					Sections: []ubereats.StoreMenuSection{
						{
							Ref:   "section-1",
							Title: "Menu",
							Items: []ubereats.StoreItem{
								{
									Ref:           "item-1",
									StoreRef:      "store-1",
									StoreTitle:    "Rosa Mexicano",
									SectionRef:    "section-1",
									SubsectionRef: "sub-1",
									Title:         "Ahi Tuna Taquitos",
									PriceMinor:    2530,
									CurrencyCode:  "USD",
								},
							},
						},
					},
				}, nil
			},
			searchItems: func(context.Context, string, int) ([]ubereats.StoreItem, error) {
				return []ubereats.StoreItem{
					{
						Ref:               "item-2",
						StoreRef:          "store-2",
						StoreTitle:        "CVS",
						SectionRef:        "section-9",
						SubsectionRef:     "sub-9",
						Title:             "Gummy Bears",
						Description:       "Haribo",
						PriceMinor:        0,
						CurrencyCode:      "USD",
						SoldOut:           false,
						HasCustomizations: false,
					},
				}, nil
			},
			searchStoreItems: func(context.Context, string, string, int) ([]ubereats.StoreItem, error) {
				return []ubereats.StoreItem{
					{
						Ref:           "item-1",
						StoreRef:      "store-1",
						StoreTitle:    "Rosa Mexicano",
						SectionRef:    "section-1",
						SubsectionRef: "sub-1",
						Title:         "Ahi Tuna Taquitos",
						Description:   "Soy-lime marinade",
						PriceMinor:    2530,
						CurrencyCode:  "USD",
					},
				}, nil
			},
			getMenuItem: func(context.Context, string, string) (ubereats.ItemDetail, error) {
				return ubereats.ItemDetail{
					StoreItem: ubereats.StoreItem{
						Ref:               "item-1",
						StoreRef:          "store-1",
						StoreTitle:        "Rosa Mexicano",
						SectionRef:        "section-1",
						SubsectionRef:     "sub-1",
						Title:             "Ahi Tuna Taquitos",
						Description:       "Soy-lime marinade",
						PriceMinor:        2530,
						CurrencyCode:      "USD",
						HasCustomizations: true,
					},
					Customizations: []ubereats.CustomizationGroup{
						{
							Ref:   "group-1",
							Title: "Add Dips",
							Options: []ubereats.CustomizationOption{
								{Ref: "option-1", Title: "Caesar Dressing", PriceMinor: 0},
							},
						},
					},
				}, nil
			},
		}
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "stores", "show", "store-1"}, "")
	if err != nil {
		t.Fatalf("stores show: %v out=%s", err, out)
	}
	if !strings.Contains(out, "ref=store-1") || !strings.Contains(out, "title=Rosa Mexicano") {
		t.Fatalf("unexpected stores show out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "stores", "show", "store-1", "--json"}, "")
	if err != nil {
		t.Fatalf("stores show --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"orderable": false`) || !strings.Contains(out, `"favorite": false`) {
		t.Fatalf("unexpected stores show --json out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "stores", "menu", "store-1"}, "")
	if err != nil {
		t.Fatalf("stores menu: %v out=%s", err, out)
	}
	if !strings.Contains(out, "section_ref=section-1") || !strings.Contains(out, "item_ref=item-1") {
		t.Fatalf("unexpected stores menu out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "items", "search", "tuna", "--store", "store-1"}, "")
	if err != nil {
		t.Fatalf("items search: %v out=%s", err, out)
	}
	if !strings.Contains(out, "item_ref=item-1") || !strings.Contains(out, "store_ref=store-1") {
		t.Fatalf("unexpected items search out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "items", "search", "gummy"}, "")
	if err != nil {
		t.Fatalf("items search global: %v out=%s", err, out)
	}
	if !strings.Contains(out, "item_ref=item-2") || !strings.Contains(out, "store_ref=store-2") {
		t.Fatalf("unexpected global items search out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "items", "search", "gummy", "--json"}, "")
	if err != nil {
		t.Fatalf("items search --json: %v out=%s", err, out)
	}
	for _, want := range []string{`"price_minor": 0`, `"sold_out": false`, `"has_customizations": false`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in items search --json out=%s", want, out)
		}
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "items", "show", "item-1", "--store", "store-1"}, "")
	if err != nil {
		t.Fatalf("items show: %v out=%s", err, out)
	}
	if !strings.Contains(out, "item_ref=item-1") || !strings.Contains(out, "customization_ref=group-1") || strings.Contains(out, "option_ref=option-1 title=Caesar Dressing price=") {
		t.Fatalf("unexpected items show out=%s", out)
	}
}

func TestUberEatsCLI_StoreSearch(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	oldFactory := uberEatsClientFactory
	t.Cleanup(func() { uberEatsClientFactory = oldFactory })
	uberEatsClientFactory = func(st *state, _ uberEatsCommand) uberEatsClient {
		return fakeUberEatsClient{
			searchStores: func(context.Context, string, int) ([]ubereats.Store, error) {
				return []ubereats.Store{
					{
						Ref:          "store-1",
						Title:        "CVS",
						CurrencyCode: "USD",
						Orderable:    true,
						Favorite:     true,
						ETADisplay:   "15-25 min",
						FeeDisplay:   "$0.00",
					},
				}, nil
			},
		}
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "stores", "search", "pharmacy"}, "")
	if err != nil {
		t.Fatalf("stores search: %v out=%s", err, out)
	}
	if !strings.Contains(out, "ref=store-1") || !strings.Contains(out, "title=CVS") {
		t.Fatalf("unexpected stores search out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "stores", "search", "pharmacy", "--json"}, "")
	if err != nil {
		t.Fatalf("stores search --json: %v out=%s", err, out)
	}
	if !strings.Contains(out, `"ref": "store-1"`) || !strings.Contains(out, `"items"`) {
		t.Fatalf("unexpected stores search --json out=%s", out)
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

func TestUberEatsCLI_LoginRejectsLegacyProfileDirWithoutMarker(t *testing.T) {
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

	cfg := config.New()
	cfg.UberEats().BaseURL = "https://www.ubereats.com"
	cfg.UberEats().BrowserProfile = profileDir
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, _, err := runCLI(cfgPath, []string{"ubereats", "login"}, "")
	if err == nil || !strings.Contains(err.Error(), "refusing to use non-empty unmanaged profile dir") {
		t.Fatalf("unexpected err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(profileDir, uberEatsProfileMarker)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no marker after rejected legacy dir, err=%v", err)
	}
}

func TestUberEatsCLI_ConfigShowSanitizesTamperedBaseURL(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	raw := []byte("{\n  \"version\": 1,\n  \"providers\": {\n    \"ubereats\": {\n      \"base_url\": \"https://evil.example.com\",\n      \"default_watch_interval\": 15000000000\n    }\n  }\n}\n")
	if err := os.WriteFile(cfgPath, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "config", "show"}, "")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if !strings.Contains(out, "base_url=https://www.ubereats.com") {
		t.Fatalf("unexpected out=%s", out)
	}
}
