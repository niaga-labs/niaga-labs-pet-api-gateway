package routes

import (
	"net/http"
	"strings"

	"github.com/Kilat-Pet-Delivery/api-gateway/internal/proxy"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const (
	upstreamIdentity     = "identity"
	upstreamRunner       = "runner"
	upstreamBooking      = "booking"
	upstreamPayment      = "payment"
	upstreamTracking     = "tracking"
	upstreamNotification = "notification"
	upstreamReview       = "review"
	upstreamChat         = "chat"
	upstreamIncident     = "incident"
	upstreamLoyalty      = "loyalty"
	upstreamZones        = "zones"
)

// NewNoRouteHandler returns the gateway fallback router for proxied REST traffic.
func NewNoRouteHandler(upstreams map[string]string, logger *zap.Logger) gin.HandlerFunc {
	proxies := make(map[string]gin.HandlerFunc, len(upstreams))
	for name, target := range upstreams {
		proxies[name] = proxy.NewHTTPProxy(target, logger)
	}

	return func(c *gin.Context) {
		route, ok := RouteForPath(c.Request.URL.Path)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found", "success": false})
			return
		}

		handler, ok := proxies[route.Upstream]
		if !ok {
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream service unavailable", "success": false})
			return
		}

		originalPath := c.Request.URL.Path
		if route.RewritePath != "" {
			c.Request.URL.Path = route.RewritePath
			defer func() { c.Request.URL.Path = originalPath }()
		}
		handler(c)
	}
}

type RouteMatch struct {
	Upstream    string
	RewritePath string
}

// UpstreamForPath maps a public gateway path to a backend service key.
func UpstreamForPath(path string) (string, bool) {
	route, ok := RouteForPath(path)
	return route.Upstream, ok
}

// RouteForPath maps a public gateway path to a backend service and optional path rewrite.
func RouteForPath(path string) (RouteMatch, bool) {
	switch {
	case matchesIdentityRoute(path):
		return RouteMatch{Upstream: upstreamIdentity}, true
	case matchesRunnerRoute(path):
		return RouteMatch{Upstream: upstreamRunner}, true
	case matchesBookingRoute(path):
		return RouteMatch{Upstream: upstreamBooking}, true
	case matchesPaymentRoute(path):
		return RouteMatch{Upstream: upstreamPayment}, true
	case strings.HasPrefix(path, "/api/v1/tracking"):
		return RouteMatch{Upstream: upstreamTracking}, true
	case strings.HasPrefix(path, "/api/v1/notifications"):
		return RouteMatch{Upstream: upstreamNotification}, true
	case strings.HasPrefix(path, "/api/v1/reviews"):
		return RouteMatch{Upstream: upstreamReview}, true
	case strings.HasPrefix(path, "/api/v1/chat"):
		return RouteMatch{Upstream: upstreamTracking}, true
	case matchesChatRoute(path):
		return RouteMatch{Upstream: upstreamChat, RewritePath: rewriteV1ToAPIV1(path)}, true
	case matchesIncidentRoute(path):
		return RouteMatch{Upstream: upstreamIncident, RewritePath: rewriteAPIV1ToV1(path)}, true
	case matchesLoyaltyRoute(path):
		return RouteMatch{Upstream: upstreamLoyalty, RewritePath: rewriteAPIV1ToV1(path)}, true
	case matchesZonesRoute(path):
		return RouteMatch{Upstream: upstreamZones, RewritePath: rewriteAPIV1ToV1(path)}, true
	default:
		return RouteMatch{}, false
	}
}

func matchesIdentityRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/auth") ||
		strings.HasPrefix(path, "/api/v1/me") ||
		strings.HasPrefix(path, "/api/v1/agents") ||
		strings.HasPrefix(path, "/api/v1/admin/users") ||
		strings.HasPrefix(path, "/api/v1/admin/stats/users") ||
		strings.HasPrefix(path, "/api/v1/admin/documents")
}

func hasAnyPrefix(path string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func rewriteAPIV1ToV1(path string) string {
	if strings.HasPrefix(path, "/api/v1/") {
		return strings.TrimPrefix(path, "/api")
	}
	return ""
}

func rewriteV1ToAPIV1(path string) string {
	if strings.HasPrefix(path, "/v1/") {
		return "/api" + path
	}
	return ""
}

func matchesRunnerRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/runners") ||
		strings.HasPrefix(path, "/api/v1/petshops")
}

func matchesBookingRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/bookings") ||
		strings.HasPrefix(path, "/api/v1/pets") ||
		strings.HasPrefix(path, "/api/v1/admin/bookings") ||
		strings.HasPrefix(path, "/api/v1/admin/stats/bookings")
}

func matchesPaymentRoute(path string) bool {
	return strings.HasPrefix(path, "/api/v1/payments") ||
		strings.HasPrefix(path, "/api/v1/promos") ||
		strings.HasPrefix(path, "/api/v1/subscriptions") ||
		strings.HasPrefix(path, "/api/v1/payouts") ||
		strings.HasPrefix(path, "/api/v1/bank-accounts") ||
		strings.HasPrefix(path, "/api/v1/admin/payments") ||
		strings.HasPrefix(path, "/api/v1/admin/promos") ||
		strings.HasPrefix(path, "/api/v1/admin/stats/payments")
}
