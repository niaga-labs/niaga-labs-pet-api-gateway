package routes

func matchesLoyaltyRoute(path string) bool {
	return hasAnyPrefix(path,
		"/api/v1/quests",
		"/api/v1/tier",
		"/api/v1/referrals",
		"/v1/quests",
		"/v1/tier",
		"/v1/referrals",
	)
}
