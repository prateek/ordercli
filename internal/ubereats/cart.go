package ubereats

import (
	"context"
	"crypto/rand"
	"encoding/json"
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

const (
	CartDeliveryTypeRegular = "regular"
	CartDeliveryTypePremium = "premium"
)

type CartItemUpdate struct {
	Quantity *int    `json:"quantity,omitempty"`
	Note     *string `json:"note,omitempty"`
}

type CartUpdate struct {
	DeliveryType    string `json:"delivery_type,omitempty"`
	InteractionType string `json:"interaction_type,omitempty"`
}

type CartMutation struct {
	Ref       string `json:"ref,omitempty"`
	Added     bool   `json:"added,omitempty"`
	Updated   bool   `json:"updated,omitempty"`
	Removed   bool   `json:"removed,omitempty"`
	Discarded bool   `json:"discarded,omitempty"`
	Cart      *Cart  `json:"cart,omitempty"`
}

var newUUID = randomUUID

type cartDetail struct {
	Cart
	raw map[string]any
}

type orderSeed struct {
	StoreRef     string
	CurrencyCode string
	Items        []map[string]any
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

	return c.createCart(ctx, storeRef, []map[string]any{cartLineFromMenuItem(item, quantity, note)}, item.CurrencyCode)
}

func (c *Client) CreateCartFromOrder(ctx context.Context, ref string) (Cart, error) {
	seed, err := c.findOrderSeed(ctx, ref)
	if err != nil {
		return Cart{}, err
	}
	return c.createCart(ctx, seed.StoreRef, seed.Items, seed.CurrencyCode)
}

func (c *Client) AddCartItem(ctx context.Context, ref, itemRef string, quantity int, note string) (CartMutation, error) {
	ref = strings.TrimSpace(ref)
	itemRef = strings.TrimSpace(itemRef)
	note = strings.TrimSpace(note)
	if itemRef == "" {
		return CartMutation{}, fmt.Errorf("ubereats: item ref is required")
	}
	if quantity <= 0 {
		return CartMutation{}, fmt.Errorf("ubereats: quantity must be positive")
	}

	detail, err := c.resolveCartDetail(ctx, ref)
	if err != nil {
		return CartMutation{}, err
	}
	item, err := c.GetMenuItem(ctx, detail.StoreRef, itemRef)
	if err != nil {
		return CartMutation{}, err
	}
	if itemRequiresCustomization(item) {
		return CartMutation{}, fmt.Errorf("ubereats: item %q requires customizations that are not yet supported", item.Ref)
	}
	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return CartMutation{}, err
	}
	line := cartLineFromMenuItem(item, quantity, note)
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("addItemsToDraftOrderV2"), map[string]any{
		"draftOrderUUID":               detail.Ref,
		"cartUUID":                     detail.CartRef,
		"items":                        []any{line},
		"shouldUpdateDraftOrderMetadata": false,
		"storeUUID":                    detail.StoreRef,
		"actionMeta":                   map[string]any{"isQuickAdd": false, "numClicks": 1},
	}, c.locationHeaders(location))
	if err != nil {
		return CartMutation{}, err
	}
	if !responseIncludesMappedString(nestedMapValue(parsed, "data")["addedItems"], stringValue(line["shoppingCartItemUuid"]), "shoppingCartItemUuid", "shoppingCartItemUUID") {
		return CartMutation{}, fmt.Errorf("ubereats: addItemsToDraftOrderV2 did not confirm added item %q", itemRef)
	}
	updatedCart, err := c.GetCart(ctx, detail.Ref)
	if err != nil {
		return CartMutation{}, err
	}
	return CartMutation{Ref: updatedCart.Ref, Added: true, Cart: &updatedCart}, nil
}

