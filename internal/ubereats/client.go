package ubereats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/steipete/ordercli/internal/browserpage"
)

type OrderFilter string

const (
	OrderFilterActive OrderFilter = "active"
	OrderFilterPast   OrderFilter = "past"
	OrderFilterAll    OrderFilter = "all"
)

type Session struct {
	LoggedIn  bool   `json:"logged_in"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

type Location struct {
	Ref         string  `json:"ref,omitempty"`
	Source      string  `json:"source,omitempty"`
	Label       string  `json:"label,omitempty"`
	Title       string  `json:"title,omitempty"`
	FullAddress string  `json:"full_address,omitempty"`
	Latitude    float64 `json:"latitude,omitempty"`
	Longitude   float64 `json:"longitude,omitempty"`

	locationPayload map[string]any
}

type Instruction struct {
	InteractionType string `json:"interaction_type,omitempty"`
	DisplayString   string `json:"display_string,omitempty"`
	Notes           string `json:"notes,omitempty"`
	AptOrSuite      string `json:"apt_or_suite,omitempty"`
}

type InstructionContext struct {
	AvailableInteractionTypes []string    `json:"available_interaction_types,omitempty"`
	DefaultInteractionType    string      `json:"default_interaction_type,omitempty"`
	PreferredInteractionType  string      `json:"preferred_interaction_type,omitempty"`
	SelectedInstruction       Instruction `json:"selected_instruction,omitempty"`
}

type Client struct {
	BaseURL      string
	ProfileDir   string
	CookieHeader string
	UserAgent    string
	LogWriter    io.Writer
	Timeout      time.Duration
	HTTPClient   *http.Client
	ReadSession  func(context.Context, string, browserpage.Options) (browserpage.SessionResult, error)
}

func NewClient(baseURL, profileDir, userAgent string, logWriter io.Writer) *Client {
	return &Client{
		BaseURL:     normalizeBaseURL(baseURL),
		ProfileDir:  strings.TrimSpace(profileDir),
		UserAgent:   strings.TrimSpace(userAgent),
		LogWriter:   logWriter,
		Timeout:     2 * time.Minute,
		ReadSession: browserpage.ReadSession,
	}
}

func (c *Client) SetCookieHeader(cookieHeader string) {
	c.CookieHeader = strings.TrimSpace(cookieHeader)
}

func (c *Client) CheckSession(ctx context.Context) (Session, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getUserV1"), map[string]any{
		"shouldGetPointEstimateMetadata": true,
	}, nil)
	if err != nil {
		return Session{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	return Session{
		LoggedIn:  boolValue(data["isLoggedIn"]),
		FirstName: stringValue(data["firstName"]),
		LastName:  stringValue(data["lastName"]),
	}, nil
}

func (c *Client) DefaultLocation(ctx context.Context) (Location, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getDeliveryLocationsV2"), map[string]any{
		"locationTypes": []string{"TARGET", "SAVED", "SUGGESTED"},
	}, nil)
	if err != nil {
		return Location{}, err
	}
	locations := extractLocations(parsed)
	for _, source := range []string{"TARGET", "SAVED", "SUGGESTED"} {
		for _, location := range locations {
			if location.Source == source {
				return location, nil
			}
		}
	}
	return Location{}, errors.New("ubereats: no delivery location found")
}

func (c *Client) ListLocations(ctx context.Context) ([]Location, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getDeliveryLocationsV2"), map[string]any{
		"locationTypes": []string{"TARGET", "SAVED", "SUGGESTED"},
	}, nil)
	if err != nil {
		return nil, err
	}
	return extractLocations(parsed), nil
}

func (c *Client) GetInstructionContext(ctx context.Context, location Location) (InstructionContext, error) {
	parsed, err := c.post(ctx, c.ordersURL(), c.endpointURL("getInstructionForLocationV1"), map[string]any{
		"location": instructionLocationPayload(location),
	}, c.locationHeaders(location))
	if err != nil {
		return InstructionContext{}, err
	}
	data, _ := parsed["data"].(map[string]any)
	return InstructionContext{
		AvailableInteractionTypes: stringSliceValue(data["availableInteractionTypes"]),
		DefaultInteractionType:    stringValue(data["defaultInteractionType"]),
		PreferredInteractionType:  stringValue(data["preferredInteractionType"]),
		SelectedInstruction:       instructionFromMap(data["selectedInstruction"]),
	}, nil
}

func (c *Client) ListOrders(ctx context.Context, filter OrderFilter, limit int) ([]Order, error) {
	if filter == "" {
		filter = OrderFilterActive
	}
	location, _ := c.DefaultLocation(ctx)
	headers := c.locationHeaders(location)

	res := browserpage.Result{FinalURL: c.ordersURL()}
	switch filter {
	case OrderFilterActive:
		body, err := c.postRaw(ctx, c.ordersURL(), c.endpointURL("getActiveOrdersV1"), map[string]any{
			"orderUuid":                 nil,
			"timezone":                  localTimezoneName(),
			"showAppUpsellIllustration": true,
			"isDirectTracking":          false,
		}, headers)
		if err != nil {
			return nil, err
		}
		res.Responses = append(res.Responses, browserpage.CapturedResponse{
			URL:         c.endpointURL("getActiveOrdersV1"),
			Status:      http.StatusOK,
			ContentType: "application/json",
			Body:        body,
		})
	case OrderFilterPast:
		pastResponses, err := c.pastOrderResponses(ctx, limit, headers)
		if err != nil {
			return nil, err
		}
		res.Responses = append(res.Responses, pastResponses...)
	case OrderFilterAll:
		active, err := c.postRaw(ctx, c.ordersURL(), c.endpointURL("getActiveOrdersV1"), map[string]any{
			"orderUuid":                 nil,
			"timezone":                  localTimezoneName(),
			"showAppUpsellIllustration": true,
			"isDirectTracking":          false,
		}, headers)
		if err != nil {
			return nil, err
		}
		res.Responses = append(res.Responses, browserpage.CapturedResponse{
			URL:         c.endpointURL("getActiveOrdersV1"),
			Status:      http.StatusOK,
			ContentType: "application/json",
			Body:        active,
		})
		pastResponses, err := c.pastOrderResponses(ctx, limit, headers)
		if err != nil {
			return nil, err
		}
		res.Responses = append(res.Responses, pastResponses...)
	default:
		return nil, fmt.Errorf("ubereats: unsupported filter %q", filter)
	}

	page := ParsePage(res)
	orders := page.Orders
	for i := range orders {
		orders[i].SessionInfo = sessionInfoFromLocation(location)
	}
	orders = filterOrders(orders, filter)
	if limit > 0 && len(orders) > limit {
		orders = orders[:limit]
	}
	return orders, nil
}

func (c *Client) GetOrder(ctx context.Context, ref string) (Order, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Order{}, errors.New("ubereats: missing order ref")
	}
	if parsedRef := uuidFromOrderRef(ref); parsedRef != "" {
		ref = parsedRef
	}

	location, _ := c.DefaultLocation(ctx)
	headers := c.locationHeaders(location)

	if activeOrders, err := c.ListOrders(ctx, OrderFilterActive, 0); err == nil {
		for _, order := range activeOrders {
			if order.UUID == ref || strings.EqualFold(order.URL, ref) {
				return order, nil
			}
		}
	}

	responses, err := c.pastOrderResponses(ctx, 0, headers)
	if err != nil {
		return Order{}, err
	}
	page := ParsePage(browserpage.Result{FinalURL: c.ordersURL(), Responses: responses})
	for i := range page.Orders {
		page.Orders[i].SessionInfo = sessionInfoFromLocation(location)
	}
	for _, order := range page.Orders {
		if order.UUID == ref || strings.EqualFold(order.URL, ref) {
			return order, nil
		}
	}
	return Order{}, fmt.Errorf("order %q not found", ref)
}

func (c *Client) post(ctx context.Context, pageURL, requestURL string, body any, headers map[string]string) (map[string]any, error) {
	raw, err := c.postRaw(ctx, pageURL, requestURL, body, headers)
	if err != nil {
		return nil, err
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("ubereats: decode response: %w", err)
	}
	return parsed, nil
}

func (c *Client) postRaw(ctx context.Context, pageURL, requestURL string, body any, headers map[string]string) (string, error) {
	if err := c.ensureSession(ctx); err != nil {
		return "", err
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	for key, value := range withBaseHeaders(headers) {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", c.CookieHeader)
	req.Header.Set("Origin", c.BaseURL)
	req.Header.Set("Referer", pageURL)
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	bodyText := string(bodyBytes)
	c.traceRequest(requestURL, payload, req.Header, resp.StatusCode, bodyText)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ubereats: %s status %d", requestURL, resp.StatusCode)
	}
	return bodyText, nil
}

func (c *Client) pastOrderResponses(ctx context.Context, limit int, headers map[string]string) ([]browserpage.CapturedResponse, error) {
	responses := make([]browserpage.CapturedResponse, 0, 1)
	remaining := limit
	cursor := ""
	seenCursors := map[string]struct{}{}
	for {
		payload := map[string]any{}
		if cursor != "" {
			payload["lastWorkflowUUID"] = cursor
		}
		if remaining > 0 {
			payload["limit"] = remaining
		}
		if len(payload) == 0 {
			payload["lastWorkflowUUID"] = ""
		}
		body, err := c.postRaw(ctx, c.ordersURL(), c.endpointURL("getPastOrdersV1"), payload, headers)
		if err != nil {
			return nil, err
		}
		responses = append(responses, browserpage.CapturedResponse{
			URL:         c.endpointURL("getPastOrdersV1"),
			Status:      http.StatusOK,
			ContentType: "application/json",
			Body:        body,
		})
		var count int
		cursor, count = nextPastCursor(body)
		if cursor != "" {
			if _, ok := seenCursors[cursor]; ok {
				return nil, fmt.Errorf("ubereats: repeated past order cursor %q", cursor)
			}
			seenCursors[cursor] = struct{}{}
			if count == 0 {
				return nil, fmt.Errorf("ubereats: cursor %q returned no orders", cursor)
			}
		}
		if limit > 0 {
			remaining -= count
			if remaining <= 0 {
				break
			}
		}
		if cursor == "" {
			break
		}
	}
	return responses, nil
}

func nextPastCursor(body string) (string, int) {
	var parsed struct {
		Data struct {
			OrderUUIDs     []string `json:"orderUuids"`
			PaginationData struct {
				NextCursor string `json:"nextCursor"`
			} `json:"paginationData"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return "", 0
	}
	return strings.TrimSpace(parsed.Data.PaginationData.NextCursor), len(parsed.Data.OrderUUIDs)
}

