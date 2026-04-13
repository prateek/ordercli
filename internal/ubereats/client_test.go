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

func TestNewClient_UsesSafeDefaultForUnsupportedBaseURL(t *testing.T) {
	client := NewClient("https://evil.example.com", "/tmp/profile", "", nil)
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
	if len(requestCookies) != 1 || !strings.Contains(requestCookies[0], "sid=abc") || !strings.Contains(requestCookies[0], "auth=xyz") {
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
				if got := req.Header.Get("Cookie"); !strings.Contains(got, "sid=abc") || !strings.Contains(got, "auth=xyz") {
					t.Fatalf("cookie=%q", got)
				}
				if got := req.Header.Get("User-Agent"); got != "Mozilla/5.0 Test" {
					t.Fatalf("user_agent=%q", got)
				}
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
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

func TestClientListOrders_UsesUpdatedCookiesAcrossRequests(t *testing.T) {
	var seenCookies []string
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=old; auth=xyz",
		UserAgent:    "Mozilla/5.0 Test",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				seenCookies = append(seenCookies, req.Header.Get("Cookie"))
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					resp := jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`)
					resp.Header.Add("Set-Cookie", "sid=new; Path=/; HttpOnly")
					return resp, nil
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
	if len(seenCookies) != 2 {
		t.Fatalf("seenCookies=%v", seenCookies)
	}
	if !strings.Contains(seenCookies[0], "sid=old") {
		t.Fatalf("initial cookie=%q", seenCookies[0])
	}
	if !strings.Contains(seenCookies[1], "sid=new") || strings.Contains(seenCookies[1], "sid=old") {
		t.Fatalf("rotated cookie=%q", seenCookies[1])
	}
}

func TestClientListOrders_StripsQuotedCookieValuesWhenSeedingJar(t *testing.T) {
	var seenCookies []string
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: `sid="quoted-value"; auth=xyz`,
		UserAgent:    "Mozilla/5.0 Test",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				seenCookies = append(seenCookies, req.Header.Get("Cookie"))
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
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

	if _, err := client.ListOrders(context.Background(), OrderFilterPast, 20); err != nil {
		t.Fatalf("ListOrders: %v", err)
	}
	if len(seenCookies) == 0 {
		t.Fatalf("seenCookies=%v", seenCookies)
	}
	if !strings.Contains(seenCookies[0], "sid=quoted-value") || strings.Contains(seenCookies[0], `"quoted-value"`) {
		t.Fatalf("seeded cookie=%q", seenCookies[0])
	}
}

func TestClientListOrders_FailsWhenDefaultLocationUnavailable(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/_p/api/getDeliveryLocationsV2" {
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{}}}`), nil
			}),
		},
	}

	_, err := client.ListOrders(context.Background(), OrderFilterPast, 20)
	if err == nil || !strings.Contains(err.Error(), "no delivery location found") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientDefaultLocation_PrefersTargetOverSaved(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(t, 200, `{
					"status":"success",
					"data":{
						"deliveryLocations":{
							"TARGET":[{"title":"Target Address","subtitle":"New York, NY","location":{"id":"target-1","coordinate":{"latitude":1.0,"longitude":2.0}}}],
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
	if location.Ref != "target-1" || location.Source != "TARGET" || location.FullAddress != "Target Address, New York, NY" {
		t.Fatalf("location=%+v", location)
	}
}

func TestClientListLocations_ExtractsAllSources(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return jsonResponse(t, 200, `{
					"status":"success",
					"data":{
						"deliveryLocations":{
							"SAVED":[
								{
									"title":"Home",
									"subtitle":"New York, NY",
									"location":{
										"id":"saved-1",
										"fullAddress":"222 E 39th St, New York, NY 10016, US",
										"coordinate":{"latitude":40.748198,"longitude":-73.9746683},
										"personalization":{"label":"home"}
									}
								}
							],
							"TARGET":[
								{
									"title":"Office",
									"subtitle":"Midtown",
									"location":{
										"id":"target-1",
										"coordinate":{"latitude":40.752700,"longitude":-73.977200}
									}
								}
							],
							"SUGGESTED":[
								{
									"location":{
										"id":"suggested-1",
										"name":"Bryant Park",
										"fullAddress":"1 Bryant Park, New York, NY 10036, US",
										"coordinate":{"latitude":40.755500,"longitude":-73.984000}
									}
								}
							]
						}
					}
				}`), nil
			}),
		},
	}

	locations, err := client.ListLocations(context.Background())
	if err != nil {
		t.Fatalf("ListLocations: %v", err)
	}
	if len(locations) != 3 {
		t.Fatalf("locations=%+v", locations)
	}
	if locations[0].Ref != "target-1" || locations[0].Source != "TARGET" || locations[0].Title != "Office" || locations[0].FullAddress != "Office, Midtown" {
		t.Fatalf("target=%+v", locations[0])
	}
	if locations[1].Ref != "saved-1" || locations[1].Source != "SAVED" || locations[1].Label != "home" || locations[1].Title != "Home" {
		t.Fatalf("saved=%+v", locations[1])
	}
	if locations[2].Ref != "suggested-1" || locations[2].Source != "SUGGESTED" || locations[2].Title != "Bryant Park" {
		t.Fatalf("suggested=%+v", locations[2])
	}
}

func TestClientGetInstructionContext_BuildsLocationRequest(t *testing.T) {
	var requestBody map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/_p/api/getInstructionForLocationV1" {
					t.Fatalf("path=%s", req.URL.Path)
				}
				rawBody, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if err := json.Unmarshal(rawBody, &requestBody); err != nil {
					t.Fatalf("unmarshal request body: %v", err)
				}
				return jsonResponse(t, 200, `{
					"status":"success",
					"data":{
						"availableInteractionTypes":["door_to_door","leave_at_door"],
						"defaultInteractionType":"door_to_door",
						"preferredInteractionType":"leave_at_door",
						"selectedInstruction":{
							"interactionType":"leave_at_door",
							"displayString":"Leave at my door",
							"notes":"use the side entrance"
						}
					}
				}`), nil
			}),
		},
	}

	location := Location{
		Ref:         "loc-1",
		Source:      "TARGET",
		Label:       "home",
		Title:       "222 E 39th St",
		FullAddress: "222 E 39th St, New York, NY 10016-2754, US",
		Latitude:    40.748198,
		Longitude:   -73.9746683,
		locationPayload: map[string]any{
			"location": map[string]any{
				"id":           "loc-1",
				"provider":     "uber_places",
				"addressLine1": "222 E 39th St",
				"addressLine2": "New York, NY",
				"fullAddress":  "222 E 39th St, New York, NY 10016-2754, US",
				"coordinate":   map[string]any{"latitude": 40.748198, "longitude": -73.9746683},
				"categories":   []any{"RESIDENCE"},
				"personalization": map[string]any{
					"label": "home",
				},
				"addressComponents": map[string]any{
					"CITY":                         "New York",
					"COUNTRY_CODE":                 "US",
					"FIRST_LEVEL_SUBDIVISION_CODE": "NY",
					"POSTAL_CODE":                  "10016-2754",
				},
			},
		},
	}

	context, err := client.GetInstructionContext(context.Background(), location)
	if err != nil {
		t.Fatalf("GetInstructionContext: %v", err)
	}
	if context.DefaultInteractionType != "door_to_door" || context.PreferredInteractionType != "leave_at_door" || context.SelectedInstruction.InteractionType != "leave_at_door" {
		t.Fatalf("context=%+v", context)
	}

	locationPayload, _ := requestBody["location"].(map[string]any)
	address, _ := locationPayload["address"].(map[string]any)
	if address["address1"] != "222 E 39th St" || address["address2"] != "New York, NY" || address["label"] != "home" {
		t.Fatalf("address payload=%+v", address)
	}
	if locationPayload["reference"] != "loc-1" || locationPayload["referenceType"] != "uber_places" || locationPayload["type"] != "uber_places" {
		t.Fatalf("location payload=%+v", locationPayload)
	}
	addressComponents, _ := locationPayload["addressComponents"].(map[string]any)
	if addressComponents["city"] != "New York" || addressComponents["countryCode"] != "US" || addressComponents["postalCode"] != "10016-2754" {
		t.Fatalf("addressComponents=%+v", addressComponents)
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
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
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

func TestClientGetOrder_FailsWhenDefaultLocationUnavailable(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/_p/api/getDeliveryLocationsV2" {
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{}}}`), nil
			}),
		},
	}

	_, err := client.GetOrder(context.Background(), "past-9")
	if err == nil || !strings.Contains(err.Error(), "no delivery location found") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientListCarts_UsesCartView(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/_p/api/getCartsViewForEaterUuidV1" {
					t.Fatalf("unexpected request %s", req.URL.Path)
				}
				return jsonResponse(t, 200, `{
					"status":"success",
					"data":{
						"cartsView":{
							"carts":[
								{
									"draftOrderUUID":"draft-1",
									"cartUUID":"cart-1",
									"storeUUID":"store-1",
									"storeTitle":"CVS",
									"currencyCode":"USD",
									"deliveryType":"ASAP",
									"deliveryAddress":{"fullAddress":"222 E 39th St"},
									"shoppingCart":{"itemCount":2,"subtotal":1599}
								}
							]
						}
					}
				}`), nil
			}),
		},
	}

	carts, err := client.ListCarts(context.Background(), 20)
	if err != nil {
		t.Fatalf("ListCarts: %v", err)
	}
	if len(carts) != 1 {
		t.Fatalf("carts=%+v", carts)
	}
	if carts[0].Ref != "draft-1" || carts[0].CartRef != "cart-1" || carts[0].StoreTitle != "CVS" {
		t.Fatalf("cart=%+v", carts[0])
	}
	if carts[0].ItemCount != 2 || carts[0].Subtotal != "$15.99" {
		t.Fatalf("cart=%+v", carts[0])
	}
}

