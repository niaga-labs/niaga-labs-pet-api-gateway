package routes

func matchesZonesRoute(path string) bool {
	return hasAnyPrefix(path, "/api/v1/zones", "/v1/zones")
}