func (c *Client) UpdateCartItem(ctx context.Context, ref, cartItemRef string, update CartItemUpdate) (CartMutation, error) {
	cartItemRef = strings.TrimSpace(cartItemRef)
	if cartItemRef == "" {
		return CartMutation{}, fmt.Errorf("ubereats: cart item ref is required")
	}
	if update.Quantity == nil && update.Note == nil {
		return CartMutation{}, fmt.Errorf("ubereats: at least one cart item field must be updated")
	}
	if update.Quantity != nil && *update.Quantity <= 0 {
		return CartMutation{}, fmt.Errorf("ubereats: quantity must be positive")
	}

	detail, err := c.resolveCartDetail(ctx, ref)
	if err != nil {
		return CartMutation{}, err
	}
	rawItem := findCartItemPayload(detail.raw, cartItemRef)
	if len(rawItem) == 0 {
		return CartMutation{}, fmt.Errorf("ubereats: cart item %q not found", cartItemRef)
	}
	itemPayload := cartLineFromRawItem(rawItem)
	if update.Quantity != nil {
		itemPayload["quantity"] = *update.Quantity
	}
	if update.Note != nil {
		itemPayload["specialInstructions"] = strings.TrimSpace(*update.Note)
	}

	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return CartMutation{}, err
	}
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("updateItemInDraftOrderV2"), map[string]any{
		"draftOrderUUID": detail.Ref,
		"cartUUID":       detail.CartRef,
		"item":           itemPayload,
	}, c.locationHeaders(location))
	if err != nil {
		return CartMutation{}, err
	}
	itemData, _ := nestedMapValue(parsed, "data")["item"].(map[string]any)
	if firstStringValue(itemData, "shoppingCartItemUuid", "shoppingCartItemUUID") != cartItemRef {
		return CartMutation{}, fmt.Errorf("ubereats: updateItemInDraftOrderV2 did not confirm update for cart item %q", cartItemRef)
	}
	updatedCart, err := c.GetCart(ctx, detail.Ref)
	if err != nil {
		return CartMutation{}, err
	}
	updatedItem, ok := findCartItem(updatedCart, cartItemRef)
	if !ok {
		return CartMutation{}, fmt.Errorf("ubereats: updateItemInDraftOrderV2 did not apply update for cart item %q", cartItemRef)
	}
	if update.Quantity != nil && updatedItem.Quantity != *update.Quantity {
		return CartMutation{}, fmt.Errorf("ubereats: updateItemInDraftOrderV2 did not apply requested quantity for cart item %q", cartItemRef)
	}
	if update.Note != nil && updatedItem.Note != strings.TrimSpace(*update.Note) {
		return CartMutation{}, fmt.Errorf("ubereats: updateItemInDraftOrderV2 did not apply requested note for cart item %q", cartItemRef)
	}
	return CartMutation{Ref: updatedCart.Ref, Updated: true, Cart: &updatedCart}, nil
}

func (c *Client) UpdateCart(ctx context.Context, ref string, update CartUpdate) (CartMutation, error) {
	if strings.TrimSpace(update.DeliveryType) == "" && strings.TrimSpace(update.InteractionType) == "" {
		return CartMutation{}, fmt.Errorf("ubereats: at least one cart field must be updated")
	}
	deliveryType, err := normalizeCartDeliveryType(update.DeliveryType)
	if err != nil {
		return CartMutation{}, err
	}
	detail, err := c.resolveCartDetail(ctx, ref)
	if err != nil {
		return CartMutation{}, err
	}
	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return CartMutation{}, err
	}
	_, paymentProfileRef, err := c.selectedProfileContext(ctx)
	if err != nil {
		return CartMutation{}, err
	}
	payload := updateDraftOrderPayload(detail, location, paymentProfileRef)
	if deliveryType != "" {
		payload["deliveryType"] = deliveryType
	}
	if interactionType := strings.TrimSpace(update.InteractionType); interactionType != "" {
		payload["interactionType"] = interactionType
	}
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("updateDraftOrderV2"), payload, c.locationHeaders(location))
	if err != nil {
		return CartMutation{}, err
	}
	data := nestedMapValue(parsed, "data")
	if len(listOfMaps(data["validationErrors"])) > 0 {
		return CartMutation{}, fmt.Errorf("ubereats: updateDraftOrderV2 returned validation errors")
	}
	draftOrder := nestedMapValue(data, "draftOrder")
	if deliveryType != "" && firstStringValue(draftOrder, "deliveryType") != deliveryType {
		return CartMutation{}, fmt.Errorf("ubereats: updateDraftOrderV2 did not apply delivery type %q", deliveryType)
	}
	if interactionType := strings.TrimSpace(update.InteractionType); interactionType != "" && firstStringValue(draftOrder, "interactionType") != interactionType {
		return CartMutation{}, fmt.Errorf("ubereats: updateDraftOrderV2 did not apply interaction type %q", interactionType)
	}
	updatedCart, err := c.GetCart(ctx, detail.Ref)
	if err != nil {
		return CartMutation{}, err
	}
	return CartMutation{Ref: updatedCart.Ref, Updated: true, Cart: &updatedCart}, nil
}

