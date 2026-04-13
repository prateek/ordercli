package ubereats

import (
	"context"
	"fmt"
	"strings"
)

type Cart struct {
	Ref               string      `json:"ref,omitempty"`
	CartRef           string      `json:"cart_ref,omitempty"`
	StoreRef          string      `json:"store_ref,omitempty"`
	StoreTitle        string      `json:"store_title,omitempty"`
	State             string      `json:"state,omitempty"`
	CurrencyCode      string      `json:"currency_code,omitempty"`
	DeliveryType      string      `json:"delivery_type,omitempty"`
	InteractionType   string      `json:"interaction_type,omitempty"`
	Address           string      `json:"address,omitempty"`
	PaymentProfileRef string      `json:"payment_profile_ref,omitempty"`
	CheckoutReady     bool        `json:"checkout_ready"`
	FeeSummary        string      `json:"fee_summary,omitempty"`
	ItemCount         int         `json:"item_count,omitempty"`
	Subtotal          string      `json:"subtotal,omitempty"`
	Total             string      `json:"total,omitempty"`
	Items             []CartItem  `json:"items,omitempty"`
	SessionInfo       SessionInfo `json:"session,omitempty"`
}

type CartItem struct {
	Ref          string `json:"ref,omitempty"`
	ItemRef      string `json:"item_ref,omitempty"`
	Title        string `json:"title,omitempty"`
	Quantity     int    `json:"quantity,omitempty"`
	Note         string `json:"note,omitempty"`
	PriceMinor   int    `json:"price_minor,omitempty"`
	TotalMinor   int    `json:"total_minor,omitempty"`
	CurrencyCode string `json:"currency_code,omitempty"`
}

func (c *Client) ListCarts(ctx context.Context, limit int) ([]Cart, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getCartsViewForEaterUuidV1"), map[string]any{}, nil)
	if err != nil {
		return nil, err
	}
	carts := cartsFromCollection(parsed)
	if limit > 0 && len(carts) > limit {
		return carts[:limit], nil
	}
	return carts, nil
}

func (c *Client) GetCart(ctx context.Context, ref string) (Cart, error) {
	ref = strings.TrimSpace(ref)
	cart, err := c.fetchCart(ctx, ref)
	if err != nil {
		resolvedRef := c.resolveDraftOrderRef(ctx, ref)
		if resolvedRef == "" || resolvedRef == ref {
			return Cart{}, err
		}
		cart, err = c.fetchCart(ctx, resolvedRef)
		if err != nil {
			return Cart{}, err
		}
	}
	location, err := c.DefaultLocation(ctx)
	if err == nil {
		cart.SessionInfo = sessionInfoFromLocation(location)
		cart.SessionInfo.PaymentProfileRef = firstNonEmpty(cart.PaymentProfileRef, cart.SessionInfo.PaymentProfileRef)
	}
	if profileName, profilePaymentRef, err := c.selectedProfileContext(ctx); err == nil {
		if cart.SessionInfo.Profile == "" {
			cart.SessionInfo.Profile = profileName
		}
		if cart.SessionInfo.PaymentProfileRef == "" {
			cart.SessionInfo.PaymentProfileRef = profilePaymentRef
		}
		if cart.PaymentProfileRef == "" {
			cart.PaymentProfileRef = profilePaymentRef
		}
	}
	if cart.StoreTitle == "" || cart.Subtotal == "" || cart.Address == "" {
		c.enrichCartFromSummaries(ctx, &cart)
	}
	return cart, nil
}

