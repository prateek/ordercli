package ubereats

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steipete/ordercli/internal/browserpage"
)

type Order struct {
	UUID         string   `json:"uuid,omitempty"`
	URL          string   `json:"url,omitempty"`
	OrderNumber  string   `json:"order_number,omitempty"`
	Merchant     string   `json:"merchant,omitempty"`
	Status       string   `json:"status,omitempty"`
	StatusDetail string   `json:"status_detail,omitempty"`
	ETA          string   `json:"eta,omitempty"`
	Total        string   `json:"total,omitempty"`
	Courier      string   `json:"courier,omitempty"`
	StoreAddress string   `json:"store_address,omitempty"`
	Items        []string `json:"items,omitempty"`
	Active       bool     `json:"active"`
	occurredAt   time.Time
}

type Page struct {
	FinalURL string  `json:"final_url,omitempty"`
	Title    string  `json:"title,omitempty"`
	Orders   []Order `json:"orders,omitempty"`
	RawText  string  `json:"raw_text,omitempty"`
}

func ParsePage(res browserpage.Result) Page {
	page := Page{
		FinalURL: res.FinalURL,
		Title:    res.Title,
		RawText:  res.Text,
	}

	var orders []Order
	for _, captured := range res.Responses {
		if strings.TrimSpace(captured.Body) == "" {
			continue
		}
		var payload any
		if err := json.Unmarshal([]byte(captured.Body), &payload); err != nil {
			continue
		}
		walkJSON(payload, func(m map[string]any) {
			if order, ok := orderFromMap(m, res.FinalURL); ok {
				orders = append(orders, order)
			}
		})
	}

	page.Orders = dedupeOrders(orders)
	sortOrdersByOccurredAt(page.Orders)
	return page
}

func BuildOrderURL(baseURL, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("missing order reference")
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		u, err := url.Parse(ref)
		if err != nil {
			return "", err
		}
		if u.Scheme == "" || u.Host == "" {
			return "", fmt.Errorf("invalid order url %q", ref)
		}
		return u.String(), nil
	}

	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://www.ubereats.com"
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid base url %q", baseURL)
	}
	ref = strings.Trim(ref, "/")
	u.Path = "/orders/" + ref
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func walkJSON(v any, visit func(map[string]any)) {
	switch typed := v.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkJSON(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkJSON(child, visit)
		}
	}
}

func orderFromMap(m map[string]any, finalURL string) (Order, bool) {
	order := Order{
		UUID:         firstStringValue(m, "orderUuid", "uuid", "order_id", "id"),
		OrderNumber:  firstStringValue(m, "orderNumber", "displayOrderId", "humanReadableId"),
		Merchant:     firstNonEmpty(namedValue(m, "storeInfo"), namedValue(m, "store", "merchant", "restaurant")),
		Status:       firstNonEmpty(statusValue(m), firstStringValue(m, "statusTitle", "statusText")),
		StatusDetail: nestedNamedValue(m, "currentStatus", "description", "detail", "subtitle"),
		ETA:          firstNonEmpty(nestedNamedValue(m, "etaRange", "label"), nestedNamedValue(m, "deliveryTime", "displayString"), firstStringValue(m, "eta")),
		Total:        firstNonEmpty(displayValue(m["total"]), fareTotal(m)),
		Courier:      firstNonEmpty(namedValue(m, "courierInfo"), namedValue(m, "courier", "driver")),
		StoreAddress: locationAddress(m),
		Items:        firstNonEmptyItems(itemLines(m), shoppingCartItems(m)),
		occurredAt:   orderOccurredAt(m),
	}
	if baseOrder, ok := m["baseEaterOrder"].(map[string]any); ok {
		applyBaseOrderFields(&order, baseOrder)
	}
	if order.UUID == "" {
		order.UUID = uuidFromOrderURL(finalURL)
	}
	if order.URL == "" && order.UUID != "" && finalURL != "" {
		if builtURL, err := BuildOrderURL(finalURL, order.UUID); err == nil {
			order.URL = builtURL
		}
	}
	if order.Status == "" {
		order.Status = humanizeStatusCode(firstStringValue(m, "status", "state"))
	}
	if order.Merchant == "" && order.Status == "" && order.OrderNumber == "" && order.Total == "" {
		return Order{}, false
	}

	if orderScore(order) < 3 {
		return Order{}, false
	}

	order.Active = isActiveOrder(m, order.Status, order.StatusDetail)
	return order, true
}

