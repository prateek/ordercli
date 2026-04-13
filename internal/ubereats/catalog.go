package ubereats

import (
	"context"
	"fmt"
	"strings"
)

type Store struct {
	Ref          string  `json:"ref"`
	Title        string  `json:"title"`
	CurrencyCode string  `json:"currency_code"`
	Orderable    bool    `json:"orderable"`
	Favorite     bool    `json:"favorite"`
	Rating       float64 `json:"rating"`
	RatingCount  string  `json:"rating_count"`
	ETADisplay   string  `json:"eta_display"`
	FeeDisplay   string  `json:"fee_display"`
}

type StoreMenu struct {
	Store    Store              `json:"store"`
	Sections []StoreMenuSection `json:"sections"`
}

type StoreMenuSection struct {
	Ref      string      `json:"ref"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle"`
	Items    []StoreItem `json:"items"`
}

type StoreItem struct {
	Ref               string `json:"ref"`
	StoreRef          string `json:"store_ref"`
	StoreTitle        string `json:"store_title"`
	SectionRef        string `json:"section_ref"`
	SubsectionRef     string `json:"subsection_ref"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	PriceMinor        int    `json:"price_minor"`
	CurrencyCode      string `json:"currency_code"`
	SoldOut           bool   `json:"sold_out"`
	HasCustomizations bool   `json:"has_customizations"`
}

type ItemDetail struct {
	StoreItem
	Customizations []CustomizationGroup `json:"customizations"`
}

type CustomizationGroup struct {
	Ref          string                `json:"ref"`
	Title        string                `json:"title"`
	MinPermitted int                   `json:"min_permitted"`
	MaxPermitted int                   `json:"max_permitted"`
	Options      []CustomizationOption `json:"options"`
}

type CustomizationOption struct {
	Ref        string `json:"ref"`
	Title      string `json:"title"`
	PriceMinor int    `json:"price_minor"`
	SoldOut    bool   `json:"sold_out"`
}

func (c *Client) GetStore(ctx context.Context, ref string) (Store, error) {
	data, _, err := c.storeData(ctx, ref)
	if err != nil {
		return Store{}, err
	}
	return normalizeStore(data), nil
}

func (c *Client) GetStoreMenu(ctx context.Context, ref string) (StoreMenu, error) {
	data, location, err := c.storeData(ctx, ref)
	if err != nil {
		return StoreMenu{}, err
	}
	return normalizeStoreMenu(data, location), nil
}

func (c *Client) SearchStoreItems(ctx context.Context, storeRef, query string, limit int) ([]StoreItem, error) {
	menu, err := c.GetStoreMenu(ctx, storeRef)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	seen := map[string]struct{}{}
	out := make([]StoreItem, 0)
	for _, section := range menu.Sections {
		for _, item := range section.Items {
			if query != "" {
				haystack := strings.ToLower(strings.TrimSpace(item.Title + "\n" + item.Description))
				if !strings.Contains(haystack, query) {
					continue
				}
			}
			if _, ok := seen[item.Ref]; ok {
				continue
			}
			seen[item.Ref] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (c *Client) SearchItems(ctx context.Context, query string, limit int) ([]StoreItem, error) {
	location, err := c.effectiveLocation(ctx)
	if err != nil {
		return nil, err
	}
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getSearchFeedV1"), map[string]any{
		"userQuery":      strings.TrimSpace(query),
		"date":           "",
		"startTime":      0,
		"endTime":        0,
		"sortAndFilters": []any{},
		"vertical":       "ALL",
		"searchSource":   "SEARCH_SUGGESTION",
		"displayType":    "SEARCH_RESULTS",
		"searchType":     "GLOBAL_SEARCH",
		"keyName":        "",
		"cacheKey":       "",
		"recaptchaToken": "",
	}, c.locationHeaders(location))
	if err != nil {
		return nil, err
	}
	data, _ := parsed["data"].(map[string]any)
	storesMap, _ := data["storesMap"].(map[string]any)
	feedItems, _ := data["feedItems"].([]any)
	currency := firstStringValue(data, "currencyCode")
	seen := map[string]struct{}{}
	out := make([]StoreItem, 0)
	for _, rawFeedItem := range feedItems {
		feedItem, _ := rawFeedItem.(map[string]any)
		defaultStoreRef := firstNonEmpty(firstStringValue(feedItem, "storeUuid", "uuid"), storeUUIDFromFeedItem(feedItem))
		storeInfo, _ := storesMap[defaultStoreRef].(map[string]any)
		found := searchFeedItems(feedItem)
		for _, itemMap := range found {
			item := StoreItem{
				Ref:               firstStringValue(itemMap, "uuid"),
				StoreRef:          firstNonEmpty(firstStringValue(itemMap, "storeUuid"), defaultStoreRef),
				StoreTitle:        firstNonEmpty(firstStringValue(storeInfo, "title"), nestedNamedValue(storeInfo, "title", "text"), firstStringValue(itemMap, "storeTitle")),
				SectionRef:        firstStringValue(itemMap, "sectionUuid"),
				SubsectionRef:     firstStringValue(itemMap, "subsectionUuid"),
				Title:             firstStringValue(itemMap, "title"),
				Description:       firstStringValue(itemMap, "itemDescription"),
				PriceMinor:        intValue(itemMap["price"]),
				CurrencyCode:      firstNonEmpty(firstStringValue(itemMap, "currencyCode"), currency),
				SoldOut:           boolValue(itemMap["isSoldOut"]),
				HasCustomizations: boolValue(itemMap["hasCustomizations"]),
			}
			if item.Ref == "" || item.Title == "" {
				continue
			}
			key := item.StoreRef + "\x00" + item.Ref
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if limit > 0 && len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func (c *Client) GetMenuItem(ctx context.Context, storeRef, itemRef string) (ItemDetail, error) {
	menu, err := c.GetStoreMenu(ctx, storeRef)
	if err != nil {
		return ItemDetail{}, err
	}
	var summary StoreItem
	found := false
	for _, section := range menu.Sections {
		for _, item := range section.Items {
			if item.Ref == itemRef {
				summary = item
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		return ItemDetail{}, fmt.Errorf("ubereats: item %q not found in store %q", itemRef, storeRef)
	}

	location, err := c.effectiveLocation(ctx)
	if err != nil {
		return ItemDetail{}, err
	}
	parsed, err := c.post(ctx, c.storePageURL(storeRef), c.endpointURL("getMenuItemV1"), map[string]any{
		"itemRequestType":            "ITEM",
		"storeUuid":                  strings.TrimSpace(storeRef),
		"sectionUuid":                summary.SectionRef,
		"subsectionUuid":             summary.SubsectionRef,
		"menuItemUuid":               summary.Ref,
		"cbType":                     "EATER_ENDORSED",
		"includeCheaperAlternatives": false,
		"contextReferences": []any{
			map[string]any{
				"type": "GROUP_ITEMS",
				"payload": map[string]any{
					"type": "groupItemsContextReferencePayload",
					"groupItemsContextReferencePayload": map[string]any{
						"catalogSectionUUID": "",
					},
				},
				"pageContext": "STORE",
			},
		},
	}, c.locationHeaders(location))
	if err != nil {
		return ItemDetail{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	detail := ItemDetail{
		StoreItem: StoreItem{
			Ref:               firstNonEmpty(firstStringValue(data, "uuid"), summary.Ref),
			StoreRef:          summary.StoreRef,
			StoreTitle:        summary.StoreTitle,
			SectionRef:        firstNonEmpty(firstStringValue(data, "sectionUuid"), summary.SectionRef),
			SubsectionRef:     firstNonEmpty(firstStringValue(data, "subsectionUuid"), summary.SubsectionRef),
			Title:             firstNonEmpty(firstStringValue(data, "title"), summary.Title),
			Description:       firstNonEmpty(firstStringValue(data, "itemDescription"), summary.Description),
			PriceMinor:        firstNonZeroInt(intValue(data["price"]), summary.PriceMinor),
			CurrencyCode:      summary.CurrencyCode,
			SoldOut:           boolValue(data["isSoldOut"]),
			HasCustomizations: boolValue(data["hasCustomizations"]),
		},
		Customizations: customizationGroups(data["customizationsList"]),
	}
	return detail, nil
}

func (c *Client) storeData(ctx context.Context, ref string) (map[string]any, Location, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, Location{}, fmt.Errorf("ubereats: missing store ref")
	}
	location, err := c.effectiveLocation(ctx)
	if err != nil {
		return nil, Location{}, err
	}
	parsed, err := c.post(ctx, c.storePageURL(ref), c.endpointURL("getStoreV1"), map[string]any{
		"storeUuid":  ref,
		"diningMode": "DELIVERY",
		"time":       map[string]any{"asap": true},
		"cbType":     "EATER_ENDORSED",
	}, c.locationHeaders(location))
	if err != nil {
		return nil, Location{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	return data, location, nil
}

func (c *Client) effectiveLocation(ctx context.Context) (Location, error) {
	location, err := c.DefaultLocation(ctx)
	if err != nil {
		return Location{}, fmt.Errorf("ubereats: resolve default location: %w", err)
	}
	return location, nil
}

func (c *Client) storePageURL(ref string) string {
	return c.BaseURL + "/store/" + strings.TrimSpace(ref)
}

func normalizeStoreMenu(data map[string]any, _ Location) StoreMenu {
	store := normalizeStore(data)
	catalogSectionsMap, _ := data["catalogSectionsMap"].(map[string]any)
	rawSections, _ := data["sections"].([]any)
	sections := make([]StoreMenuSection, 0, len(rawSections))
	for _, rawSection := range rawSections {
		sectionMap, _ := rawSection.(map[string]any)
		sectionRef := firstStringValue(sectionMap, "uuid")
		section := StoreMenuSection{
			Ref:      sectionRef,
			Title:    firstStringValue(sectionMap, "title"),
			Subtitle: firstStringValue(sectionMap, "subtitle"),
			Items:    normalizeCatalogItems(catalogSectionsMap[sectionRef], store),
		}
		sections = append(sections, section)
	}
	return StoreMenu{
		Store:    store,
		Sections: sections,
	}
}

func normalizeStore(data map[string]any) Store {
	currency := firstStringValue(data, "currencyCode")
	feeDisplay := firstNonEmpty(
		nestedNamedValue(data, "fareBadge", "text"),
		minorCurrencyDisplay(intValue(nestedMapValue(data, "fareInfo")["serviceFee"]), currency),
	)
	return Store{
		Ref:          firstStringValue(data, "uuid"),
		Title:        firstStringValue(data, "title"),
		CurrencyCode: currency,
		Orderable:    boolValue(data["isOrderable"]),
		Favorite:     boolValue(data["isFavorite"]),
		Rating:       floatValue(data["rating"]),
		RatingCount:  firstNonEmpty(nestedNamedValue(data, "rating", "accessibilityText"), nestedNamedValue(data, "rating", "text"), firstStringValue(data, "ratingCount")),
		ETADisplay:   etaDisplay(nestedMapValue(data, "etaRange")),
		FeeDisplay:   feeDisplay,
	}
}

func normalizeCatalogItems(raw any, store Store) []StoreItem {
	rows, _ := raw.([]any)
	items := make([]StoreItem, 0)
	seen := map[string]struct{}{}
	for _, row := range rows {
		rowMap, _ := row.(map[string]any)
		payload, _ := rowMap["payload"].(map[string]any)
		standard, _ := payload["standardItemsPayload"].(map[string]any)
		catalogItems, _ := standard["catalogItems"].([]any)
		for _, rawItem := range catalogItems {
			itemMap, _ := rawItem.(map[string]any)
			item := StoreItem{
				Ref:               firstStringValue(itemMap, "uuid"),
				StoreRef:          store.Ref,
				StoreTitle:        store.Title,
				SectionRef:        firstStringValue(itemMap, "sectionUuid"),
				SubsectionRef:     firstStringValue(itemMap, "subsectionUuid"),
				Title:             firstStringValue(itemMap, "title"),
				Description:       firstStringValue(itemMap, "itemDescription"),
				PriceMinor:        intValue(itemMap["price"]),
				CurrencyCode:      store.CurrencyCode,
				SoldOut:           boolValue(itemMap["isSoldOut"]),
				HasCustomizations: boolValue(itemMap["hasCustomizations"]),
			}
			if item.Ref == "" {
				continue
			}
			key := item.Ref + "\x00" + item.SectionRef
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			items = append(items, item)
		}
	}
	return items
}

func customizationGroups(v any) []CustomizationGroup {
	rows, _ := v.([]any)
	if len(rows) == 0 {
		return nil
	}
	out := make([]CustomizationGroup, 0, len(rows))
	for _, raw := range rows {
		groupMap, _ := raw.(map[string]any)
		group := CustomizationGroup{
			Ref:          firstStringValue(groupMap, "uuid"),
			Title:        firstStringValue(groupMap, "title"),
			MinPermitted: intValue(groupMap["minPermitted"]),
			MaxPermitted: intValue(groupMap["maxPermitted"]),
			Options:      customizationOptions(groupMap["options"]),
		}
		out = append(out, group)
	}
	return out
}

func customizationOptions(v any) []CustomizationOption {
	rows, _ := v.([]any)
	if len(rows) == 0 {
		return nil
	}
	out := make([]CustomizationOption, 0, len(rows))
	for _, raw := range rows {
		optionMap, _ := raw.(map[string]any)
		out = append(out, CustomizationOption{
			Ref:        firstStringValue(optionMap, "uuid"),
			Title:      firstStringValue(optionMap, "title"),
			PriceMinor: intValue(optionMap["price"]),
			SoldOut:    boolValue(optionMap["isSoldOut"]),
		})
	}
	return out
}

func nestedMapValue(m map[string]any, key string) map[string]any {
	value, _ := m[key].(map[string]any)
	return value
}

func etaDisplay(m map[string]any) string {
	min := intValue(m["min"])
	max := intValue(m["max"])
	switch {
	case min > 0 && max > 0 && min != max:
		return fmt.Sprintf("%d-%d min", min, max)
	case min > 0:
		return fmt.Sprintf("%d min", min)
	case max > 0:
		return fmt.Sprintf("%d min", max)
	default:
		return ""
	}
}

func minorCurrencyDisplay(minor int, currency string) string {
	if minor <= 0 {
		return ""
	}
	return formatMoney(float64(minor)/100, currency)
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func searchFeedItems(v any) []map[string]any {
	switch typed := v.(type) {
	case map[string]any:
		out := make([]map[string]any, 0)
		if looksLikeStoreItem(typed) {
			out = append(out, typed)
		}
		for _, value := range typed {
			out = append(out, searchFeedItems(value)...)
		}
		return out
	case []any:
		out := make([]map[string]any, 0)
		for _, item := range typed {
			out = append(out, searchFeedItems(item)...)
		}
		return out
	default:
		return nil
	}
}

func looksLikeStoreItem(m map[string]any) bool {
	if firstStringValue(m, "uuid") == "" || firstStringValue(m, "title") == "" {
		return false
	}
	if intValue(m["price"]) > 0 {
		return true
	}
	if firstStringValue(m, "itemDescription", "sectionUuid", "subsectionUuid") != "" {
		return true
	}
	if _, ok := m["hasCustomizations"]; ok {
		return true
	}
	return false
}

func storeUUIDFromFeedItem(m map[string]any) string {
	for _, key := range []string{"storeUuid", "uuid"} {
		if value := firstStringValue(m, key); value != "" {
			return value
		}
	}
	payload, _ := m["payload"].(map[string]any)
	return firstStringValue(payload, "storeUuid", "uuid")
}
