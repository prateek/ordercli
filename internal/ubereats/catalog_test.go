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
