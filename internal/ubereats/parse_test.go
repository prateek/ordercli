package ubereats

import (
	"strings"
	"testing"

	"github.com/steipete/ordercli/internal/browserpage"
)

func TestParsePage_ExtractsOrdersFromCapturedResponses(t *testing.T) {
	res := browserpage.Result{
		FinalURL: "https://www.ubereats.com/orders/",
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
									"shoppingCart": {
										"items": [
											{"title": "ShackBurger", "quantity": 1},
											{"name": "Fries", "quantity": 2}
										]
									},
									"deliveryStateChanges": [
										{"stateChangeTime": "2026-04-11T16:05:00Z", "type": "DISPATCHED"}
									]
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
									"isCompleted":true,
									"completedAt":"2026-04-12T18:00:00Z",
									"currencyCode":"USD",
									"shoppingCart":{"items":[{"title":"Bowl","quantity":1}]},
									"deliveryStateChanges":[
										{"stateChangeTime":"2026-04-12T17:20:00Z","type":"DISPATCHED"},
										{"stateChangeTime":"2026-04-12T18:00:00Z","type":"COMPLETED"}
									]
								},
								"storeInfo":{"title":"Chipotle"},
								"fareInfo":{"checkoutInfo":[{"label":"Total","key":"eats_fare.total","rawValue":18.50}]}
							},
							"past-2":{
								"baseEaterOrder":{
									"uuid":"past-2",
									"isCompleted":true,
									"completedAt":"2026-04-10T18:00:00Z",
									"currencyCode":"USD",
									"shoppingCart":{"items":[{"title":"Salad","quantity":1}]},
									"deliveryStateChanges":[
										{"stateChangeTime":"2026-04-10T17:45:00Z","type":"DISPATCHED"},
										{"stateChangeTime":"2026-04-10T18:00:00Z","type":"COMPLETED"}
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
	}

	page := ParsePage(res)
	if len(page.Orders) != 3 {
		t.Fatalf("orders=%+v", page.Orders)
	}

	latestPast := page.Orders[0]
	if latestPast.UUID != "past-1" || latestPast.Merchant != "Chipotle" || latestPast.Status != "Completed" || latestPast.Active {
		t.Fatalf("latest_past=%+v", latestPast)
	}

	active := page.Orders[1]
	if active.UUID != "active-1" || active.Merchant != "Shake Shack" || active.OrderNumber != "A-101" {
		t.Fatalf("active=%+v", active)
	}
	if active.Status != "Preparing your order" {
		t.Fatalf("status=%q", active.Status)
	}
	if active.StatusDetail != "Courier is on the way to the store" {
		t.Fatalf("detail=%q", active.StatusDetail)
	}
	if active.ETA != "15-25 min" || !active.Active {
		t.Fatalf("active eta/flag=%+v", active)
	}
	if got := strings.Join(active.Items, ", "); got != "1x ShackBurger, 2x Fries" {
		t.Fatalf("items=%q", got)
	}
	if active.Total != "$24.90" {
		t.Fatalf("total=%q", active.Total)
	}

	olderPast := page.Orders[2]
	if olderPast.UUID != "past-2" || olderPast.Merchant != "Sweetgreen" || olderPast.Status != "Completed" || olderPast.Active {
		t.Fatalf("older_past=%+v", olderPast)
	}
}

func TestParsePage_ExtractsOrdersFromPastOrdersEndpoint(t *testing.T) {
	res := browserpage.Result{
		FinalURL: "https://www.ubereats.com/orders/",
		Responses: []browserpage.CapturedResponse{
			{
				URL:         "https://www.ubereats.com/_p/api/getPastOrdersV1",
				Status:      200,
				ContentType: "application/json",
				Body: `{
					"status":"success",
					"data":{
						"ordersMap":{
							"uuid-1":{
								"baseEaterOrder":{
									"uuid":"uuid-1",
									"isCancelled":false,
									"isCompleted":true,
									"completedAt":"2026-04-10T18:00:00Z",
									"currencyCode":"USD",
									"shoppingCart":{
										"items":[
											{"title":"Popcorn","quantity":2},
											{"title":"Candy","quantity":1}
										]
									}
								},
								"storeInfo":{
									"title":"CVS",
									"location":{"address":{"eaterFormattedAddress":"150 East 42nd Street, New York, NY 10017"}}
								},
								"courierInfo":{"name":"Taylor"},
								"fareInfo":{
									"checkoutInfo":[
										{"label":"Subtotal","rawValue":59.69},
										{"label":"Total","key":"eats_fare.total","rawValue":65.51}
									]
								},
								"interactionType":"leave_at_door"
							}
						}
					}
				}`,
			},
		},
	}

	page := ParsePage(res)
	if len(page.Orders) != 1 {
		t.Fatalf("orders=%+v", page.Orders)
	}
	order := page.Orders[0]
	if order.UUID != "uuid-1" || order.Merchant != "CVS" {
		t.Fatalf("order=%+v", order)
	}
	if order.Status != "Completed" || order.Active {
		t.Fatalf("status=%+v", order)
	}
	if order.Total != "$65.51" {
		t.Fatalf("total=%q", order.Total)
	}
	if order.Courier != "Taylor" {
		t.Fatalf("courier=%q", order.Courier)
	}
	if order.StoreAddress != "150 East 42nd Street, New York, NY 10017" {
		t.Fatalf("store_address=%q", order.StoreAddress)
	}
	if got := strings.Join(order.Items, ", "); got != "2x Popcorn, 1x Candy" {
		t.Fatalf("items=%q", got)
	}
}

func TestParsePage_FiltersDuplicateOrders(t *testing.T) {
	res := browserpage.Result{
		FinalURL: "https://www.ubereats.com/orders/",
		Responses: []browserpage.CapturedResponse{
			{
				URL:         "https://www.ubereats.com/_p/api/getPastOrdersV1",
				Status:      200,
				ContentType: "application/json",
				Body:        `{"status":"success","data":{"ordersMap":{"dupe-1":{"baseEaterOrder":{"uuid":"dupe-1","isCompleted":true,"completedAt":"2026-04-10T18:00:00Z"},"storeInfo":{"title":"Store A"}}}}}`,
			},
			{
				URL:         "https://www.ubereats.com/_p/api/getPastOrdersV1",
				Status:      200,
				ContentType: "application/json",
				Body:        `{"status":"success","data":{"ordersMap":{"dupe-1":{"baseEaterOrder":{"uuid":"dupe-1","isCompleted":true,"completedAt":"2026-04-10T18:00:00Z"},"storeInfo":{"title":"Store A"}}}}}`,
			},
		},
	}

	page := ParsePage(res)
	if len(page.Orders) != 1 {
		t.Fatalf("orders=%+v", page.Orders)
	}
}

func TestBuildOrderURL(t *testing.T) {
	got, err := BuildOrderURL("https://www.ubereats.com", "abc-123")
	if err != nil {
		t.Fatalf("BuildOrderURL uuid: %v", err)
	}
	if got != "https://www.ubereats.com/orders/abc-123" {
		t.Fatalf("got=%q", got)
	}

	got, err = BuildOrderURL("https://www.ubereats.com", "https://www.ubereats.com/en-US/orders/abc-123/receipt/")
	if err != nil {
		t.Fatalf("BuildOrderURL url: %v", err)
	}
	if got != "https://www.ubereats.com/en-US/orders/abc-123/receipt/" {
		t.Fatalf("got=%q", got)
	}
}