func TestClientGetCart_IncludesSessionContext(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"cartsView":{
								"carts":[
									{
										"draftOrderUUID":"draft-1",
										"title":"CVS",
										"tagline1":{"text":"Subtotal: $15.99"},
										"tagline2":{"text":"Deliver to 222 E 39th St"},
										"itemCount":2
									}
								]
							}
						}
					}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"fareBreakdown":{"displayString":"Fees $1.25"},
							"deliveryAddress":{"fullAddress":"222 E 39th St"},
							"shoppingCart":{
								"cartUuid":"cart-1",
								"currencyCode":"USD",
								"isActive":true,
								"items":[
									{
										"shoppingCartItemUuid":"line-1",
										"uuid":"item-1",
										"title":"Gummy Bears",
										"quantity":2,
										"price":399,
										"totalPrice":798,
										"specialInstructions":"red only"
									}
								]
							}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	cart, err := client.GetCart(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if cart.Ref != "draft-1" || cart.CartRef != "cart-1" || cart.StoreTitle != "CVS" {
		t.Fatalf("cart=%+v", cart)
	}
	if cart.ItemCount != 2 || len(cart.Items) != 1 {
		t.Fatalf("cart=%+v", cart)
	}
	if cart.Items[0].Ref != "line-1" || cart.Items[0].ItemRef != "item-1" || cart.Items[0].Note != "red only" {
		t.Fatalf("item=%+v", cart.Items[0])
	}
	if cart.FeeSummary != "Fees $1.25" {
		t.Fatalf("cart=%+v", cart)
	}
	if cart.SessionInfo.LocationSource != "TARGET" || cart.SessionInfo.LocationRef != "loc-1" || cart.SessionInfo.Profile != "Personal" || cart.SessionInfo.PaymentProfileRef != "payment-1" {
		t.Fatalf("session=%+v cart=%+v", cart.SessionInfo, cart)
	}
	if !cart.CheckoutReady {
		t.Fatalf("cart=%+v", cart)
	}
}

func TestClientGetCart_AllowsMissingDefaultLocation(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"cartsView":{
								"carts":[
									{
										"draftOrderUUID":"draft-1",
										"title":"CVS",
										"tagline1":{"text":"Subtotal: $3.99"},
										"tagline2":{"text":"Deliver to 222 E 39th St"},
										"itemCount":1,
										"action":"OPEN_CHECKOUT"
									}
								]
							}
						}
					}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"shoppingCart":{
								"cartUuid":"cart-1",
								"currencyCode":"USD",
								"isActive":true,
								"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]
							}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	cart, err := client.GetCart(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("GetCart: %v", err)
	}
	if cart.Ref != "draft-1" || cart.SessionInfo.Profile != "Personal" {
		t.Fatalf("cart=%+v", cart)
	}
	if cart.CheckoutReady != true {
		t.Fatalf("cart=%+v", cart)
	}
	if cart.SessionInfo.LocationRef != "" || cart.SessionInfo.Location != "" {
		t.Fatalf("expected missing location enrichment, got %+v", cart.SessionInfo)
	}
}

