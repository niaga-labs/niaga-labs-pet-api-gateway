package config

import (
	"fmt"

	"github.com/Kilat-Pet-Delivery/lib-common/config"
)

// Upstream holds the name and URL for a backend service.
type Upstream struct {
	Name string
	URL  string
}

// GatewayConfig holds the API gateway configuration.
type GatewayConfig struct {
	Port            string
	AppEnv          string
	RateLimitPerMin int
	Upstreams       map[string]string
}

// Load reads gateway configuration from environment variables.
func Load() (*GatewayConfig, error) {
	v, err := config.Load("gateway")
	if err != nil {
		return nil, err
	}

	port := v.GetInt("GATEWAY_PORT")
	if port == 0 {
		port = 8080
	}

	rateLimit := v.GetInt("RATE_LIMIT_PER_MIN")
	if rateLimit == 0 {
		rateLimit = 200
	}

	upstreams := map[string]string{
		"identity":     getUpstream(v.GetString("UPSTREAM_IDENTITY"), "http://service-identity:8004"),
		"runner":       getUpstream(v.GetString("UPSTREAM_RUNNER"), "http://service-runner:8003"),
		"booking":      getUpstream(v.GetString("UPSTREAM_BOOKING"), "http://service-booking:8001"),
		"payment":      getUpstream(v.GetString("UPSTREAM_PAYMENT"), "http://service-payment:8002"),
		"tracking":     getUpstream(v.GetString("UPSTREAM_TRACKING"), "http://service-tracking:8005"),
		"notification": getUpstream(v.GetString("UPSTREAM_NOTIFICATION"), "http://service-notification:8006"),
		"review":       getUpstream(v.GetString("UPSTREAM_REVIEW"), "http://service-review:8007"),
	}

	return &GatewayConfig{
		Port:            fmt.Sprintf(":%d", port),
		AppEnv:          v.GetString("APP_ENV"),
		RateLimitPerMin: rateLimit,
		Upstreams:       upstreams,
	}, nil
}

func getUpstream(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}
