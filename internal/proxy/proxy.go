package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	gatewaymiddleware "github.com/Kilat-Pet-Delivery/api-gateway/internal/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// NewHTTPProxy creates a reverse proxy handler that forwards requests to the target URL.
// The pathPrefix is stripped before forwarding (e.g., "/api/v1/auth" strips nothing since
// downstream services already expect /api/v1/auth paths).
func NewHTTPProxy(targetURL string, logger *zap.Logger) gin.HandlerFunc {
	target, err := url.Parse(targetURL)
	if err != nil {
		logger.Fatal("invalid upstream URL", zap.String("url", targetURL), zap.Error(err))
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host

			// Preserve the original path — downstream services expect the full /api/v1/* path
			// req.URL.Path is already set by Gin

			// Forward X-Forwarded headers
			if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
				prior := req.Header.Get("X-Forwarded-For")
				if prior != "" {
					clientIP = prior + ", " + clientIP
				}
				req.Header.Set("X-Forwarded-For", clientIP)
			}
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Forwarded-Proto", "http")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Error("proxy error",
				zap.String("upstream", targetURL),
				zap.String("path", r.URL.Path),
				zap.Error(err),
			)
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(`{"error":"upstream service unavailable","success":false}`))
		},
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 25,
			IdleConnTimeout:     90 * time.Second,
		},
		ModifyResponse: gatewaymiddleware.StripQRPickupToken,
	}

	return func(c *gin.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}

// NewWebSocketProxy creates a handler that tunnels WebSocket connections to the upstream.
// It uses raw TCP hijacking to avoid double-upgrade — the upstream service-tracking
// handles the actual WebSocket upgrade via gorilla/websocket.
func NewWebSocketProxy(targetURL string, logger *zap.Logger) gin.HandlerFunc {
	target, err := url.Parse(targetURL)
	if err != nil {
		logger.Fatal("invalid upstream WebSocket URL", zap.String("url", targetURL), zap.Error(err))
	}

	return func(c *gin.Context) {
		// Build the upstream URL preserving path and query params (including ?token=)
		upstreamURL := *target
		upstreamURL.Path = c.Request.URL.Path
		upstreamURL.RawQuery = c.Request.URL.RawQuery

		// Dial the upstream service
		upstreamConn, err := net.DialTimeout("tcp", target.Host, 10*time.Second)
		if err != nil {
			logger.Error("failed to dial upstream for WebSocket",
				zap.String("host", target.Host),
				zap.Error(err),
			)
			c.JSON(http.StatusBadGateway, gin.H{"error": "upstream service unavailable"})
			return
		}

		// Hijack the client connection
		hijacker, ok := c.Writer.(http.Hijacker)
		if !ok {
			upstreamConn.Close()
			c.JSON(http.StatusInternalServerError, gin.H{"error": "WebSocket hijack not supported"})
			return
		}

		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			upstreamConn.Close()
			logger.Error("failed to hijack client connection", zap.Error(err))
			return
		}

		// Forward the original HTTP request to upstream (so it can do the WS upgrade)
		reqLine := c.Request.Method + " " + upstreamURL.RequestURI() + " HTTP/1.1\r\n"
		clientConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		upstreamConn.Write([]byte(reqLine))

		// Forward all headers
		for key, values := range c.Request.Header {
			for _, value := range values {
				upstreamConn.Write([]byte(key + ": " + value + "\r\n"))
			}
		}
		// Override Host header
		upstreamConn.Write([]byte("Host: " + target.Host + "\r\n"))
		upstreamConn.Write([]byte("\r\n"))

		// Clear deadlines for long-lived WebSocket connection
		clientConn.SetDeadline(time.Time{})
		upstreamConn.SetDeadline(time.Time{})

		// Bidirectional pipe
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			io.Copy(upstreamConn, clientConn)
			upstreamConn.Close()
		}()

		go func() {
			defer wg.Done()
			io.Copy(clientConn, upstreamConn)
			clientConn.Close()
		}()

		wg.Wait()
	}
}

// HealthAggregator creates a handler that checks all upstream services' health concurrently.
func HealthAggregator(upstreams map[string]string, logger *zap.Logger) gin.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}

	return func(c *gin.Context) {
		type result struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error,omitempty"`
		}

		var mu sync.Mutex
		var wg sync.WaitGroup
		results := make([]result, 0, len(upstreams))
		allHealthy := true

		for name, baseURL := range upstreams {
			wg.Add(1)
			go func(name, baseURL string) {
				defer wg.Done()

				healthURL := strings.TrimRight(baseURL, "/") + "/health"
				resp, err := client.Get(healthURL)

				r := result{Name: name}
				if err != nil {
					r.Status = "unhealthy"
					r.Error = err.Error()
					mu.Lock()
					allHealthy = false
					results = append(results, r)
					mu.Unlock()
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusOK {
					r.Status = "healthy"
				} else {
					r.Status = "unhealthy"
					r.Error = resp.Status
					mu.Lock()
					allHealthy = false
					mu.Unlock()
				}

				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}(name, baseURL)
		}

		wg.Wait()

		status := "healthy"
		httpCode := http.StatusOK
		if !allHealthy {
			status = "degraded"
			httpCode = http.StatusServiceUnavailable
		}

		c.JSON(httpCode, gin.H{
			"status":   status,
			"service":  "api-gateway",
			"services": results,
		})
	}
}
