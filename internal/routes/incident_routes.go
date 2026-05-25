package routes

func matchesIncidentRoute(path string) bool {
	return hasAnyPrefix(path, "/api/v1/incidents", "/v1/incidents")
}