func TestClientCreateCartFromItem_UsesCreateDraftOrder(t *testing.T) {
	oldNewUUID := newUUID
	newUUID = func() string { return "line-uuid-1" }
	t.Cleanup(func() { newUUID = oldNewUUID })

	var createPayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"title":"CVS",
							"uuid":"store-1",
							"currencyCode":"USD",
							"isOrderable":true,
							"sections":[{"uuid":"section-1","title":"Candy","subsectionUuids":["subsection-1"]}],
							"catalogSectionsMap":{
								"section-1":[{"payload":{"standardItemsPayload":{"catalogItems":[{"uuid":"item-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","price":399,"hasCustomizations":false}]}}}]
							}
						}
					}`), nil
				case "/_p/api/getMenuItemV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"item-1",
							"title":"Gummy Bears",
							"sectionUuid":"section-1",
							"subsectionUuid":"subsection-1",
							"price":399,
							"hasCustomizations":false,
							"customizationsList":[]
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getInstructionForLocationV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"preferredInteractionType":"leave_at_door","defaultInteractionType":"door_to_door","selectedInstruction":{"interactionType":"leave_at_door"}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/createDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read create body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &createPayload); err != nil {
						t.Fatalf("unmarshal create body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"draftOrder":{"uuid":"draft-1"}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"shoppingCart":{
								"cartUuid":"cart-1",
								"currencyCode":"USD",
								"isActive":true,
								"items":[{"shoppingCartItemUuid":"line-uuid-1","uuid":"item-1","title":"Gummy Bears","quantity":2,"price":399,"totalPrice":798,"specialInstructions":"red only"}]
							}
						}
					}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","title":"CVS","tagline1":{"text":"Subtotal: $7.98"},"tagline2":{"text":"Deliver to 222 E 39th St"},"itemCount":2,"action":"OPEN_CHECKOUT"}]}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	cart, err := client.CreateCartFromItem(context.Background(), "store-1", "item-1", 2, "red only")
	if err != nil {
		t.Fatalf("CreateCartFromItem: %v", err)
	}
	if cart.Ref != "draft-1" || cart.ItemCount != 2 || cart.Subtotal != "$7.98" {
		t.Fatalf("cart=%+v", cart)
	}
	items, _ := createPayload["shoppingCartItems"].([]any)
	if len(items) != 1 {
		t.Fatalf("createPayload=%v", createPayload)
	}
	item, _ := items[0].(map[string]any)
	if item["shoppingCartItemUuid"] != "line-uuid-1" || item["quantity"] != float64(2) || item["specialInstructions"] != "red only" {
		t.Fatalf("item payload=%v", item)
	}
	if createPayload["paymentProfileUUID"] != "payment-1" || createPayload["interactionType"] != "leave_at_door" {
		t.Fatalf("createPayload=%v", createPayload)
	}
}

func TestClientCreateCartFromOrder_UsesPastOrderItems(t *testing.T) {
	oldNewUUID := newUUID
	uuidValues := []string{"line-uuid-1", "line-uuid-2"}
	newUUID = func() string {
		value := uuidValues[0]
		uuidValues = uuidValues[1:]
		return value
	}
	t.Cleanup(func() { newUUID = oldNewUUID })

	var createPayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getInstructionForLocationV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"preferredInteractionType":"leave_at_door","defaultInteractionType":"door_to_door","selectedInstruction":{"interactionType":"leave_at_door"}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/getActiveOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"orders":[]}}`), nil
				case "/_p/api/getPastOrdersV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"ordersMap":{
								"order-1":{
									"baseEaterOrder":{
										"uuid":"order-1",
										"storeUuid":"store-1",
										"currencyCode":"USD",
										"shoppingCart":{
											"items":[
												{"uuid":"item-1","shoppingCartItemUuid":"old-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","price":399,"title":"Gummy Bears","quantity":2,"specialInstructions":"red only","customizations":{}},
												{"uuid":"item-2","shoppingCartItemUuid":"old-2","storeUuid":"store-1","sectionUuid":"section-2","subsectionUuid":"subsection-2","price":250,"title":"Cola","quantity":1,"specialInstructions":"","customizations":{"addon":[{"uuid":"opt-1","quantity":1}]}}
											]
										}
									}
								}
							},
							"orderUuids":["order-1"],
							"paginationData":{"nextCursor":""}
						}
					}`), nil
				case "/_p/api/createDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read create body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &createPayload); err != nil {
						t.Fatalf("unmarshal create body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"draftOrder":{"uuid":"draft-1"}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"shoppingCart":{
								"cartUuid":"cart-1",
								"currencyCode":"USD",
								"isActive":true,
								"items":[
									{"shoppingCartItemUuid":"line-uuid-1","uuid":"item-1","title":"Gummy Bears","quantity":2,"price":399,"totalPrice":798,"specialInstructions":"red only"},
									{"shoppingCartItemUuid":"line-uuid-2","uuid":"item-2","title":"Cola","quantity":1,"price":250,"totalPrice":250}
								]
							}
						}
					}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","title":"CVS","tagline1":{"text":"Subtotal: $10.48"},"tagline2":{"text":"Deliver to 222 E 39th St"},"itemCount":3,"action":"OPEN_CHECKOUT"}]}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	cart, err := client.CreateCartFromOrder(context.Background(), "order-1")
	if err != nil {
		t.Fatalf("CreateCartFromOrder: %v", err)
	}
	if cart.Ref != "draft-1" || cart.ItemCount != 3 || cart.Subtotal != "$10.48" {
		t.Fatalf("cart=%+v", cart)
	}
	items, _ := createPayload["shoppingCartItems"].([]any)
	if len(items) != 2 {
		t.Fatalf("createPayload=%v", createPayload)
	}
	item1, _ := items[0].(map[string]any)
	item2, _ := items[1].(map[string]any)
	if item1["shoppingCartItemUuid"] != "line-uuid-1" || item1["quantity"] != float64(2) || item1["specialInstructions"] != "red only" {
		t.Fatalf("item1 payload=%v", item1)
	}
	if item2["shoppingCartItemUuid"] != "line-uuid-2" {
		t.Fatalf("item2 payload=%v", item2)
	}
	if createPayload["paymentProfileUUID"] != "payment-1" || createPayload["interactionType"] != "leave_at_door" {
		t.Fatalf("createPayload=%v", createPayload)
	}
}

func TestClientCreateCartFromOrder_ReturnsActiveLookupErrorWhenPastLookupMisses(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getActiveOrdersV1":
					return jsonResponse(t, 500, `{"status":"failure"}`), nil
				case "/_p/api/getPastOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"ordersMap":{},"orderUuids":[],"paginationData":{"nextCursor":""}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.CreateCartFromOrder(context.Background(), "order-1")
	if err == nil || !strings.Contains(err.Error(), "getActiveOrdersV1") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientCreateCartFromItem_RejectsNonPositiveQuantity(t *testing.T) {
	client := &Client{}
	_, err := client.CreateCartFromItem(context.Background(), "store-1", "item-1", 0, "")
	if err == nil || !strings.Contains(err.Error(), "quantity must be positive") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientCreateCartFromItem_RejectsRequiredCustomizations(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"title":"CVS",
							"uuid":"store-1",
							"currencyCode":"USD",
							"isOrderable":true,
							"sections":[{"uuid":"section-1","title":"Candy","subsectionUuids":["subsection-1"]}],
							"catalogSectionsMap":{
								"section-1":[{"payload":{"standardItemsPayload":{"catalogItems":[{"uuid":"item-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","price":399,"hasCustomizations":true}]}}}]
							}
						}
					}`), nil
				case "/_p/api/getMenuItemV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"item-1",
							"title":"Gummy Bears",
							"sectionUuid":"section-1",
							"subsectionUuid":"subsection-1",
							"price":399,
							"hasCustomizations":true,
							"customizationsList":[{"title":"Size","minPermitted":1,"maxPermitted":1,"options":[{"uuid":"opt-1","title":"Large"}]}]
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.CreateCartFromItem(context.Background(), "store-1", "item-1", 1, "")
	if err == nil || !strings.Contains(err.Error(), "requires customizations") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientRemoveCartItem_RemovesAndDetectsCollapsedCart(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/removeItemsFromDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"shoppingCartItemUUIDs":["line-1"],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[]}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.RemoveCartItem(context.Background(), "draft-1", "line-1")
	if err != nil {
		t.Fatalf("RemoveCartItem: %v", err)
	}
	if !result.Removed || result.Ref != "draft-1" || result.Cart != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientAddCartItem_UsesAddItemsEndpoint(t *testing.T) {
	oldNewUUID := newUUID
	newUUID = func() string { return "line-uuid-2" }
	t.Cleanup(func() { newUUID = oldNewUUID })

	var addPayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[
								{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}
							]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"title":"CVS",
							"uuid":"store-1",
							"currencyCode":"USD",
							"isOrderable":true,
							"sections":[{"uuid":"section-2","title":"Drinks","subsectionUuids":["subsection-2"]}],
							"catalogSectionsMap":{
								"section-2":[{"payload":{"standardItemsPayload":{"catalogItems":[{"uuid":"item-2","sectionUuid":"section-2","subsectionUuid":"subsection-2","title":"Cola","price":250,"hasCustomizations":false}]}}}]
							}
						}
					}`), nil
				case "/_p/api/getMenuItemV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"item-2","title":"Cola","sectionUuid":"section-2","subsectionUuid":"subsection-2","price":250,"hasCustomizations":false,"customizationsList":[]}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/addItemsToDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read add body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &addPayload); err != nil {
						t.Fatalf("unmarshal add body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"addedItems":[{"uuid":"item-2","shoppingCartItemUuid":"line-uuid-2"}],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.AddCartItem(context.Background(), "draft-1", "item-2", 2, "cold")
	if err != nil {
		t.Fatalf("AddCartItem: %v", err)
	}
	if !result.Added || result.Cart == nil || result.Cart.ItemCount != 1 {
		t.Fatalf("result=%+v", result)
	}
	items, _ := addPayload["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("addPayload=%v", addPayload)
	}
	item, _ := items[0].(map[string]any)
	if item["shoppingCartItemUuid"] != "line-uuid-2" || item["quantity"] != float64(2) || item["specialInstructions"] != "cold" {
		t.Fatalf("item payload=%v", item)
	}
}

func TestClientAddCartItem_ErrorsWhenResponseDoesNotConfirmAddedItem(t *testing.T) {
	oldNewUUID := newUUID
	newUUID = func() string { return "line-uuid-2" }
	t.Cleanup(func() { newUUID = oldNewUUID })

	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"title":"CVS","uuid":"store-1","currencyCode":"USD","isOrderable":true,"sections":[{"uuid":"section-2","title":"Drinks","subsectionUuids":["subsection-2"]}],"catalogSectionsMap":{"section-2":[{"payload":{"standardItemsPayload":{"catalogItems":[{"uuid":"item-2","sectionUuid":"section-2","subsectionUuid":"subsection-2","title":"Cola","price":250,"hasCustomizations":false}]}}}]}}}`), nil
				case "/_p/api/getMenuItemV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"item-2","title":"Cola","sectionUuid":"section-2","subsectionUuid":"subsection-2","price":250,"hasCustomizations":false,"customizationsList":[]}}`), nil
				case "/_p/api/addItemsToDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"addedItems":[{"uuid":"item-2","shoppingCartItemUuid":"other-line"}],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.AddCartItem(context.Background(), "draft-1", "item-2", 2, "cold")
	if err == nil || !strings.Contains(err.Error(), "did not confirm added item") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientUpdateCartItem_UsesExistingLinePayload(t *testing.T) {
	var updatePayload map[string]any
	var draftReads int
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					draftReads++
					if draftReads == 1 {
						return jsonResponse(t, 200, `{
							"status":"success",
							"data":{
								"uuid":"draft-1",
								"state":"UNORDERED",
								"storeUuid":"store-1",
								"deliveryType":"ASAP",
								"interactionType":"leave_at_door",
								"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[
									{"shoppingCartItemUuid":"line-1","uuid":"item-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","quantity":1,"price":399,"specialInstructions":"old note","customizations":{"addon":[{"uuid":"opt-1","quantity":1}]}}
								]}
							}
						}`), nil
					}
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"leave_at_door",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[
								{"shoppingCartItemUuid":"line-1","uuid":"item-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","quantity":3,"price":399,"specialInstructions":"less ice","customizations":{"addon":[{"uuid":"opt-1","quantity":1}]}}
							]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/updateItemInDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read update body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &updatePayload); err != nil {
						t.Fatalf("unmarshal update body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"item":{"uuid":"item-1","shoppingCartItemUuid":"line-1","quantity":3},"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	quantity := 3
	note := "less ice"
	result, err := client.UpdateCartItem(context.Background(), "draft-1", "line-1", CartItemUpdate{Quantity: &quantity, Note: &note})
	if err != nil {
		t.Fatalf("UpdateCartItem: %v", err)
	}
	if !result.Updated || result.Cart == nil || result.Cart.Ref != "draft-1" {
		t.Fatalf("result=%+v", result)
	}
	item, _ := updatePayload["item"].(map[string]any)
	if item["quantity"] != float64(3) || item["specialInstructions"] != "less ice" {
		t.Fatalf("item payload=%v", item)
	}
	customizations, _ := item["customizations"].(map[string]any)
	if len(customizations) == 0 {
		t.Fatalf("item payload=%v", item)
	}
}

func TestClientUpdateCartItem_ErrorsWhenResponseDoesNotConfirmUpdatedItem(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","quantity":1,"price":399,"customizations":{}}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/updateItemInDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"item":{"uuid":"item-1","shoppingCartItemUuid":"other-line","quantity":3},"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	quantity := 3
	_, err := client.UpdateCartItem(context.Background(), "draft-1", "line-1", CartItemUpdate{Quantity: &quantity})
	if err == nil || !strings.Contains(err.Error(), "did not confirm update") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientUpdateCartItem_ErrorsWhenUpdatedCartDoesNotReflectRequestedFields(t *testing.T) {
	var draftReads int
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					draftReads++
					if draftReads == 1 {
						return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","quantity":1,"price":399,"specialInstructions":"old note","customizations":{}}]}}}`), nil
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","storeUuid":"store-1","sectionUuid":"section-1","subsectionUuid":"subsection-1","title":"Gummy Bears","quantity":1,"price":399,"specialInstructions":"old note","customizations":{}}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/updateItemInDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"item":{"uuid":"item-1","shoppingCartItemUuid":"line-1","quantity":3},"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	quantity := 3
	note := "less ice"
	_, err := client.UpdateCartItem(context.Background(), "draft-1", "line-1", CartItemUpdate{Quantity: &quantity, Note: &note})
	if err == nil || !strings.Contains(err.Error(), "did not apply") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientRemoveCartItem_TreatsMissingCartAfterRefreshFailureAsRemoved(t *testing.T) {
	var draftReads int
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					draftReads++
					if draftReads == 1 {
						return jsonResponse(t, 200, `{
							"status":"success",
							"data":{
								"uuid":"draft-1",
								"state":"UNORDERED",
								"storeUuid":"store-1",
								"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
							}
						}`), nil
					}
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/removeItemsFromDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"shoppingCartItemUUIDs":["line-1"],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return nil, context.DeadlineExceeded
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.RemoveCartItem(context.Background(), "draft-1", "line-1")
	if err != nil {
		t.Fatalf("RemoveCartItem: %v", err)
	}
	if !result.Removed || result.Ref != "draft-1" || result.Cart != nil {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientRemoveCartItem_ReturnsUpdatedCartWhenCartSurvives(t *testing.T) {
	var draftReads int
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					draftReads++
					if draftReads == 1 {
						return jsonResponse(t, 200, `{
							"status":"success",
							"data":{
								"uuid":"draft-1",
								"state":"UNORDERED",
								"storeUuid":"store-1",
								"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[
									{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399},
									{"shoppingCartItemUuid":"line-2","uuid":"item-2","title":"Cola","quantity":1,"price":250}
								]}
							}
						}`), nil
					}
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[
								{"shoppingCartItemUuid":"line-2","uuid":"item-2","title":"Cola","quantity":1,"price":250}
							]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{}}`), nil
				case "/_p/api/removeItemsFromDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"shoppingCartItemUUIDs":["line-1"],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","title":"CVS","tagline1":{"text":"Subtotal: $2.50"},"tagline2":{"text":"Deliver to 222 E 39th St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.RemoveCartItem(context.Background(), "draft-1", "line-1")
	if err != nil {
		t.Fatalf("RemoveCartItem: %v", err)
	}
	if !result.Removed || result.Cart == nil || result.Cart.Ref != "draft-1" || result.Cart.ItemCount != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientRemoveCartItem_ErrorsWhenResponseDoesNotConfirmItem(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/removeItemsFromDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"shoppingCartItemUUIDs":["line-2"],"draftOrderUUID":"draft-1","draftOrderPresentationResponse":{}}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.RemoveCartItem(context.Background(), "draft-1", "line-1")
	if err == nil || !strings.Contains(err.Error(), "did not confirm removal") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientDiscardCart_UsesDiscardEndpoint(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1}]}}}`), nil
				case "/_p/api/discardDraftOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"discardedDraftOrderUUIDs":["draft-1"]}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.DiscardCart(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("DiscardCart: %v", err)
	}
	if !result.Discarded || result.Ref != "draft-1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestClientUpdateCart_UsesUpdateDraftOrder(t *testing.T) {
	var updatePayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"interactionType":"door_to_door",
							"paymentProfileUUID":"payment-1",
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.748198,"longitude":-73.9746683}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/updateDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read update draft body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &updatePayload); err != nil {
						t.Fatalf("unmarshal update draft body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"draftOrder":{"uuid":"draft-1","deliveryType":"PREMIUM_DELIVERY","interactionType":"leave_at_door"},"validationErrors":null}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.UpdateCart(context.Background(), "draft-1", CartUpdate{
		DeliveryType:    CartDeliveryTypePremium,
		InteractionType: "leave_at_door",
	})
	if err != nil {
		t.Fatalf("UpdateCart: %v", err)
	}
	if !result.Updated || result.Cart == nil || result.Cart.Ref != "draft-1" {
		t.Fatalf("result=%+v", result)
	}
	if updatePayload["deliveryType"] != "PREMIUM_DELIVERY" || updatePayload["interactionType"] != "leave_at_door" || updatePayload["paymentProfileUUID"] != "payment-1" {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
	if _, ok := updatePayload["deliveryAddress"].(map[string]any); !ok {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
}

func TestClientUpdateCart_PreservesExistingCartContext(t *testing.T) {
	var updatePayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"ASAP",
							"diningMode":"PICKUP",
							"interactionType":"door_to_door",
							"paymentProfileUUID":"payment-existing",
							"deliveryAddress":{"fullAddress":"Saved Address"},
							"targetDeliveryTimeRange":{"scheduled":true,"startTime":"2026-04-14T12:00:00Z","endTime":"2026-04-14T12:30:00Z"},
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.748198,"longitude":-73.9746683}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"name":"Personal","defaultPaymentProfileUuid":"payment-default"}}}`), nil
				case "/_p/api/updateDraftOrderV2":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read update draft body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &updatePayload); err != nil {
						t.Fatalf("unmarshal update draft body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"draftOrder":{"uuid":"draft-1","deliveryType":"ASAP","interactionType":"leave_at_door"},"validationErrors":null}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.UpdateCart(context.Background(), "draft-1", CartUpdate{
		InteractionType: "leave_at_door",
	})
	if err != nil {
		t.Fatalf("UpdateCart: %v", err)
	}
	if !result.Updated || result.Cart == nil || result.Cart.Ref != "draft-1" {
		t.Fatalf("result=%+v", result)
	}
	if updatePayload["paymentProfileUUID"] != "payment-existing" {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
	if updatePayload["diningMode"] != "PICKUP" {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
	deliveryAddress, _ := updatePayload["deliveryAddress"].(map[string]any)
	if deliveryAddress["fullAddress"] != "Saved Address" {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
	targetDeliveryTimeRange, _ := updatePayload["targetDeliveryTimeRange"].(map[string]any)
	if scheduled, _ := targetDeliveryTimeRange["scheduled"].(bool); !scheduled {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
	if updatePayload["interactionType"] != "leave_at_door" {
		t.Fatalf("updatePayload=%v", updatePayload)
	}
}

func TestClientUpdateCart_ErrorsWhenValidationErrorsAreReturned(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"CVS","itemCount":1}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","state":"UNORDERED","storeUuid":"store-1","deliveryType":"ASAP","interactionType":"door_to_door","paymentProfileUUID":"payment-1","shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.748198,"longitude":-73.9746683}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/updateDraftOrderV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"draftOrder":{"uuid":"draft-1","deliveryType":"ASAP","interactionType":"door_to_door"},"validationErrors":[{"code":"bad_request"}]}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.UpdateCart(context.Background(), "draft-1", CartUpdate{DeliveryType: CartDeliveryTypePremium})
	if err == nil || !strings.Contains(err.Error(), "validation errors") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientGetCheckoutPreview_UsesCheckoutPresentation(t *testing.T) {
	var checkoutPayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Rosa Mexicano","tagline1":{"text":"Subtotal: $25.30"},"tagline2":{"text":"Deliver to 222 E 39th St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"PREMIUM_DELIVERY",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"deliveryAddress":{"fullAddress":"222 E 39th St"},
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.748198,"longitude":-73.9746683}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/getCheckoutPresentationV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read checkout body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &checkoutPayload); err != nil {
						t.Fatalf("unmarshal checkout body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"draftOrderUUID":"draft-1",
							"checkoutPayloads":{
								"subtotal":{"subtotal":{"formattedValue":"$25.30"}},
								"total":{"total":{"formattedValue":"$30.59"}},
								"eta":{"rangeText":"8:51–9:03 PM"},
								"fareBreakdown":{
									"charges":[
										{"title":{"text":"Subtotal"},"value":{"text":"$25.30"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":2530000,"currencyCode":"USD"}}]}},
										{"title":{"text":"Delivery Fee"},"value":{"text":"$0.49"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":49000,"currencyCode":"USD"}}]}},
										{"title":{"text":"Service Fee"},"value":{"text":"$3.26"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":326000,"currencyCode":"USD"}}]}},
										{"title":{"text":"Taxes"},"value":{"text":"$1.54"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":154000,"currencyCode":"USD"}}]}}
									]
								}
							},
							"validationErrors":null
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	preview, err := client.GetCheckoutPreview(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("GetCheckoutPreview: %v", err)
	}
	if preview.Ref != "draft-1" || preview.Cart.Ref != "draft-1" {
		t.Fatalf("preview=%+v", preview)
	}
	if preview.Subtotal != "$25.30" || preview.Total != "$30.59" || preview.Fees != "$3.75" || preview.Taxes != "$1.54" || preview.Tip != "not set" {
		t.Fatalf("preview=%+v", preview)
	}
	if preview.ETA != "8:51–9:03 PM" || preview.Cart.SessionInfo.Profile != "Personal" {
		t.Fatalf("preview=%+v", preview)
	}
	payloadTypes, _ := checkoutPayload["payloadTypes"].([]any)
	if len(payloadTypes) == 0 || checkoutPayload["draftOrderUUID"] != "draft-1" {
		t.Fatalf("checkoutPayload=%v", checkoutPayload)
	}
}

func TestClientGetCheckoutPreview_UsesDisplayFallbacksWithoutAnalytics(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Cafe","tagline1":{"text":"Subtotal: €12.30"},"tagline2":{"text":"Deliver to 123 Main St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","state":"UNORDERED","storeUuid":"store-1","deliveryType":"PREMIUM_DELIVERY","interactionType":"leave_at_door","paymentProfileUUID":"payment-1","deliveryAddress":{"fullAddress":"123 Main St"},"shoppingCart":{"cartUuid":"cart-1","currencyCode":"EUR","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Latte","quantity":1,"price":1230}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"123 Main St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/getCheckoutPresentationV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"draftOrderUUID":"draft-1",
							"checkoutPayloads":{
								"subtotal":{"subtotal":{"formattedValue":"€12.30"}},
								"total":{"total":{"formattedValue":"€16.80"}},
								"fareBreakdown":{
									"charges":[
										{"title":{"text":"Subtotal"},"value":{"text":"€12.30"}},
										{"title":{"text":"Service Fee"},"value":{"text":"€3.00"}},
										{"title":{"text":"Taxes"},"value":{"text":"€1.50"}}
									]
								}
							},
							"validationErrors":null
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	preview, err := client.GetCheckoutPreview(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("GetCheckoutPreview: %v", err)
	}
	if preview.Fees != "€3.00" || preview.Taxes != "€1.50" || preview.Tip != "not set" {
		t.Fatalf("preview=%+v", preview)
	}
	if preview.ETA != "" {
		t.Fatalf("expected empty eta, got %+v", preview)
	}
}

func TestClientGetCheckoutPreview_ReportsExplicitZeroTip(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Cafe","tagline1":{"text":"Subtotal: $12.30"},"tagline2":{"text":"Deliver to 123 Main St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","state":"UNORDERED","storeUuid":"store-1","deliveryType":"ASAP","interactionType":"leave_at_door","paymentProfileUUID":"payment-1","deliveryAddress":{"fullAddress":"123 Main St"},"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Latte","quantity":1,"price":1230}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"123 Main St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/getCheckoutPresentationV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"draftOrderUUID":"draft-1",
							"checkoutPayloads":{
								"fareBreakdown":{
									"charges":[
										{"title":{"text":"Subtotal"},"value":{"text":"$12.30"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":1230000,"currencyCode":"USD"}}]}},
										{"title":{"text":"Tip"},"value":{"text":"$0.00"},"fareBreakdownChargeMetadata":{"analyticsInfo":[{"currencyAmount":{"amountE5":0,"currencyCode":"USD"}}]}}
									]
								}
							},
							"validationErrors":null
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	preview, err := client.GetCheckoutPreview(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("GetCheckoutPreview: %v", err)
	}
	if preview.Tip != "$0.00" {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestClientCheckoutCart_UsesCheckoutEndpoint(t *testing.T) {
	var checkoutPayload map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Rosa Mexicano","tagline1":{"text":"Subtotal: $25.30"},"tagline2":{"text":"Deliver to 222 E 39th St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"draft-1",
							"state":"UNORDERED",
							"storeUuid":"store-1",
							"deliveryType":"PREMIUM_DELIVERY",
							"interactionType":"leave_at_door",
							"paymentProfileUUID":"payment-1",
							"deliveryAddress":{"fullAddress":"222 E 39th St"},
							"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1,"price":399}]}
						}
					}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.748198,"longitude":-73.9746683}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/checkoutOrdersByDraftOrdersV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read checkout confirm body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &checkoutPayload); err != nil {
						t.Fatalf("unmarshal checkout confirm body: %v body=%s", err, string(rawBody))
					}
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"orders":[{
								"uuid":"order-1",
								"orderInfo":{"storeInfo":{"name":"Rosa Mexicano"}},
								"activeOrderOverview":{"subtitle":"3 items for $69.26","items":[{"title":"Taquitos","quantity":1}]},
								"activeOrderStatus":{"currentProgress":1},
								"status":"ACTIVE"
							}],
							"paymentProviderConfirmationUrl":"https://payments.example/confirm"
						}
					}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.CheckoutCart(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("CheckoutCart: %v", err)
	}
	if result.Ref != "draft-1" || result.Order.UUID != "order-1" || result.PaymentProviderConfirmationURL != "https://payments.example/confirm" {
		t.Fatalf("result=%+v", result)
	}
	if checkoutPayload["draftOrderUUID"] != "draft-1" || checkoutPayload["paymentProfileUuid"] != "payment-1" {
		t.Fatalf("checkoutPayload=%v", checkoutPayload)
	}
	extraParams, _ := checkoutPayload["extraParams"].(map[string]any)
	if strings.TrimSpace(stringValue(extraParams["timezone"])) == "" {
		t.Fatalf("checkoutPayload=%v", checkoutPayload)
	}
}

func TestClientCheckoutCart_ErrorsWhenValidationErrorsReturned(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Cafe","tagline1":{"text":"Subtotal: $12.30"},"tagline2":{"text":"Deliver to 123 Main St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","state":"UNORDERED","storeUuid":"store-1","deliveryType":"ASAP","interactionType":"leave_at_door","paymentProfileUUID":"payment-1","deliveryAddress":{"fullAddress":"123 Main St"},"shoppingCart":{"cartUuid":"cart-1","currencyCode":"USD","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Latte","quantity":1,"price":1230}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"123 Main St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"selectedProfile":{"uuid":"profile-1","name":"Personal","defaultPaymentProfileUuid":"payment-1"}}}`), nil
				case "/_p/api/checkoutOrdersByDraftOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"orders":[],"validationErrors":[{"code":"card_declined"}]}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.CheckoutCart(context.Background(), "draft-1")
	if err == nil || !strings.Contains(err.Error(), "validation errors") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientCheckoutCart_UsesCartPaymentProfileWhenProfileLookupFails(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getCartsViewForEaterUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"cartsView":{"carts":[{"draftOrderUUID":"draft-1","cartUUID":"cart-1","title":"Cafe","tagline1":{"text":"Subtotal: €12.30"},"tagline2":{"text":"Deliver to 123 Main St"},"itemCount":1,"action":"OPEN_CHECKOUT"}]}}}`), nil
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","state":"UNORDERED","storeUuid":"store-1","deliveryType":"ASAP","interactionType":"leave_at_door","paymentProfileUUID":"payment-cart","deliveryAddress":{"fullAddress":"123 Main St"},"shoppingCart":{"cartUuid":"cart-1","currencyCode":"EUR","isActive":true,"items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Latte","quantity":1,"price":1230}]}}}`), nil
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"123 Main St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getProfilesForUserV1":
					return jsonResponse(t, 500, `{"status":"failure"}`), nil
				case "/_p/api/checkoutOrdersByDraftOrdersV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read checkout confirm body: %v", err)
					}
					var payload map[string]any
					if err := json.Unmarshal(rawBody, &payload); err != nil {
						t.Fatalf("unmarshal checkout confirm body: %v body=%s", err, string(rawBody))
					}
					if payload["paymentProfileUuid"] != "payment-cart" {
						t.Fatalf("payload=%v", payload)
					}
					return jsonResponse(t, 200, `{"status":"success","data":{"orders":[{"uuid":"order-1","orderInfo":{"storeInfo":{"name":"Cafe"}},"activeOrderOverview":{"subtitle":"1 item for €16.80","items":[{"title":"Latte","quantity":1}]},"status":"ACTIVE"}]}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	result, err := client.CheckoutCart(context.Background(), "draft-1")
	if err != nil {
		t.Fatalf("CheckoutCart: %v", err)
	}
	if result.Order.Total != "€16.80" {
		t.Fatalf("result=%+v", result)
	}
}

func TestExtractMoneyString_PreservesNonUSDCurrency(t *testing.T) {
	if got := extractMoneyString("1 item for €16.80"); got != "€16.80" {
		t.Fatalf("got=%q", got)
	}
}

func TestClientDiscardCart_ErrorsWhenDraftWasNotDiscarded(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDraftOrderByUuidV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"uuid":"draft-1","storeUuid":"store-1","shoppingCart":{"cartUuid":"cart-1","items":[{"shoppingCartItemUuid":"line-1","uuid":"item-1","title":"Gummy Bears","quantity":1}]}}}`), nil
				case "/_p/api/discardDraftOrdersV1":
					return jsonResponse(t, 200, `{"status":"success","data":{"discardedDraftOrderUUIDs":[]}}`), nil
				default:
					t.Fatalf("unexpected request %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	_, err := client.DiscardCart(context.Background(), "draft-1")
	if err == nil || !strings.Contains(err.Error(), "did not discard") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientCheckSession_WrapsBrowserRefreshErrors(t *testing.T) {
	client := &Client{
		BaseURL:    "https://www.ubereats.com",
		ProfileDir: "/tmp/ubereats-profile",
		ReadSession: func(context.Context, string, browserpage.Options) (browserpage.SessionResult, error) {
			return browserpage.SessionResult{}, context.DeadlineExceeded
		},
	}

	_, err := client.CheckSession(context.Background())
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "run `ordercli ubereats login`") {
		t.Fatalf("unexpected err=%v", err)
	}
}