func (c *Client) locationHeaders(location Location) map[string]string {
	if location.Latitude == 0 && location.Longitude == 0 {
		return nil
	}
	lat := fmt.Sprintf("%.6f", location.Latitude)
	lng := fmt.Sprintf("%.6f", location.Longitude)
	return map[string]string{
		"x-uber-device-location-latitude":  lat,
		"x-uber-device-location-longitude": lng,
		"x-uber-target-location-latitude":  lat,
		"x-uber-target-location-longitude": lng,
	}
}

func (c *Client) endpointURL(name string) string {
	return c.BaseURL + "/_p/api/" + name
}

func (c *Client) ordersURL() string {
	return c.BaseURL + "/orders/"
}

func (c *Client) timeout() time.Duration {
	if c.Timeout <= 0 {
		return 2 * time.Minute
	}
	return c.Timeout
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: c.timeout()}
}

func (c *Client) ensureSession(ctx context.Context) error {
	if strings.TrimSpace(c.CookieHeader) != "" {
		return nil
	}
	if strings.TrimSpace(c.ProfileDir) == "" {
		return errors.New("ubereats: missing browser profile (run `ordercli ubereats login`)")
	}
	if c.ReadSession == nil {
		return errors.New("ubereats: session reader missing")
	}
	res, err := c.ReadSession(ctx, c.ordersURL(), browserpage.Options{
		Timeout:              c.timeout(),
		Headless:             true,
		LogWriter:            c.LogWriter,
		ProfileDir:           c.ProfileDir,
		WaitForURLSubstrings: []string{"/orders"},
	})
	if err != nil {
		return err
	}
	c.CookieHeader = strings.TrimSpace(res.CookieHeader)
	if c.CookieHeader == "" {
		return errors.New("ubereats: browser profile did not produce session cookies")
	}
	if c.UserAgent == "" {
		c.UserAgent = strings.TrimSpace(res.UserAgent)
	}
	return nil
}

