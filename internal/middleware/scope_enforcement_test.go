package middleware

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireMerchantOfShopAllowsOwnerOfShop(t *testing.T) {
	rec := exerciseScopeMiddleware("/api/v1/shops/shop-1/products", bearerToken(map[string]any{
		"shop_roles": map[string]string{"shop-1": ShopRoleOwner},
	}))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestRequireMerchantOfShopRejectsOwnerOfDifferentShop(t *testing.T) {
	rec := exerciseScopeMiddleware("/api/v1/shops/shop-2/products", bearerToken(map[string]any{
		"shop_roles": map[string]string{"shop-1": ShopRoleOwner},
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestRequireShopOwnerRejectsShopManager(t *testing.T) {
	rec := exerciseScopeMiddleware("/api/v1/shops/shop-1/staff", bearerToken(map[string]any{
		"shop_roles": map[string]string{"shop-1": ShopRoleManager},
	}))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func exerciseScopeMiddleware(path, token string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if !EnforceShopScope(c) {
			return
		}
		c.Next()
	})
	router.GET("/api/v1/shops/:shopId/products", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/v1/shops/:shopId/staff", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", token)
	router.ServeHTTP(rec, req)
	return rec
}

func bearerToken(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "none", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	return "Bearer " + base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}
