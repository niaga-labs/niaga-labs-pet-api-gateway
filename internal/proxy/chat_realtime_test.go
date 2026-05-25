package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kilat-Pet-Delivery/lib-common/auth"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestHandleChatWSRejectsMissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	realtime := NewChatRealtime(
		"http://service-chat:8008",
		nil,
		"gateway-test",
		auth.NewJWTManager("test-secret", 0, 0),
		zap.NewNop(),
	)

	router := gin.New()
	router.GET("/ws/chat", realtime.HandleChatWS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws/chat", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
