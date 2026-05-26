package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	ShopRoleOwner   = "owner"
	ShopRoleManager = "manager"
	ShopRoleStaff   = "staff"
)

type ShopRoles map[string]string

func RequireShopOwner() gin.HandlerFunc {
	return RequireMerchantOfShop(ShopRoleOwner)
}

func RequireMerchantOfShop(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		shopID := c.Param("shopId")
		if shopID == "" {
			shopID = c.Param("id")
		}
		if shopID == "" || AuthorizesShopRole(c.GetHeader("Authorization"), shopID, allowedRoles...) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden_for_role", "success": false})
	}
}

func EnforceShopScope(c *gin.Context) bool {
	path := c.Request.URL.Path
	method := c.Request.Method

	if isStaffInvitePreview(path, method) || isCreateShop(path, method) || isInviteAccept(path) {
		return true
	}

	shopID, ok := ShopIDFromPath(path)
	if !ok {
		return true
	}

	allowed := []string{ShopRoleOwner, ShopRoleManager, ShopRoleStaff}
	if requiresShopOwner(path, method) {
		allowed = []string{ShopRoleOwner}
	}
	if AuthorizesShopRole(c.GetHeader("Authorization"), shopID, allowed...) {
		return true
	}

	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden_for_role", "success": false})
	return false
}

func AuthorizesShopRole(authHeader, shopID string, allowedRoles ...string) bool {
	roles := ShopRolesFromAuthHeader(authHeader)
	role, ok := roles[shopID]
	if !ok {
		return false
	}
	for _, allowed := range allowedRoles {
		if role == allowed {
			return true
		}
	}
	return false
}

func ShopRolesFromAuthHeader(authHeader string) ShopRoles {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return nil
	}
	return ShopRolesFromJWT(parts[1])
}

func ShopRolesFromJWT(token string) ShopRoles {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return normalizeShopRoles(claims["shop_roles"])
}

func ShopIDFromPath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "shops" && parts[i+1] != "" && parts[i+1] != "me" {
			return parts[i+1], true
		}
	}
	return "", false
}

func normalizeShopRoles(raw any) ShopRoles {
	roles := ShopRoles{}
	switch v := raw.(type) {
	case map[string]any:
		for shopID, role := range v {
			if roleText, ok := role.(string); ok {
				roles[shopID] = roleText
			}
		}
	case []any:
		for _, item := range v {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			shopID, _ := row["shop_id"].(string)
			role, _ := row["role"].(string)
			if shopID != "" && role != "" {
				roles[shopID] = role
			}
		}
	}
	if len(roles) == 0 {
		return nil
	}
	return roles
}

func isStaffInvitePreview(path, method string) bool {
	return method == http.MethodGet && strings.HasPrefix(path, "/api/v1/staff-invites/")
}

func isInviteAccept(path string) bool {
	return path == "/api/v1/staff-invites/accept"
}

func isCreateShop(path, method string) bool {
	return path == "/api/v1/shops" && method == http.MethodPost
}

func requiresShopOwner(path, method string) bool {
	if strings.Contains(path, "/staff") {
		return true
	}
	if strings.HasSuffix(path, "/status") || (method == http.MethodPatch && !strings.Contains(path, "/products")) {
		return true
	}
	if strings.Contains(path, "/withdrawals") && method == http.MethodPost {
		return true
	}
	return false
}