func (c *Client) createCart(ctx context.Context, storeRef string, items []map[string]any, currencyCode string) (Cart, error) {
	storeRef = strings.TrimSpace(storeRef)
	if storeRef == "" {
		return Cart{}, fmt.Errorf("ubereats: store ref is required")
	}
	if len(items) == 0 {
		return Cart{}, fmt.Errorf("ubereats: cart requires at least one item")
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

	parsed, err := c.post(ctx, c.storePageURL(storeRef), c.endpointURL("createDraftOrderV2"), createDraftOrderPayload(items, currencyCode, paymentProfileRef, selectedInteractionType(instruction), location), c.locationHeaders(location))
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
	detail, err := c.fetchCartDetail(ctx, ref)
	if err != nil {
		resolvedRef := c.resolveDraftOrderRef(ctx, ref)
		if resolvedRef == "" || resolvedRef == ref {
			return Cart{}, err
		}
		detail, err = c.fetchCartDetail(ctx, resolvedRef)
		if err != nil {
			return Cart{}, err
		}
	}
	cart := detail.Cart
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

func (c *Client) resolveCartDetail(ctx context.Context, ref string) (cartDetail, error) {
	ref = strings.TrimSpace(ref)
	detail, err := c.fetchCartDetail(ctx, ref)
	if err == nil {
		return detail, nil
	}
	resolvedRef := c.resolveDraftOrderRef(ctx, ref)
	if resolvedRef == "" || resolvedRef == ref {
		return cartDetail{}, err
	}
	return c.fetchCartDetail(ctx, resolvedRef)
}

func (c *Client) fetchCart(ctx context.Context, ref string) (Cart, error) {
	detail, err := c.fetchCartDetail(ctx, ref)
	if err != nil {
		return Cart{}, err
	}
	return detail.Cart, nil
}

func (c *Client) fetchCartDetail(ctx context.Context, ref string) (cartDetail, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getDraftOrderByUuidV1"), map[string]any{
		"draftOrderUuid": strings.TrimSpace(ref),
	}, nil)
	if err != nil {
		return cartDetail{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	cart, ok := cartFromMap(data)
	if !ok {
		return cartDetail{}, fmt.Errorf("ubereats: cart %q not found", ref)
	}
	return cartDetail{Cart: cart, raw: data}, nil
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

func responseIncludesMappedString(v any, want string, keys ...string) bool {
	for _, item := range listOfMaps(v) {
		if firstStringValue(item, keys...) == want {
			return true
		}
	}
	return false
}

func findCartItem(cart Cart, ref string) (CartItem, bool) {
	for _, item := range cart.Items {
		if item.Ref == ref {
			return item, true
		}
	}
	return CartItem{}, false
}

func normalizeCartDeliveryType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case CartDeliveryTypeRegular, "asap":
		return "ASAP", nil
	case CartDeliveryTypePremium, "premium_delivery":
		return "PREMIUM_DELIVERY", nil
	default:
		return "", fmt.Errorf("ubereats: unsupported delivery type %q", value)
	}
}

func cartLineFromMenuItem(item ItemDetail, quantity int, note string) map[string]any {
	return map[string]any{
		"uuid":                 item.Ref,
		"shoppingCartItemUuid": newUUID(),
		"storeUuid":            item.StoreRef,
		"sectionUuid":          item.SectionRef,
		"subsectionUuid":       item.SubsectionRef,
		"price":                item.PriceMinor,
		"title":                item.Title,
		"quantity":             quantity,
		"customizations":       map[string]any{},
		"specialInstructions":  strings.TrimSpace(note),
		"itemId":               nil,
	}
}

func cartLineFromRawItem(raw map[string]any) map[string]any {
	line := map[string]any{
		"uuid":                 firstStringValue(raw, "uuid"),
		"shoppingCartItemUuid": firstStringValue(raw, "shoppingCartItemUuid", "shoppingCartItemUUID"),
		"storeUuid":            firstStringValue(raw, "storeUuid", "storeUUID"),
		"sectionUuid":          firstStringValue(raw, "sectionUuid"),
		"subsectionUuid":       firstStringValue(raw, "subsectionUuid"),
		"price":                intValue(raw["price"]),
		"title":                firstStringValue(raw, "title"),
		"quantity":             firstNonZeroInt(intValue(raw["quantity"]), 1),
		"customizations":       mapValue(raw["customizations"]),
		"specialInstructions":  firstStringValue(raw, "specialInstructions"),
		"itemId":               raw["itemId"],
	}
	if line["customizations"] == nil {
		line["customizations"] = map[string]any{}
	}
	if imageURL := firstStringValue(raw, "imageURL"); imageURL != "" {
		line["imageURL"] = imageURL
	}
	if fulfillmentIssueAction := mapValue(raw["fulfillmentIssueAction"]); len(fulfillmentIssueAction) > 0 {
		line["fulfillmentIssueAction"] = fulfillmentIssueAction
	}
	return line
}

func createDraftOrderPayload(items []map[string]any, currencyCode, paymentProfileRef, interactionType string, location Location) map[string]any {
	shoppingCartItems := make([]any, 0, len(items))
	for _, item := range items {
		shoppingCartItems = append(shoppingCartItems, item)
	}
	payload := map[string]any{
		"isMulticart": true,
		"shoppingCartItems": shoppingCartItems,
		"useCredits":           true,
		"extraPaymentProfiles": []any{},
		"promotionOptions": map[string]any{
			"autoApplyPromotionUUIDs":        []any{},
			"selectedPromotionInstanceUUIDs": []any{},
			"skipApplyingPromotion":          false,
		},
		"deliveryTime":                map[string]any{"asap": true},
		"deliveryType":                "ASAP",
		"currencyCode":                currencyCode,
		"interactionType":             interactionType,
		"paymentProfileUUID":          paymentProfileRef,
		"deliveryAddress":             instructionLocationPayload(location),
		"checkMultipleDraftOrdersCap": true,
		"actionMeta":                  map[string]any{"isQuickAdd": false, "numClicks": 0},
		"businessDetails":             map[string]any{},
	}
	return payload
}

func updateDraftOrderPayload(detail cartDetail, location Location, selectedPaymentProfileRef string) map[string]any {
	payload := map[string]any{
		"draftOrderUUID": detail.Ref,
	}
	if paymentProfileRef := firstNonEmpty(detail.PaymentProfileRef, selectedPaymentProfileRef); paymentProfileRef != "" {
		payload["paymentProfileUUID"] = paymentProfileRef
		payload["paymentProfileSelectionSource"] = "CLIENT_SYSTEM"
	}
	if deliveryAddress := mapValue(detail.raw["deliveryAddress"]); len(deliveryAddress) > 0 {
		payload["deliveryAddress"] = deliveryAddress
	} else {
		payload["deliveryAddress"] = instructionLocationPayload(location)
	}
	if targetDeliveryTimeRange := mapValue(detail.raw["targetDeliveryTimeRange"]); len(targetDeliveryTimeRange) > 0 {
		payload["targetDeliveryTimeRange"] = targetDeliveryTimeRange
	} else if deliveryTime := mapValue(detail.raw["deliveryTime"]); len(deliveryTime) > 0 {
		payload["targetDeliveryTimeRange"] = deliveryTime
	} else {
		payload["targetDeliveryTimeRange"] = map[string]any{"asap": true}
	}
	if diningMode := firstStringValue(detail.raw, "diningMode"); diningMode != "" {
		payload["diningMode"] = diningMode
	} else {
		payload["diningMode"] = "DELIVERY"
	}
	return payload
}

func findCartItemPayload(cart map[string]any, cartItemRef string) map[string]any {
	items := listOfMaps(nestedMapValue(cart, "shoppingCart")["items"])
	for _, item := range items {
		if firstStringValue(item, "shoppingCartItemUuid", "shoppingCartItemUUID") == cartItemRef {
			return item
		}
	}
	return nil
}

func (c *Client) findOrderSeed(ctx context.Context, ref string) (orderSeed, error) {
	ref = strings.TrimSpace(uuidFromOrderRef(ref))
	if ref == "" {
		return orderSeed{}, fmt.Errorf("ubereats: missing order ref")
	}
	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return orderSeed{}, err
	}
	headers := c.locationHeaders(location)
	activeBody, activeErr := c.postRaw(ctx, c.ordersURL(), c.endpointURL("getActiveOrdersV1"), map[string]any{
		"orderUuid":                 nil,
		"timezone":                  localTimezoneName(),
		"showAppUpsellIllustration": true,
		"isDirectTracking":          false,
	}, headers)
	if activeErr == nil {
		if seed, ok := findOrderSeedInBody(activeBody, ref); ok {
			return seed, nil
		}
	}
	responses, err := c.pastOrderResponses(ctx, 0, headers)
	if err != nil {
		return orderSeed{}, err
	}
	for _, response := range responses {
		if seed, ok := findOrderSeedInBody(response.Body, ref); ok {
			return seed, nil
		}
	}
	if activeErr != nil {
		return orderSeed{}, activeErr
	}
	return orderSeed{}, fmt.Errorf("order %q not found", ref)
}