func sortOrdersByOccurredAt(orders []Order) {
	sort.SliceStable(orders, func(i, j int) bool {
		return occurredAtAfter(orders[i].occurredAt, orders[j].occurredAt)
	})
}

func occurredAtAfter(left, right time.Time) bool {
	if left.Equal(right) {
		return false
	}
	if left.IsZero() {
		return false
	}
	if right.IsZero() {
		return true
	}
	return left.After(right)
}

func applyBaseOrderFields(order *Order, baseOrder map[string]any) {
	if order.UUID == "" {
		order.UUID = firstStringValue(baseOrder, "uuid")
	}
	if order.OrderNumber == "" {
		order.OrderNumber = firstStringValue(baseOrder, "displayName")
	}
	if order.Items == nil {
		order.Items = shoppingCartItems(baseOrder)
	}
	if order.Status == "" {
		order.Status = baseOrderStatus(baseOrder)
	}
	if order.occurredAt.IsZero() {
		order.occurredAt = orderOccurredAt(baseOrder)
	}
}

func orderScore(order Order) int {
	score := 0
	if order.UUID != "" {
		score += 2
	}
	for _, ok := range []bool{
		order.Merchant != "",
		order.Status != "",
		order.OrderNumber != "",
		len(order.Items) > 0,
		order.Total != "",
	} {
		if ok {
			score++
		}
	}
	return score
}

func dedupeOrders(in []Order) []Order {
	out := make([]Order, 0, len(in))
	seen := map[string]struct{}{}
	for _, order := range in {
		key := strings.ToLower(strings.Join([]string{
			order.UUID,
			order.OrderNumber,
			order.Merchant,
			order.Status,
		}, "|"))
		if key == "|||" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, order)
	}
	return out
}

func firstStringValue(m map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		if got := displayValue(value); got != "" {
			return got
		}
	}
	return ""
}

func nestedNamedValue(m map[string]any, parent string, keys ...string) string {
	value, ok := m[parent]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case map[string]any:
		return firstStringValue(typed, keys...)
	}
	return ""
}

func namedValue(m map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := m[key]
		if !ok {
			continue
		}
		if got := displayValue(value); got != "" {
			return got
		}
		switch typed := value.(type) {
		case map[string]any:
			if got := firstStringValue(typed, "title", "name", "label", "displayString", "displayName"); got != "" {
				return got
			}
		}
	}
	return ""
}

func statusValue(m map[string]any) string {
	if current := nestedNamedValue(m, "currentStatus", "title", "label", "name"); current != "" {
		return current
	}
	return humanizeStatusCode(firstStringValue(m, "status", "state"))
}

func itemLines(m map[string]any) []string {
	for _, key := range []string{"items", "orderItems", "lineItems", "products", "cartItems"} {
		value, ok := m[key]
		if !ok {
			continue
		}
		list, ok := value.([]any)
		if !ok {
			continue
		}
		lines := make([]string, 0, len(list))
		for _, item := range list {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := firstStringValue(itemMap, "title", "name", "label")
			if name == "" {
				continue
			}
			qty := intValue(itemMap["quantity"])
			if qty > 0 {
				lines = append(lines, fmt.Sprintf("%dx %s", qty, name))
			} else {
				lines = append(lines, name)
			}
		}
		if len(lines) > 0 {
			return lines
		}
	}
	return nil
}

func shoppingCartItems(m map[string]any) []string {
	for _, key := range []string{"shoppingCart", "cart"} {
		value, ok := m[key]
		if !ok {
			continue
		}
		cart, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if items := itemLines(cart); len(items) > 0 {
			return items
		}
	}
	return nil
}

