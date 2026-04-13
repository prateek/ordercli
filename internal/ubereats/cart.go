package ubereats

import (
	"context"
	"crypto/rand"
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

type CartMutation struct {
	Ref       string `json:"ref,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Discarded bool   `json:"discarded,omitempty"`
	Cart      *Cart  `json:"cart,omitempty"`
}

var newUUID = randomUUID

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

func (c *Client) CreateCartFromItem(ctx context.Context, storeRef, itemRef string, quantity int, note string) (Cart, error) {
	storeRef = strings.TrimSpace(storeRef)
	itemRef = strings.TrimSpace(itemRef)
	note = strings.TrimSpace(note)
	if storeRef == "" || itemRef == "" {
		return Cart{}, fmt.Errorf("ubereats: store ref and item ref are required")
	}
	if quantity <= 0 {
		return Cart{}, fmt.Errorf("ubereats: quantity must be positive")
	}

	item, err := c.GetMenuItem(ctx, storeRef, itemRef)
	if err != nil {
		return Cart{}, err
	}
	if itemRequiresCustomization(item) {
		return Cart{}, fmt.Errorf("ubereats: item %q requires customizations that are not yet supported", item.Ref)
	}

	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return Cart{}, err
	}
	instruction, err := c.GetInstructionContext(ctx, location)
	if err != nil {
		return Cart{}, err
	}
	_, paymentProfileRef, err := c.selectedProfileContext(ctx)
	if err != nil {
		return Cart{}, err
	}

	parsed, err := c.post(ctx, c.storePageURL(storeRef), c.endpointURL("createDraftOrderV2"), map[string]any{
		"isMulticart": true,
		"shoppingCartItems": []any{map[string]any{
			"uuid":                 item.Ref,
			"shoppingCartItemUuid": newUUID(),
			"storeUuid":            storeRef,
			"sectionUuid":          item.SectionRef,
			"subsectionUuid":       item.SubsectionRef,
			"price":                item.PriceMinor,
			"title":                item.Title,
			"quantity":             quantity,
			"customizations":       map[string]any{},
			"specialInstructions":  note,
			"itemId":               nil,
		}},
		"useCredits":           true,
		"extraPaymentProfiles": []any{},
		"promotionOptions": map[string]any{
			"autoApplyPromotionUUIDs":        []any{},
			"selectedPromotionInstanceUUIDs": []any{},
			"skipApplyingPromotion":          false,
		},
		"deliveryTime":                map[string]any{"asap": true},
		"deliveryType":                "ASAP",
		"currencyCode":                item.CurrencyCode,
		"interactionType":             selectedInteractionType(instruction),
		"paymentProfileUUID":          paymentProfileRef,
		"deliveryAddress":             instructionLocationPayload(location),
		"checkMultipleDraftOrdersCap": true,
		"actionMeta":                  map[string]any{"isQuickAdd": false, "numClicks": 0},
		"businessDetails":             map[string]any{},
	}, c.locationHeaders(location))
	if err != nil {
		return Cart{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	draftOrder, _ := data["draftOrder"].(map[string]any)
	draftRef := firstStringValue(draftOrder, "uuid")
	if draftRef == "" {
		return Cart{}, fmt.Errorf("ubereats: createDraftOrderV2 did not return a draft order uuid")
	}
	return c.GetCart(ctx, draftRef)
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

func (c *Client) RemoveCartItem(ctx context.Context, ref, cartItemRef string) (CartMutation, error) {
	cartItemRef = strings.TrimSpace(cartItemRef)
	if cartItemRef == "" {
		return CartMutation{}, fmt.Errorf("ubereats: cart item ref is required")
	}
	ref = c.resolveDraftOrderRef(ctx, ref)
	cart, err := c.fetchCart(ctx, ref)
	if err != nil {
		return CartMutation{}, err
	}
	headers := map[string]string{}
	if location, err := c.DefaultLocation(ctx); err == nil {
		headers = c.locationHeaders(location)
	}
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("removeItemsFromDraftOrderV2"), map[string]any{
		"cartUUID":              cart.CartRef,
		"draftOrderUUID":        cart.Ref,
		"shoppingCartItemUUIDs": []string{cartItemRef},
		"storeUUID":             cart.StoreRef,
	}, headers)
	if err != nil {
		return CartMutation{}, err
	}
	if !responseIncludesString(nestedMapValue(parsed, "data")["shoppingCartItemUUIDs"], cartItemRef) {
		return CartMutation{}, fmt.Errorf("ubereats: removeItemsFromDraftOrderV2 did not confirm removal for cart item %q", cartItemRef)
	}
	summaries, err := c.ListCarts(ctx, 0)
	if err == nil && !cartStillPresent(summaries, cart.Ref, cart.CartRef) {
		return CartMutation{Ref: cart.Ref, Removed: true}, nil
	}
	updatedCart, err := c.GetCart(ctx, cart.Ref)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return CartMutation{Ref: cart.Ref, Removed: true}, nil
		}
		return CartMutation{}, err
	}
	return CartMutation{Ref: updatedCart.Ref, Removed: true, Cart: &updatedCart}, nil
}

func (c *Client) DiscardCart(ctx context.Context, ref string) (CartMutation, error) {
	ref = strings.TrimSpace(ref)
	cart, err := c.fetchCart(ctx, ref)
	if err != nil {
		resolvedRef := c.resolveDraftOrderRef(ctx, ref)
		if resolvedRef == "" || resolvedRef == ref {
			return CartMutation{}, err
		}
		cart, err = c.fetchCart(ctx, resolvedRef)
		if err != nil {
			return CartMutation{}, err
		}
	}
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("discardDraftOrdersV1"), map[string]any{
		"draftOrderUUIDs": []string{cart.Ref},
		"storeUUID":       cart.StoreRef,
	}, nil)
	if err != nil {
		return CartMutation{}, err
	}
	if !responseIncludesString(nestedMapValue(parsed, "data")["discardedDraftOrderUUIDs"], cart.Ref) {
		return CartMutation{}, fmt.Errorf("ubereats: discardDraftOrdersV1 did not discard cart %q", cart.Ref)
	}
	return CartMutation{Ref: cart.Ref, Discarded: true}, nil
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

func selectedInteractionType(context InstructionContext) string {
	return firstNonEmpty(
		context.SelectedInstruction.InteractionType,
		context.PreferredInteractionType,
		context.DefaultInteractionType,
	)
}

func itemRequiresCustomization(item ItemDetail) bool {
	for _, group := range item.Customizations {
		if group.MinPermitted > 0 {
			return true
		}
	}
	return false
}

func cartStillPresent(carts []Cart, refs ...string) bool {
	for _, cart := range carts {
		for _, ref := range refs {
			if ref == "" {
				continue
			}
			if cart.Ref == ref || cart.CartRef == ref {
				return true
			}
		}
	}
	return false
}

func responseIncludesString(v any, want string) bool {
	for _, value := range stringList(v) {
		if value == want {
			return true
		}
	}
	return false
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

func stringList(v any) []string {
	rows, _ := v.([]any)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		value := strings.TrimSpace(stringValue(row))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func randomUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	)
}
