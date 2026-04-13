package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steipete/ordercli/internal/browserpage"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(t *testing.T, status int, body string) *http.Response {
	t.Helper()
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestNewClient_NormalizesUberBaseURL(t *testing.T) {
	client := NewClient("https://www.ubereats.com:443/en-US", "/tmp/profile", "", nil)
	if client.BaseURL != "https://www.ubereats.com" {
		t.Fatalf("base_url=%q", client.BaseURL)
	}
}

func TestClientCheckSession_UsesProfileSessionWhenCookieMissing(t *testing.T) {
	var sessionReads int
	var requestCookies []string
	client := &Client{
		BaseURL:    "https://www.ubereats.com",
		ProfileDir: "/tmp/ubereats-profile",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requestCookies = append(requestCookies, req.Header.Get("Cookie"))
				return jsonResponse(t, 200, `{"status":"success","data":{"isLoggedIn":true,"firstName":"Prateek"}}`), nil
			}),
		},
		ReadSession: func(_ context.Context, targetURL string, opts browserpage.Options) (browserpage.SessionResult, error) {
			sessionReads++
			if targetURL != "https://www.ubereats.com/orders/" {
				t.Fatalf("target_url=%q", targetURL)
			}
			if opts.ProfileDir != "/tmp/ubereats-profile" || !opts.Headless {
				t.Fatalf("opts=%+v", opts)
			}
			return browserpage.SessionResult{
				FinalURL:     targetURL,
				UserAgent:    "Mozilla/5.0 Test",
				CookieHeader: "sid=abc; auth=xyz",
			}, nil
		},
	}

	session, err := client.CheckSession(context.Background())
	if err != nil {
		t.Fatalf("CheckSession: %v", err)
	}
	if !session.LoggedIn || session.FirstName != "Prateek" {
		t.Fatalf("session=%+v", session)
	}
	if sessionReads != 1 {
		t.Fatalf("sessionReads=%d", sessionReads)
	}
	if len(requestCookies) != 1 || requestCookies[0] != "sid=abc; auth=xyz" {
		t.Fatalf("requestCookies=%v", requestCookies)
	}
}

func TestClientListOrders_PastUsesPlainHTTPWithCookies(t *testing.T) {
	var requests []string
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		UserAgent:    "Mozilla/5.0 Test",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests = append(requests, req.URL.Path)
				if req.Method != http.MethodPost {
					t.Fatalf("method=%s", req.Method)
				}
				if got := req.Header.Get("Cookie"); got != "sid=abc; auth=xyz" {
					t.Fatalf("cookie=%q", got)
				}
				if got := req.Header.Get("User-Agent"); got != "Mozilla/5.0 Test" {
					t.Fatalf("user_agent=%q", got)
				}
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{}}}`), nil
				case "/_p/api/getPastOrdersV1":
					return jsonResponse(t, 200, `{
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
										"shoppingCart":{"items":[{"title":"Bowl","quantity":1}]}
									},
									"storeInfo":{"title":"Chipotle"},
									"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":18.50}]}
								}
							},
							"orderUuids":["past-1"],
							"paginationData":{"nextCursor":""}
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	orders, err := client.ListOrders(context.Background(), OrderFilterPast, 20)
	if err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(orders) != 1 || orders[0].UUID != "past-1" {
		t.Fatalf("orders=%+v", orders)
	}
	if len(requests) != 2 {
		t.Fatalf("requests=%v", requests)
	}
}

func TestClientDefaultLocation_PrefersSavedOverTarget(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(t, 200, `{
					"status":"success",
					"data":{
						"deliveryLocations":{
							"TARGET":[{"location":{"id":"target-1","fullAddress":"Target Address","coordinate":{"latitude":1.0,"longitude":2.0}}}],
							"SAVED":[{"location":{"id":"saved-1","fullAddress":"Saved Address","coordinate":{"latitude":3.0,"longitude":4.0}}}]
						}
					}
				}`), nil
			}),
		},
	}

	location, err := client.DefaultLocation(context.Background())
	if err != nil {
		t.Fatalf("DefaultLocation: %v", err)
	}
	if location.Ref != "saved-1" || location.Source != "SAVED" || location.FullAddress != "Saved Address" {
		t.Fatalf("location=%+v", location)
	}
}

func TestClientGetOrder_PagesPastOrdersUntilFound(t *testing.T) {
	var pastRequests []map[string]any
	var logBuf bytes.Buffer
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		UserAgent:    "Mozilla/5.0 Test",
		LogWriter:    &logBuf,
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getActiveOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"orders":[]}}`), nil
				case "/_p/api/getPastOrdersV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read body: %v", err)
					}
					var payload map[string]any
					if err := json.Unmarshal(rawBody, &payload); err != nil {
						t.Fatalf("decode body: %v", err)
					}
					pastRequests = append(pastRequests, payload)
					if len(pastRequests) == 1 {
						return jsonResponse(t, 200, `{"status":"success","data":{"ordersMap":{},"orderUuids":["old-1"],"paginationData":{"nextCursor":"cursor-2"}}}`), nil
					}
					if len(pastRequests) == 2 {
						return jsonResponse(t, 200, `{
							"status":"success",
							"data":{
								"ordersMap":{
									"past-200":{
										"baseEaterOrder":{
											"uuid":"past-200",
											"displayName":"P-200",
											"isCompleted":true,
											"completedAt":"2026-04-10T11:00:00Z",
											"currencyCode":"USD",
											"shoppingCart":{"items":[{"title":"Salad","quantity":1}]}
										},
										"storeInfo":{"title":"Sweetgreen"},
										"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":14.25}]}
									}
								},
								"orderUuids":["past-200"],
								"paginationData":{"nextCursor":""}
							}
						}`), nil
					}
					t.Fatalf("unexpected page=%d payload=%s", len(pastRequests), string(rawBody))
					return nil, nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	order, err := client.GetOrder(context.Background(), "past-200")
	if err != nil {
		t.Fatalf("GetOrder: %v\ntrace:\n%s", err, logBuf.String())
	}
	if order.UUID != "past-200" {
		t.Fatalf("order=%+v", order)
	}
	if len(pastRequests) != 2 {
		t.Fatalf("pastRequests=%v", pastRequests)
	}
	if got, _ := pastRequests[1]["lastWorkflowUUID"].(string); got != "cursor-2" {
		t.Fatalf("second cursor=%q pastRequests=%v", got, pastRequests)
	}
}

func TestClientGetOrder_FallsBackToPastWhenActiveFails(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{}}}`), nil
				case "/_p/api/getActiveOrdersV1":
					return jsonResponse(t, 500, `{"status":"failure"}`), nil
				case "/_p/api/getPastOrdersV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"ordersMap":{
								"past-9":{
									"baseEaterOrder":{
										"uuid":"past-9",
										"isCompleted":true,
										"completedAt":"2026-04-10T11:00:00Z",
										"currencyCode":"USD",
										"shoppingCart":{"items":[{"title":"Soup","quantity":1}]}
									},
									"storeInfo":{"title":"Store B"},
									"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":9.25}]}
								}
							},
							"orderUuids":["past-9"],
							"paginationData":{"nextCursor":""}
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	order, err := client.GetOrder(context.Background(), "past-9")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if order.UUID != "past-9" {
		t.Fatalf("order=%+v", order)
	}
}
