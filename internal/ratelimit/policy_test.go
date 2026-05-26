package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestChatSendPolicyBlocksAtSixtyFirstRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	limiter := NewLimiter(DefaultPolicies(), func() time.Time { return now })

	router := gin.New()
	router.Use(limiter.Middleware())
	router.POST("/api/v1/threads/:id/messages", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for i := 1; i <= 60; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/threads/thread-1/messages", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("request %d status = %d, want %d", i, rec.Code, http.StatusNoContent)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/threads/thread-1/messages", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestWithdrawRateLimitPerShopNotPerUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	limiter := NewLimiter(DefaultPolicies(), func() time.Time { return now })

	router := gin.New()
	router.Use(limiter.Middleware())
	router.POST("/api/v1/payments/shops/:shopId/withdrawals", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	for i := 1; i <= 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shops/shop-1/withdrawals", nil)
		req.RemoteAddr = "192.0.2.10:1234"
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("shop-1 request %d status = %d, want %d", i, rec.Code, http.StatusNoContent)
		}
	}

	blocked := httptest.NewRecorder()
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shops/shop-1/withdrawals", nil)
	blockedReq.RemoteAddr = "192.0.2.11:1234"
	router.ServeHTTP(blocked, blockedReq)
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("same shop status = %d, want %d", blocked.Code, http.StatusTooManyRequests)
	}

	allowed := httptest.NewRecorder()
	allowedReq := httptest.NewRequest(http.MethodPost, "/api/v1/payments/shops/shop-2/withdrawals", nil)
	allowedReq.RemoteAddr = "192.0.2.11:1234"
	router.ServeHTTP(allowed, allowedReq)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("different shop status = %d, want %d", allowed.Code, http.StatusNoContent)
	}
}