func normalizeBaseURL(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://www.ubereats.com"
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(baseURL, "/")
	}
	if strings.EqualFold(u.Hostname(), "www.ubereats.com") {
		u.Scheme = "https"
		u.Host = u.Hostname()
		u.Path = ""
		u.RawQuery = ""
		u.Fragment = ""
		return strings.TrimRight(u.String(), "/")
	}
	return strings.TrimRight(baseURL, "/")
}

func withBaseHeaders(headers map[string]string) map[string]string {
	out := map[string]string{
		"content-type": "application/json",
		"x-csrf-token": "x",
	}
	for k, v := range headers {
		out[k] = v
	}
	return out
}

func filterOrders(orders []Order, filter OrderFilter) []Order {
	filtered := make([]Order, 0, len(orders))
	for _, order := range orders {
		switch filter {
		case OrderFilterActive:
			if !order.Active {
				continue
			}
		case OrderFilterPast:
			if order.Active {
				continue
			}
		}
		filtered = append(filtered, order)
	}
	return filtered
}

func sessionInfoFromLocation(location Location) SessionInfo {
	if location.Ref == "" && location.Source == "" && location.FullAddress == "" {
		return SessionInfo{}
	}
	return SessionInfo{
		LocationSource: location.Source,
		LocationRef:    location.Ref,
		Location:       location.FullAddress,
	}
}