func displayValue(v any) string {
	switch typed := v.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case map[string]any:
		return firstStringValue(typed, "displayString", "displayValue", "title", "name", "label")
	default:
		return ""
	}
}

func firstNonEmptyItems(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func intValue(v any) int {
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func humanizeStatusCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.EqualFold(value, strings.ToUpper(value)) {
		value = strings.ReplaceAll(strings.ToLower(value), "_", " ")
	}
	parts := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func baseOrderStatus(baseOrder map[string]any) string {
	if completed, ok := baseOrder["isCompleted"].(bool); ok && completed {
		return "Completed"
	}
	if cancelled, ok := baseOrder["isCancelled"].(bool); ok && cancelled {
		return "Cancelled"
	}
	stateChanges, ok := baseOrder["deliveryStateChanges"].([]any)
	if !ok || len(stateChanges) == 0 {
		return ""
	}
	last, ok := stateChanges[len(stateChanges)-1].(map[string]any)
	if !ok {
		return ""
	}
	return humanizeStatusCode(firstStringValue(last, "type"))
}

func fareTotal(m map[string]any) string {
	fareInfo, ok := m["fareInfo"].(map[string]any)
	if !ok {
		return ""
	}
	currency := ""
	if baseOrder, ok := m["baseEaterOrder"].(map[string]any); ok {
		currency = firstStringValue(baseOrder, "currencyCode")
	}
	if checkoutInfo, ok := fareInfo["checkoutInfo"].([]any); ok {
		for _, raw := range checkoutInfo {
			line, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !strings.EqualFold(firstStringValue(line, "label", "key"), "Total") && firstStringValue(line, "key") != "eats_fare.total" {
				continue
			}
			if rawValue, ok := line["rawValue"].(float64); ok {
				return formatMoney(rawValue, currency)
			}
		}
	}
	if totalPrice, ok := fareInfo["totalPrice"].(float64); ok {
		return formatMoney(totalPrice/100, currency)
	}
	return ""
}

func formatMoney(value float64, currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USD":
		return fmt.Sprintf("$%.2f", value)
	default:
		if currency == "" {
			return fmt.Sprintf("%.2f", value)
		}
		return fmt.Sprintf("%s %.2f", strings.ToUpper(currency), value)
	}
}

func locationAddress(m map[string]any) string {
	storeInfo, ok := m["storeInfo"].(map[string]any)
	if !ok {
		return ""
	}
	location, ok := storeInfo["location"].(map[string]any)
	if !ok {
		return ""
	}
	address, ok := location["address"].(map[string]any)
	if !ok {
		return ""
	}
	return firstStringValue(address, "eaterFormattedAddress", "title", "address1")
}

func isActiveOrder(m map[string]any, status string, detail string) bool {
	for _, key := range []string{"isActive", "active"} {
		if raw, ok := m[key].(bool); ok {
			return raw
		}
	}
	text := strings.ToLower(strings.TrimSpace(status + " " + detail))
	if text == "" {
		return false
	}
	for _, marker := range []string{"delivered", "completed", "cancelled", "canceled"} {
		if strings.Contains(text, marker) {
			return false
		}
	}
	return true
}

func orderOccurredAt(m map[string]any) time.Time {
	for _, key := range []string{"completedAt", "submittedAt", "createdAt", "deliveredAt", "placedAt", "updatedAt"} {
		if ts := parseTimestamp(firstStringValue(m, key)); !ts.IsZero() {
			return ts
		}
	}
	stateChanges, ok := m["deliveryStateChanges"].([]any)
	if !ok || len(stateChanges) == 0 {
		return time.Time{}
	}
	for i := len(stateChanges) - 1; i >= 0; i-- {
		change, ok := stateChanges[i].(map[string]any)
		if !ok {
			continue
		}
		if ts := parseTimestamp(firstStringValue(change, "stateChangeTime", "time")); !ts.IsZero() {
			return ts
		}
	}
	return time.Time{}
}

func parseTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func uuidFromOrderURL(targetURL string) string {
	if targetURL == "" {
		return ""
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "orders" && parts[i+1] != "" {
			return parts[i+1]
		}
	}
	return ""
}
