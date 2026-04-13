package ubereats

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestClientGetStoreMenu_AndSearchStoreItems(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"title":"Rosa Mexicano",
							"uuid":"store-1",
							"currencyCode":"USD",
							"isOrderable":true,
							"isFavorite":true,
							"rating":4.8,
							"etaRange":{"min":20,"max":35},
							"fareInfo":{"serviceFee":249},
							"sections":[
								{"uuid":"section-1","title":"Menu","subtitle":"Open","subsectionUuids":["sub-1"]}
							],
							"catalogSectionsMap":{
								"section-1":[
									{
										"payload":{
											"standardItemsPayload":{
												"catalogItems":[
													{
														"uuid":"item-1",
														"title":"Chicken Enchiladas",
														"itemDescription":"Tomatillo salsa verde",
														"price":3105,
														"sectionUuid":"section-1",
														"subsectionUuid":"sub-1",
														"isSoldOut":false,
														"hasCustomizations":true
													},
													{
														"uuid":"item-1",
														"title":"Chicken Enchiladas",
														"itemDescription":"Tomatillo salsa verde",
														"price":3105,
														"sectionUuid":"section-1",
														"subsectionUuid":"sub-1",
														"isSoldOut":false,
														"hasCustomizations":true
													},
													{
														"uuid":"item-2",
														"title":"Ahi Tuna Taquitos",
														"itemDescription":"Soy-lime marinade",
														"price":2530,
														"sectionUuid":"section-1",
														"subsectionUuid":"sub-1",
														"isSoldOut":true,
														"hasCustomizations":false
													}
												]
											}
										}
									}
								]
							}
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	menu, err := client.GetStoreMenu(context.Background(), "store-1")
	if err != nil {
		t.Fatalf("GetStoreMenu: %v", err)
	}
	if menu.Store.Ref != "store-1" || menu.Store.Title != "Rosa Mexicano" || len(menu.Sections) != 1 {
		t.Fatalf("menu=%+v", menu)
	}
	if len(menu.Sections[0].Items) != 2 {
		t.Fatalf("section=%+v", menu.Sections[0])
	}

	items, err := client.SearchStoreItems(context.Background(), "store-1", "enchi", 10)
	if err != nil {
		t.Fatalf("SearchStoreItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Ref != "item-1" || items[0].StoreRef != "store-1" || items[0].SectionRef != "section-1" {
		t.Fatalf("item=%+v", items[0])
	}
}

func TestClientGetMenuItem_ResolvesSectionMetadataFromStore(t *testing.T) {
	var menuItemRequest map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getStoreV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"title":"Rosa Mexicano",
							"uuid":"store-1",
							"currencyCode":"USD",
							"sections":[{"uuid":"section-1","title":"Menu","subsectionUuids":["sub-1"]}],
							"catalogSectionsMap":{
								"section-1":[
									{
										"payload":{
											"standardItemsPayload":{
												"catalogItems":[
													{
														"uuid":"item-1",
														"title":"Ahi Tuna Taquitos",
														"itemDescription":"Soy-lime marinade",
														"price":2530,
														"sectionUuid":"section-1",
														"subsectionUuid":"sub-1",
														"isSoldOut":false,
														"hasCustomizations":true
													}
												]
											}
										}
									}
								]
							}
						}
					}`), nil
				case "/_p/api/getMenuItemV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read body: %v", err)
					}
					if err := json.Unmarshal(rawBody, &menuItemRequest); err != nil {
						t.Fatalf("unmarshal body: %v", err)
					}
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"uuid":"item-1",
							"title":"Ahi Tuna Taquitos",
							"itemDescription":"Soy-lime marinade",
							"price":2530,
							"sectionUuid":"section-1",
							"subsectionUuid":"sub-1",
							"isSoldOut":false,
							"hasCustomizations":true,
							"customizationsList":[
								{
									"uuid":"group-1",
									"title":"Add Dips",
									"minPermitted":0,
									"maxPermitted":2,
									"options":[
										{"uuid":"option-1","title":"Caesar Dressing","price":200,"isSoldOut":false}
									]
								}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	item, err := client.GetMenuItem(context.Background(), "store-1", "item-1")
	if err != nil {
		t.Fatalf("GetMenuItem: %v", err)
	}
	if menuItemRequest["sectionUuid"] != "section-1" || menuItemRequest["subsectionUuid"] != "sub-1" || menuItemRequest["menuItemUuid"] != "item-1" {
		t.Fatalf("request=%+v", menuItemRequest)
	}
	if item.Ref != "item-1" || len(item.Customizations) != 1 || item.Customizations[0].Options[0].Ref != "option-1" {
		t.Fatalf("item=%+v", item)
	}
}

func TestClientSearchItems_ParsesMixedSearchFeed(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getSearchFeedV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"currencyCode":"USD",
							"storesMap":{
								"store-1":{"uuid":"store-1","title":"CVS"}
							},
							"feedItems":[
								{
									"type":"MINI_STORE_WITH_ITEMS",
									"storeUuid":"store-1",
									"payload":{
										"catalogItems":[
											{
												"uuid":"item-1",
												"title":"Gummy Bears",
												"itemDescription":"Haribo",
												"price":299,
												"sectionUuid":"section-1",
												"subsectionUuid":"sub-1",
												"hasCustomizations":false
											}
										]
									}
								}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	items, err := client.SearchItems(context.Background(), "gummy bears", 10)
	if err != nil {
		t.Fatalf("SearchItems: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items=%+v", items)
	}
	if items[0].Ref != "item-1" || items[0].StoreRef != "store-1" || items[0].StoreTitle != "CVS" || items[0].PriceMinor != 299 {
		t.Fatalf("item=%+v", items[0])
	}
}

func TestClientGetStoreMenu_FailsWhenDefaultLocationUnavailable(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 500, `{"status":"failure"}`), nil
				case "/_p/api/getStoreV1":
					t.Fatalf("getStoreV1 should not be called when location lookup fails")
				}
				return nil, nil
			}),
		},
	}

	if _, err := client.GetStoreMenu(context.Background(), "store-1"); err == nil {
		t.Fatalf("expected GetStoreMenu to fail when location lookup fails")
	}
}

func TestClientSearchItems_FailsWhenDefaultLocationUnavailable(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 500, `{"status":"failure"}`), nil
				case "/_p/api/getSearchFeedV1":
					t.Fatalf("getSearchFeedV1 should not be called when location lookup fails")
				}
				return nil, nil
			}),
		},
	}

	if _, err := client.SearchItems(context.Background(), "gummy", 10); err == nil {
		t.Fatalf("expected SearchItems to fail when location lookup fails")
	}
}

func TestClientSearchStores_UsesSearchFeed(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getSearchFeedV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"storesMap":{
								"store-1":{"uuid":"store-1","title":"CVS","isOrderable":true,"isFavorite":true,"rating":4.7,"etaRange":{"min":15,"max":25},"fareInfo":{"serviceFee":0},"currencyCode":"USD"},
								"store-2":{"uuid":"store-2","title":"Walgreens","isOrderable":false,"isFavorite":false,"rating":4.3,"etaRange":{"min":20,"max":35},"fareInfo":{"serviceFee":199},"currencyCode":"USD"}
							},
							"feedItems":[
								{"type":"MINI_STORE","storeUuid":"store-1"},
								{"type":"MINI_STORE_WITH_ITEMS","storeUuid":"store-2"}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	stores, err := client.SearchStores(context.Background(), "pharmacy", 10)
	if err != nil {
		t.Fatalf("SearchStores: %v", err)
	}
	if len(stores) != 2 {
		t.Fatalf("stores=%+v", stores)
	}
	if stores[0].Ref != "store-1" || stores[0].Title != "CVS" {
		t.Fatalf("store[0]=%+v", stores[0])
	}
	if stores[1].Ref != "store-2" || stores[1].Orderable {
		t.Fatalf("store[1]=%+v", stores[1])
	}
}

func TestClientSearchStores_ParsesNestedFeedStoreCards(t *testing.T) {
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getSearchFeedV1":
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"feedItems":[
								{"type":"SECTION_HEADER","uuid":"header-1","title":{"text":"Top result"}},
								{"type":"REGULAR_STORE","store":{
									"storeUuid":"store-1",
									"title":{"text":"CVS (150 East 42Nd St.)"},
									"favorite":false,
									"rating":{"text":"4.8"},
									"meta":[{"badgeType":"ETD","text":"19 min"}],
									"tracking":{"storePayload":{"isOrderable":true,"ratingInfo":{"ratingCount":"1,000+"},"fareInfo":{"actualServiceFee":{"low":0}}}}
								}},
								{"type":"CAROUSEL","carousel":{"stores":[
									{
										"storeUuid":"store-2",
										"title":{"text":"Walgreens"},
										"favorite":true,
										"meta":[{"badgeType":"ETD","text":"22 min"}],
										"tracking":{"storePayload":{"isOrderable":true,"ratingInfo":{"ratingCount":"900+"},"fareInfo":{"actualServiceFee":{"low":249}}}}
									}
								]}}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	stores, err := client.SearchStores(context.Background(), "pharmacy", 10)
	if err != nil {
		t.Fatalf("SearchStores: %v", err)
	}
	if len(stores) != 2 {
		t.Fatalf("stores=%+v", stores)
	}
	if stores[0].Ref != "store-1" || stores[0].Title != "CVS (150 East 42Nd St.)" || !stores[0].Orderable {
		t.Fatalf("store[0]=%+v", stores[0])
	}
	if stores[1].Ref != "store-2" || !stores[1].Favorite || stores[1].FeeDisplay == "" {
		t.Fatalf("store[1]=%+v", stores[1])
	}
}

func TestClientListStores_FiltersFavorites(t *testing.T) {
	var requestBodies []map[string]any
	client := &Client{
		BaseURL:      "https://www.ubereats.com",
		CookieHeader: "sid=abc; auth=xyz",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/_p/api/getDeliveryLocationsV2":
					return jsonResponse(t, 200, `{"status":"success","data":{"deliveryLocations":{"TARGET":[{"location":{"id":"loc-1","fullAddress":"222 E 39th St","coordinate":{"latitude":40.7,"longitude":-73.9}}}]}}}`), nil
				case "/_p/api/getFeedV1":
					rawBody, err := io.ReadAll(req.Body)
					if err != nil {
						t.Fatalf("read body: %v", err)
					}
					var payload map[string]any
					if len(rawBody) > 0 {
						if err := json.Unmarshal(rawBody, &payload); err != nil {
							t.Fatalf("unmarshal body: %v body=%s", err, string(rawBody))
						}
					}
					requestBodies = append(requestBodies, payload)
					return jsonResponse(t, 200, `{
						"status":"success",
						"data":{
							"favorites":{"store-2":{}},
							"feedItems":[
								{"type":"REGULAR_STORE","store":{"storeUuid":"store-1","title":{"text":"CVS"},"favorite":false,"tracking":{"storePayload":{"isOrderable":true}}}},
								{"type":"FEATURED_STORES","carousel":{"stores":[
									{"storeUuid":"store-2","title":"Walgreens","favorite":false,"trackingCode":{"storePayload":{"isOrderable":true,"ratingInfo":{"ratingCount":"900+"},"fareInfo":{"actualServiceFee":{"low":249}}}}}
								]}}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			}),
		},
	}

	stores, err := client.ListStores(context.Background(), false, 10)
	if err != nil {
		t.Fatalf("ListStores: %v", err)
	}
	if len(stores) != 2 {
		t.Fatalf("stores=%+v", stores)
	}
	if !stores[1].Favorite {
		t.Fatalf("expected favorites map to mark Walgreens as favorite, got %+v", stores[1])
	}

	favorites, err := client.ListStores(context.Background(), true, 10)
	if err != nil {
		t.Fatalf("ListStores favorites: %v", err)
	}
	if len(favorites) != 1 || favorites[0].Ref != "store-2" || !favorites[0].Favorite {
		t.Fatalf("favorites=%+v", favorites)
	}
	if len(requestBodies) != 2 {
		t.Fatalf("requestBodies=%v", requestBodies)
	}
	if len(requestBodies[0]) != 0 {
		t.Fatalf("default payload=%v", requestBodies[0])
	}
	if got, ok := requestBodies[1]["storeFilters"].([]any); !ok || len(got) != 1 || got[0] != "FAVORITES" {
		t.Fatalf("favorites payload=%v", requestBodies[1])
	}
}