func findOrderSeedInBody(body string, ref string) (orderSeed, bool) {
	var parsed any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return orderSeed{}, false
	}
	var found orderSeed
	ok := false
	walkJSON(parsed, func(m map[string]any) {
		if ok {
			return
		}
		if firstStringValue(m, "uuid", "orderUuid") == ref {
			if seed, seedOK := orderSeedFromMap(m); seedOK {
				found = seed
				ok = true
				return
			}
		}
		if baseOrder := nestedMapValue(m, "baseEaterOrder"); len(baseOrder) > 0 && firstStringValue(baseOrder, "uuid") == ref {
			if seed, seedOK := orderSeedFromMap(baseOrder); seedOK {
				found = seed
				ok = true
			}
		}
	})
	return found, ok
}

func orderSeedFromMap(m map[string]any) (orderSeed, bool) {
	shoppingCart := nestedMapValue(m, "shoppingCart")
	storeRef := firstNonEmpty(firstStringValue(m, "storeUuid"), firstStringValue(shoppingCart, "storeUuid", "storeUUID"))
	currencyCode := firstNonEmpty(firstStringValue(m, "currencyCode"), firstStringValue(shoppingCart, "currencyCode"))
	items := listOfMaps(shoppingCart["items"])
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		seedItem := orderSeedItemFromMap(item, storeRef)
		if len(seedItem) == 0 {
			continue
		}
		out = append(out, seedItem)
	}
	if storeRef == "" || len(out) == 0 {
		return orderSeed{}, false
	}
	return orderSeed{
		StoreRef:     storeRef,
		CurrencyCode: currencyCode,
		Items:        out,
	}, true
}

func orderSeedItemFromMap(raw map[string]any, defaultStoreRef string) map[string]any {
	item := cartLineFromRawItem(raw)
	item["shoppingCartItemUuid"] = newUUID()
	item["storeUuid"] = firstNonEmpty(firstStringValue(raw, "storeUuid", "storeUUID"), defaultStoreRef)
	if item["uuid"] == "" || item["storeUuid"] == "" || item["title"] == "" {
		return nil
	}
	return item
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
