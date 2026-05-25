package routes

func matchesChatRoute(path string) bool {
	return hasAnyPrefix(path,
		"/api/v1/threads",
		"/api/v1/quick-replies",
		"/api/v1/presence",
		"/v1/threads",
		"/v1/quick-replies",
		"/v1/presence",
	)
}
