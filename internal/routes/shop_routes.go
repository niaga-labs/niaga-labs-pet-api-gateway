package routes

import "strings"

func matchesShopRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/shops") ||
		strings.HasPrefix(path, "/api/v1/staff-invites")
}

func matchesShopIdentityRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/identity/shops")
}

func matchesShopPaymentRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/payments/shops") ||
		strings.HasPrefix(path, "/api/v1/payments/bank-accounts")
}
