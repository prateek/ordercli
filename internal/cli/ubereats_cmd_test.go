package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steipete/ordercli/internal/browserpage"
)

func TestUberEatsCLI_Login_Orders_History_Order(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	origLogin := uberEatsLoginBrowser
	origRead := uberEatsReadBrowserPage
	t.Cleanup(func() {
		uberEatsLoginBrowser = origLogin
		uberEatsReadBrowserPage = origRead
	})

	uberEatsLoginBrowser = func(_ context.Context, targetURL string, profileDir string, timeout time.Duration) (browserLoginResult, error) {
		if targetURL != "https://www.ubereats.com/orders/" {
			t.Fatalf("target_url=%q", targetURL)
		}
		if profileDir != "/tmp/ubereats-profile" {
			t.Fatalf("profile_dir=%q", profileDir)
		}
		return browserLoginResult{
			FinalURL:  targetURL,
			UserAgent: "Mozilla/5.0 Test",
		}, nil
	}

	uberEatsReadBrowserPage = func(_ context.Context, targetURL string, opts browserpage.Options) (browserpage.Result, error) {
		if opts.ProfileDir != "/tmp/ubereats-profile" {
			t.Fatalf("profile_dir=%q", opts.ProfileDir)
		}
		if targetURL != "https://www.ubereats.com/orders/" {
			t.Fatalf("unexpected target_url=%q", targetURL)
		}
		if got := strings.Join(opts.CaptureResponseURLSubstrings, ","); got != strings.Join(uberEatsCaptureResponseURLSubstrings, ",") {
			t.Fatalf("capture_response_url_substrings=%q", got)
		}
		return browserpage.Result{
			FinalURL: targetURL,
			Responses: []browserpage.CapturedResponse{
				{
					URL:         "https://www.ubereats.com/_p/api/getActiveOrdersV1",
					Status:      200,
					ContentType: "application/json",
					Body: `{
						"status": "success",
						"data": {
							"orders": [
								{
									"orderUuid": "active-1",
									"orderNumber": "A-101",
									"storeInfo": {"title": "Shake Shack"},
									"currentStatus": {"title": "Preparing your order", "description": "Courier is on the way to the store"},
									"etaRange": {"label": "15-25 min"},
									"baseEaterOrder": {
										"uuid": "active-1",
										"currencyCode": "USD",
										"createdAt": "2026-04-11T16:00:00Z",
										"shoppingCart": {"items": [{"title": "ShackBurger", "quantity": 1}]},
										"deliveryStateChanges": [{"stateChangeTime": "2026-04-11T16:05:00Z", "type": "DISPATCHED"}]
									},
									"fareInfo": {"totalPrice": 2490}
								}
							]
						}
					}`,
				},
				{
					URL:         "https://www.ubereats.com/_p/api/getPastOrdersV1",
					Status:      200,
					ContentType: "application/json",
					Body: `{
						"status":"success",
						"data":{
							"ordersMap":{
								"past-1":{
									"baseEaterOrder":{
										"uuid":"past-1",
										"displayName":"P-201",
										"isCompleted":true,
										"completedAt":"2026-04-12T18:00:00Z",
										"currencyCode":"USD",
										"shoppingCart":{"items":[{"title":"Bowl","quantity":1}]},
										"deliveryStateChanges":[
											{"stateChangeTime":"2026-04-12T17:20:00Z","type":"DISPATCHED"},
											{"stateChangeTime":"2026-04-12T18:00:00Z","type":"COMPLETED"}
										]
									},
									"storeInfo":{
										"title":"Chipotle",
										"location":{"address":{"eaterFormattedAddress":"123 Main St"}}
									},
									"courierInfo":{"name":"Alex"},
									"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":18.50}]}
								},
								"past-2":{
									"baseEaterOrder":{
										"uuid":"past-2",
										"displayName":"P-200",
										"isCompleted":true,
										"completedAt":"2026-04-10T11:00:00Z",
										"currencyCode":"USD",
										"shoppingCart":{"items":[{"title":"Salad","quantity":1}]},
										"deliveryStateChanges":[
											{"stateChangeTime":"2026-04-10T10:15:00Z","type":"DISPATCHED"},
											{"stateChangeTime":"2026-04-10T11:00:00Z","type":"COMPLETED"}
										]
									},
									"storeInfo":{"title":"Sweetgreen"},
									"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":14.25}]}
								}
							}
						}
					}`,
				},
			},
		}, nil
	}

	out, _, err := runCLI(cfgPath, []string{"ubereats", "config", "set", "--browser-profile", "/tmp/ubereats-profile"}, "")
	if err != nil {
		t.Fatalf("config set: %v out=%s", err, out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "login", "--browser"}, "")
	if err != nil {
		t.Fatalf("login: %v out=%s", err, out)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("unexpected out=%q", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "config", "show"}, "")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if !strings.Contains(out, "browser_profile=/tmp/ubereats-profile") || !strings.Contains(out, "http_user_agent=Mozilla/5.0 Test") {
		t.Fatalf("unexpected out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "orders"}, "")
	if err != nil {
		t.Fatalf("orders: %v out=%s", err, out)
	}
	if !strings.Contains(out, "uuid=active-1") || !strings.Contains(out, "merchant=Shake Shack") || strings.Contains(out, "past-1") {
		t.Fatalf("unexpected out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "history"}, "")
	if err != nil {
		t.Fatalf("history: %v out=%s", err, out)
	}
	if strings.Contains(out, "uuid=active-1") || !strings.Contains(out, "uuid=past-1") || !strings.Contains(out, "uuid=past-2") {
		t.Fatalf("unexpected out=%s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "order", "latest"}, "")
	if err != nil {
		t.Fatalf("order latest: %v out=%s", err, out)
	}
	for _, want := range []string{
		"merchant=Chipotle",
		"uuid=past-1",
		"number=P-201",
		"status=Completed",
		"courier=Alex",
		"store_address=123 Main St",
		"1x Bowl",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in out=%s", want, out)
		}
	}
	if strings.Contains(out, "Shake Shack") {
		t.Fatalf("latest should not select the active order: %s", out)
	}

	out, _, err = runCLI(cfgPath, []string{"ubereats", "order", "past-1"}, "")
	if err != nil {
		t.Fatalf("order: %v out=%s", err, out)
	}
	for _, want := range []string{
		"merchant=Chipotle",
		"uuid=past-1",
		"number=P-201",
		"status=Completed",
		"courier=Alex",
		"store_address=123 Main St",
		"1x Bowl",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in out=%s", want, out)
		}
	}
}