func TestClientTraceRequest_RedactsSensitiveJSONFields(t *testing.T) {
	var logBuf bytes.Buffer
	client := &Client{LogWriter: &logBuf}

	client.traceRequest(
		"https://www.ubereats.com/_p/api/getDeliveryLocationsV2",
		[]byte(`{"location":{"fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}},"userQuery":"pizza"}`),
		http.Header{
			"Cookie":                           []string{"sid=abc"},
			"X-Csrf-Token":                     []string{"x"},
			"X-Uber-Device-Location-Latitude":  []string{"40.7"},
			"X-Uber-Device-Location-Longitude": []string{"-73.9"},
		},
		http.StatusOK,
		`{"data":{"firstName":"Prateek","deliveryLocations":{"TARGET":[{"location":{"fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`,
	)

	logged := logBuf.String()
	for _, forbidden := range []string{"222 E 39th St", "40.7", "-73.9", "Prateek", "sid=abc"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("unexpected sensitive value %q in log=%s", forbidden, logged)
		}
	}
	for _, want := range []string{"ubereats request method=POST", `"userQuery":"pizza"`, `"location":"REDACTED"`, `"firstName":"REDACTED"`, `X-Uber-Device-Location-Latitude:REDACTED`} {
		if !strings.Contains(logged, want) {
			t.Fatalf("missing %q in log=%s", want, logged)
		}
	}
}