func localTimezoneName() string {
	if name := time.Now().Location().String(); strings.TrimSpace(name) != "" {
		return name
	}
	return "UTC"
}

func (c *Client) traceRequest(requestURL string, payload []byte, headers http.Header, status int, body string) {
	if c.LogWriter == nil {
		return
	}
	redacted := map[string]string{}
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}
		value := values[0]
		switch strings.ToLower(key) {
		case "cookie", "x-csrf-token":
			value = "REDACTED"
		}
		redacted[key] = value
	}
	fmt.Fprintf(c.LogWriter, "ubereats request method=POST url=%s headers=%v body=%s\n", requestURL, redacted, strings.TrimSpace(string(payload)))
	fmt.Fprintf(c.LogWriter, "ubereats response status=%d url=%s body=%s\n", status, requestURL, strings.TrimSpace(body))
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func stringValue(v any) string {
	switch typed := v.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func floatValue(v any) float64 {
	switch typed := v.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		f, _ := typed.Float64()
		return f
	default:
		return 0
	}
}

func extractLocations(parsed map[string]any) []Location {
	data, _ := parsed["data"].(map[string]any)
	deliveryLocations, _ := data["deliveryLocations"].(map[string]any)
	out := make([]Location, 0)
	for _, source := range []string{"TARGET", "SAVED", "SUGGESTED"} {
		items, _ := deliveryLocations[source].([]any)
		for _, rawItem := range items {
			entry, _ := rawItem.(map[string]any)
			loc, _ := entry["location"].(map[string]any)
			coord, _ := loc["coordinate"].(map[string]any)
			personalization, _ := loc["personalization"].(map[string]any)
			title := firstNonEmptyString(
				stringValue(entry["title"]),
				stringValue(loc["title"]),
				stringValue(loc["name"]),
			)
			subtitle := firstNonEmptyString(
				stringValue(entry["subtitle"]),
				stringValue(loc["subtitle"]),
				stringValue(loc["addressLine2"]),
			)
			out = append(out, Location{
				Ref:         stringValue(loc["id"]),
				Source:      source,
				Label:       stringValue(personalization["label"]),
				Title:       title,
				FullAddress: firstNonEmptyString(stringValue(loc["fullAddress"]), joinNonEmpty(", ", title, subtitle), title),
				Latitude:    floatValue(coord["latitude"]),
				Longitude:   floatValue(coord["longitude"]),
				locationPayload: cloneMap(entry),
			})
		}
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func joinNonEmpty(sep string, values ...string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			filtered = append(filtered, value)
		}
	}
	return strings.Join(filtered, sep)
}

