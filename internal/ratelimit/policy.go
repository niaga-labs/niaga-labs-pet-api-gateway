package ratelimit

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Policy struct {
	Name    string
	Method  string
	Pattern string
	Limit   int
	Window  time.Duration
}

type Limiter struct {
	policies []Policy
	now      func() time.Time

	mu       sync.Mutex
	counters map[string]counter
}

type counter struct {
	windowStart time.Time
	count       int
}

func PolicyMiddleware() gin.HandlerFunc {
	return NewLimiter(DefaultPolicies(), time.Now).Middleware()
}

func DefaultPolicies() []Policy {
	return []Policy{
		{Name: "chat_send", Method: http.MethodPost, Pattern: "/api/v1/threads/:id/messages", Limit: 60, Window: time.Minute},
		{Name: "incident_create", Method: http.MethodPost, Pattern: "/api/v1/incidents", Limit: 10, Window: time.Minute},
		{Name: "quest_redemption", Method: http.MethodPost, Pattern: "/api/v1/quests/:id/redeem", Limit: 3, Window: time.Minute},
		{Name: "document_upload", Method: http.MethodPost, Pattern: "/api/v1/me/documents", Limit: 5, Window: time.Hour},
	}
}

func NewLimiter(policies []Policy, now func() time.Time) *Limiter {
	return &Limiter{
		policies: policies,
		now:      now,
		counters: make(map[string]counter),
	}
}

func (l *Limiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		policy, ok := l.match(c.Request.Method, c.Request.URL.Path)
		if !ok {
			c.Next()
			return
		}

		if !l.allow(policy, c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error":   "rate limit exceeded",
				"success": false,
			})
			return
		}
		c.Next()
	}
}

func (l *Limiter) match(method, path string) (Policy, bool) {
	for _, policy := range l.policies {
		if method == policy.Method && matchPath(policy.Pattern, path) {
			return policy, true
		}
	}
	return Policy{}, false
}

func (l *Limiter) allow(policy Policy, clientID string) bool {
	now := l.now()
	key := policy.Name + ":" + clientID

	l.mu.Lock()
	defer l.mu.Unlock()

	current := l.counters[key]
	if current.windowStart.IsZero() || now.Sub(current.windowStart) >= policy.Window {
		l.counters[key] = counter{windowStart: now, count: 1}
		return true
	}

	if current.count >= policy.Limit {
		return false
	}
	current.count++
	l.counters[key] = current
	return true
}

func matchPath(pattern, path string) bool {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		if strings.HasPrefix(patternParts[i], ":") {
			continue
		}
		if patternParts[i] != pathParts[i] {
			return false
		}
	}
	return true
}
