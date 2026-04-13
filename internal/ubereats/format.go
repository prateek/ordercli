package ubereats

import (
	"fmt"
	"strings"
)

func (o Order) Summary() string {
	parts := make([]string, 0, 8)
	appendPart := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		parts = append(parts, key+"="+value)
	}

	appendPart("uuid", o.UUID)
	appendPart("number", o.OrderNumber)
	appendPart("merchant", o.Merchant)
	appendPart("status", o.Status)
	appendPart("eta", o.ETA)
	appendPart("total", o.Total)
	appendPart("occurred_at", o.OccurredAt)

	if len(parts) == 0 {
		return "order"
	}
	return strings.Join(parts, " ")
}

func (o Order) DetailsString() string {
	lines := make([]string, 0, 12+len(o.Items))
	appendLine := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, fmt.Sprintf("%s=%s", key, value))
	}

	appendLine("merchant", o.Merchant)
	appendLine("uuid", o.UUID)
	appendLine("number", o.OrderNumber)
	appendLine("status", o.Status)
	appendLine("detail", o.StatusDetail)
	appendLine("eta", o.ETA)
	appendLine("occurred_at", o.OccurredAt)
	appendLine("total", o.Total)
	appendLine("courier", o.Courier)
	appendLine("store_address", o.StoreAddress)
	appendLine("location_source", o.SessionInfo.LocationSource)
	appendLine("location_ref", o.SessionInfo.LocationRef)
	appendLine("location", o.SessionInfo.Location)
	appendLine("profile", o.SessionInfo.Profile)
	appendLine("payment_profile_ref", o.SessionInfo.PaymentProfileRef)
	appendLine("url", o.URL)
	if len(o.Items) > 0 {
		lines = append(lines, "items:")
		for _, item := range o.Items {
			lines = append(lines, "  "+item)
		}
	}
	return strings.Join(lines, "\n")
}