func (c *Client) fetchCart(ctx context.Context, ref string) (Cart, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getDraftOrderByUuidV1"), map[string]any{
		"draftOrderUuid": strings.TrimSpace(ref),
	}, nil)
	if err != nil {
		return Cart{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	cart, ok := cartFromMap(data)
	if !ok {
		return Cart{}, fmt.Errorf("ubereats: cart %q not found", ref)
	}
	return cart, nil
}

func (c *Client) resolveDraftOrderRef(ctx context.Context, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	carts, err := c.ListCarts(ctx, 0)
	if err != nil {
		return ref
	}
	for _, cart := range carts {
		if cart.Ref == ref || cart.CartRef == ref {
			return cart.Ref
		}
	}
	return ref
}

func (c *Client) selectedProfileContext(ctx context.Context) (string, string, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getProfilesForUserV1"), map[string]any{}, nil)
	if err != nil {
		return "", "", err
	}
	data, _ := parsed["data"].(map[string]any)
	selected, _ := data["selectedProfile"].(map[string]any)
	selectedUUID := firstStringValue(selected, "profileUUID", "uuid")
	if name := firstStringValue(selected, "name"); name != "" {
		return name, firstStringValue(selected, "defaultPaymentProfileUuid", "defaultPaymentProfileUUID"), nil
	}
	for _, raw := range listOfMaps(data["profiles"]) {
		if firstStringValue(raw, "uuid") != selectedUUID {
			continue
		}
		return firstStringValue(raw, "name"), firstStringValue(raw, "defaultPaymentProfileUuid", "defaultPaymentProfileUUID"), nil
	}
	return "", "", nil
}

func (c *Client) enrichCartFromSummaries(ctx context.Context, cart *Cart) {
	if cart == nil {
		return
	}
	summaries, err := c.ListCarts(ctx, 0)
	if err != nil {
		return
	}
	for _, summary := range summaries {
		if summary.Ref != cart.Ref && summary.CartRef != cart.CartRef {
			continue
		}
		if cart.StoreTitle == "" {
			cart.StoreTitle = summary.StoreTitle
		}
		if cart.Subtotal == "" {
			cart.Subtotal = summary.Subtotal
		}
		if cart.Address == "" {
			cart.Address = summary.Address
		}
		return
	}
}

func cartSummaryAddress(m map[string]any) string {
	value := firstStringValue(m, "text")
	value = strings.TrimSpace(strings.TrimPrefix(value, "Deliver to "))
	return value
}

func subtotalFromSummary(m map[string]any) string {
	value := firstStringValue(m, "text")
	if value == "" {
		return ""
	}
	value = strings.TrimSpace(strings.TrimPrefix(value, "Subtotal:"))
	return strings.TrimSpace(value)
}

func cartAddressFromDetail(m map[string]any) string {
	address, _ := m["deliveryAddress"].(map[string]any)
	addressMap, _ := address["address"].(map[string]any)
	return firstNonEmpty(
		firstNonEmptyString(stringValue(address["fullAddress"]), stringValue(address["displayString"])),
		joinNonEmpty(", ", firstStringValue(addressMap, "title"), firstStringValue(addressMap, "subtitle")),
		firstStringValue(addressMap, "address1"),
	)
}

func cartStoreTitleFromMap(m map[string]any) string {
	return firstNonEmpty(
		firstStringValue(m, "storeTitle", "storeName", "title"),
		nestedNamedValue(m, "store", "title", "name"),
		nestedNamedValue(m, "storeInfo", "title", "name"),
	)
}

func cartSubtotalFromMap(m map[string]any, cartMap map[string]any, currencyCode string) string {
	return firstNonEmpty(
		subtotalFromSummary(nestedMapValue(m, "tagline1")),
		cartMoneyDisplay(cartMap["subtotal"], currencyCode),
		cartMoneyDisplay(m["subtotal"], currencyCode),
	)
}

func cartFeeSummaryFromMap(m map[string]any) string {
	for _, parent := range []map[string]any{
		m,
		nestedMapValue(m, "shoppingCart"),
		nestedMapValue(m, "fareBreakdown"),
		nestedMapValue(m, "feeBreakdown"),
	} {
		if len(parent) == 0 {
			continue
		}
		for _, key := range []string{"feeSummary", "fareSummary", "fareBreakdown", "feeBreakdown"} {
			if display := firstStringValue(nestedMapValue(parent, key), "displayString", "text", "title", "label"); display != "" {
				return display
			}
		}
		if display := firstStringValue(parent, "feeSummary", "fareSummary"); display != "" {
			return display
		}
	}
	return ""
}

func cartTotalFromMap(m map[string]any, cartMap map[string]any, currencyCode string) string {
	return firstNonEmpty(
		cartMoneyDisplay(cartMap["totalPrice"], currencyCode),
		cartMoneyDisplay(m["totalPrice"], currencyCode),
		cartMoneyDisplay(m["total"], currencyCode),
	)
}

func cartDeliveryTypeFromMap(m map[string]any) string {
	return firstNonEmpty(firstStringValue(m, "deliveryType"), firstStringValue(m, "diningMode"))
}

func cartAddressFromMap(m map[string]any) string {
	return firstNonEmpty(
		cartAddressFromDetail(m),
		cartSummaryAddress(nestedMapValue(m, "tagline2")),
		firstStringValue(m, "deliveryAddress"),
	)
}

func cartItemCountFromMap(m map[string]any, items []CartItem) int {
	cartMap := nestedMapValue(m, "shoppingCart")
	return firstNonZeroInt(cartItemCount(items), intValue(m["itemCount"]), intValue(cartMap["itemCount"]))
}

func cartCheckoutReadyFromMap(m map[string]any) bool {
	if strings.EqualFold(firstStringValue(m, "action"), "OPEN_CHECKOUT") {
		return true
	}
	if metadata, ok := m["metadata"].(map[string]any); ok {
		if _, ok := metadata["openCheckoutMetadata"].(map[string]any); ok {
			return true
		}
	}
	cartMap := nestedMapValue(m, "shoppingCart")
	return boolValue(cartMap["isActive"]) && strings.EqualFold(firstStringValue(m, "state"), "UNORDERED")
}

func cartPaymentProfileFromMap(m map[string]any) string {
	return firstStringValue(m, "paymentProfileUUID", "paymentProfileUuid")
}

func cartStateFromMap(m map[string]any) string {
	return firstStringValue(m, "state", "status")
}

func cartRefsFromMap(m map[string]any) (string, string) {
	cartMap := nestedMapValue(m, "shoppingCart")
	return firstNonEmpty(firstStringValue(m, "draftOrderUUID", "draftOrderUuid", "uuid"), nestedNamedValue(m, "draftOrder", "uuid")),
		firstNonEmpty(firstStringValue(m, "cartUUID", "cartUuid"), firstStringValue(cartMap, "cartUUID", "cartUuid"))
}

func cartStoreRefFromMap(m map[string]any) string {
	return firstNonEmpty(firstStringValue(m, "storeUUID", "storeUuid"), nestedNamedValue(m, "store", "uuid"), nestedNamedValue(m, "storeInfo", "uuid"))
}

func cartCurrencyFromMap(m map[string]any) string {
	return firstNonEmpty(firstStringValue(nestedMapValue(m, "shoppingCart"), "currencyCode"), firstStringValue(m, "currencyCode"))
}

func cartItemsFromMap(m map[string]any, currencyCode string) []CartItem {
	return cartItemsFromValue(nestedMapValue(m, "shoppingCart")["items"], currencyCode)
}

func cartsFromCollection(parsed map[string]any) []Cart {
	data, _ := parsed["data"].(map[string]any)
	seen := map[string]struct{}{}
	out := make([]Cart, 0)
	appendCart := func(cart Cart) {
		key := strings.TrimSpace(firstNonEmpty(cart.Ref, cart.CartRef))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, cart)
	}
	for _, raw := range listOfMaps(nestedMapValue(data, "cartsView")["carts"]) {
		if cart, ok := cartFromMap(raw); ok {
			appendCart(cart)
		}
	}
	for _, raw := range listOfMaps(data["draftOrders"]) {
		if cart, ok := cartFromMap(raw); ok {
			appendCart(cart)
		}
	}
	for _, raw := range listOfMaps(data["carts"]) {
		if cart, ok := cartFromMap(raw); ok {
			appendCart(cart)
		}
	}
	return out
}