func TestClientTraceRequest_RedactsNonJSONBodies(t *testing.T) {
	var logBuf bytes.Buffer
	client := &Client{LogWriter: &logBuf}

	client.traceRequest(
		"https://www.ubereats.com/_p/api/getUserV1",
		[]byte(`not-json-secret`),
		http.Header{},
		http.StatusBadGateway,
		`<html>secret-response</html>`,
	)

	logged := logBuf.String()
	for _, forbidden := range []string{"not-json-secret", "secret-response"} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("unexpected sensitive value %q in log=%s", forbidden, logged)
		}
	}
	for _, want := range []string{"<non-json 15 bytes>", "<non-json 28 bytes>"} {
		if !strings.Contains(logged, want) {
			t.Fatalf("missing %q in log=%s", want, logged)
		}
	}
}

func TestClientMergeResponseCookies_RemovesExpiredCookies(t *testing.T) {
	client := &Client{}
	client.SetCookieHeader("sid=old; auth=xyz")

	resp := &http.Response{
		Header: http.Header{
			"Set-Cookie": []string{
				"sid=deleted; Path=/; Max-Age=0",
				"auth=deleted; Path=/; Max-Age=0",
			},
		},
	}
	client.mergeResponseCookies(resp)

	if got := client.currentCookieHeader(); got != "" {
		t.Fatalf("cookie_header=%q", got)
	}
	if client.hasSessionCookies() {
		t.Fatalf("expected empty session cookies")
	}
}

func TestClientSetCookieHeader_ReplacesExistingCookieState(t *testing.T) {
	client := &Client{}
	client.SetCookieHeader("sid=old; auth=xyz")
	client.mergeResponseCookies(&http.Response{
		Header: http.Header{
			"Set-Cookie": []string{"pref=abc; Path=/"},
		},
	})

	client.SetCookieHeader("sid=new")

	if got := client.currentCookieHeader(); got != "sid=new" {
		t.Fatalf("cookie_header=%q", got)
	}
}

func TestClientHasSessionCookies_IgnoresNonAuthCookies(t *testing.T) {
	client := &Client{}
	client.SetCookieHeader("uev2.loc=abc; u-cookie-prefs=xyz")

	if client.hasSessionCookies() {
		t.Fatalf("expected non-auth cookies to be ignored")
	}

	client.SetCookieHeader("sid=abc; uev2.loc=abc")
	if !client.hasSessionCookies() {
		t.Fatalf("expected auth cookie to count as session")
	}
}