func instructionLocationPayload(location Location) map[string]any {
	entry := cloneMap(location.locationPayload)
	loc, _ := entry["location"].(map[string]any)
	if len(loc) == 0 {
		loc = map[string]any{}
	}
	title := firstNonEmptyString(
		stringValue(entry["title"]),
		stringValue(loc["title"]),
		stringValue(loc["name"]),
		location.Title,
	)
	subtitle := firstNonEmptyString(
		stringValue(entry["subtitle"]),
		stringValue(loc["subtitle"]),
		stringValue(loc["addressLine2"]),
	)
	personalization, _ := loc["personalization"].(map[string]any)
	label := firstNonEmptyString(stringValue(personalization["label"]), location.Label)
	return map[string]any{
		"address": map[string]any{
			"address1":             firstNonEmptyString(stringValue(loc["addressLine1"]), title),
			"address2":             firstNonEmptyString(stringValue(loc["addressLine2"]), subtitle),
			"aptOrSuite":           "",
			"eaterFormattedAddress": firstNonEmptyString(stringValue(loc["fullAddress"]), location.FullAddress, joinNonEmpty(", ", title, subtitle)),
			"subtitle":             subtitle,
			"title":                title,
			"uuid":                 "",
			"label":                label,
		},
		"latitude":          firstNonZeroFloat(floatValueFromNested(loc, "coordinate", "latitude"), location.Latitude),
		"longitude":         firstNonZeroFloat(floatValueFromNested(loc, "coordinate", "longitude"), location.Longitude),
		"reference":         firstNonEmptyString(stringValue(loc["id"]), location.Ref),
		"referenceType":     firstNonEmptyString(stringValue(loc["provider"]), "uber_places"),
		"type":              firstNonEmptyString(stringValue(loc["provider"]), "uber_places"),
		"addressComponents": normalizeAddressComponents(mapValue(loc["addressComponents"])),
		"categories":        sliceValue(loc["categories"]),
		"originType":        firstNonEmptyString(stringValue(loc["originType"]), "user_autocomplete"),
	}
}

func instructionFromMap(v any) Instruction {
	m, _ := v.(map[string]any)
	return Instruction{
		InteractionType: stringValue(m["interactionType"]),
		DisplayString:   stringValue(m["displayString"]),
		Notes:           stringValue(m["notes"]),
		AptOrSuite:      stringValue(m["aptOrSuite"]),
	}
}

func stringSliceValue(v any) []string {
	items, _ := v.([]any)
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s := stringValue(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sliceValue(v any) []any {
	items, _ := v.([]any)
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	out = append(out, items...)
	return out
}

func mapValue(v any) map[string]any {
	m, _ := v.(map[string]any)
	return cloneMap(m)
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func normalizeAddressComponents(components map[string]any) map[string]any {
	if len(components) == 0 {
		return nil
	}
	out := map[string]any{}
	for key, value := range components {
		switch key {
		case "CITY":
			out["city"] = value
		case "COUNTRY_CODE":
			out["countryCode"] = value
		case "FIRST_LEVEL_SUBDIVISION_CODE":
			out["firstLevelSubdivisionCode"] = value
		case "POSTAL_CODE":
			out["postalCode"] = value
		default:
			out[key] = value
		}
	}
	return out
}

func floatValueFromNested(m map[string]any, keys ...string) float64 {
	current := any(m)
	for _, key := range keys {
		next, _ := current.(map[string]any)
		current = next[key]
	}
	return floatValue(current)
}

func firstNonZeroFloat(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func uuidFromOrderRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		if built, err := BuildOrderURL("https://www.ubereats.com", ref); err == nil {
			ref = built
		}
	}
	parts := strings.Split(strings.Trim(ref, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "orders" {
			return parts[i+1]
		}
	}
	return ref
}
