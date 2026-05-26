package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BookingShopScope enforces merchant scope for shop booking transitions when
// the request body carries the target shop_id.
func BookingShopScope(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid body", "success": false})
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		var payload struct {
			ShopID string `json:"shop_id"`
		}
		if err := json.Unmarshal(body, &payload); err != nil || payload.ShopID == "" {
			c.Next()
			return
		}

		if AuthorizesShopRole(c.GetHeader("Authorization"), payload.ShopID, allowedRoles...) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden_for_role", "success": false})
	}
}