func cartFromMap(m map[string]any) (Cart, bool) {
	cartMap := nestedMapValue(m, "shoppingCart")
	ref, cartRef := cartRefsFromMap(m)
	currencyCode := cartCurrencyFromMap(m)
	items := cartItemsFromMap(m, currencyCode)
	cart := Cart{
		Ref:               ref,
		CartRef:           cartRef,
		StoreRef:          cartStoreRefFromMap(m),
		StoreTitle:        cartStoreTitleFromMap(m),
		State:             cartStateFromMap(m),
		CurrencyCode:      currencyCode,
		DeliveryType:      cartDeliveryTypeFromMap(m),
		InteractionType:   firstStringValue(m, "interactionType"),
		Address:           cartAddressFromMap(m),
		PaymentProfileRef: cartPaymentProfileFromMap(m),
		CheckoutReady:     cartCheckoutReadyFromMap(m),
		FeeSummary:        cartFeeSummaryFromMap(m),
		Subtotal:          cartSubtotalFromMap(m, cartMap, currencyCode),
		Total:             cartTotalFromMap(m, cartMap, currencyCode),
		Items:             items,
	}
	cart.ItemCount = cartItemCountFromMap(m, items)
	if cart.Ref == "" && cart.CartRef == "" {
		return Cart{}, false
	}
	if cart.StoreTitle == "" && cart.StoreRef == "" && cart.ItemCount == 0 && len(cart.Items) == 0 {
		return Cart{}, false
	}
	return cart, true
}

