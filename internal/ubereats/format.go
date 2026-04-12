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
	appendLine("total", o.Total)
	appendLine("courier", o.Courier)
	appendLine("store_address", o.StoreAddress)
	appendLine("url", o.URL)
	if len(o.Items) > 0 {
		lines = append(lines, "items:")
		for _, item := range o.Items {
			lines = append(lines, "  "+item)
		}
	}
	return strings.Join(lines, "\n")
}