func cartItemsFromValue(v any, currencyCode string) []CartItem {
	rows := listOfMaps(v)
	out := make([]CartItem, 0, len(rows))
	for _, row := range rows {
		item := CartItem{
			Ref:          firstStringValue(row, "shoppingCartItemUuid", "shoppingCartItemUUID"),
			ItemRef:      firstStringValue(row, "uuid", "itemUuid", "itemUUID"),
			Title:        firstStringValue(row, "title", "name"),
			Quantity:     intValue(row["quantity"]),
			Note:         firstStringValue(row, "specialInstructions", "note"),
			PriceMinor:   firstNonZeroInt(intValue(row["price"]), intValue(row["unitPrice"])),
			TotalMinor:   firstNonZeroInt(intValue(row["totalPrice"]), intValue(row["price"])*intValue(row["quantity"])),
			CurrencyCode: firstNonEmpty(firstStringValue(row, "currencyCode"), currencyCode),
		}
		if item.Ref == "" && item.ItemRef == "" && item.Title == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func cartItemCount(items []CartItem) int {
	total := 0
	for _, item := range items {
		if item.Quantity > 0 {
			total += item.Quantity
			continue
		}
		total++
	}
	return total
}

func cartMoneyDisplay(v any, currencyCode string) string {
	switch typed := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int(typed)) {
			return minorCurrencyDisplay(int(typed), currencyCode)
		}
		return formatMoney(typed, currencyCode)
	case int:
		return minorCurrencyDisplay(typed, currencyCode)
	case int64:
		return minorCurrencyDisplay(int(typed), currencyCode)
	case map[string]any:
		if display := firstStringValue(typed, "displayString", "displayValue", "title", "label"); display != "" {
			return display
		}
		if raw := typed["rawValue"]; raw != nil {
			return cartMoneyDisplay(raw, currencyCode)
		}
		return cartMoneyDisplay(firstNonZeroInt(intValue(typed["amountMinor"]), intValue(typed["value"]), intValue(typed["total"]), intValue(typed["subtotal"])), currencyCode)
	default:
		return ""
	}
}

func listOfMaps(v any) []map[string]any {
	rows, _ := v.([]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if len(m) == 0 {
			continue
		}
		out = append(out, m)
	}
	return out
}
