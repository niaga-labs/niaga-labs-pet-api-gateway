package middleware

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func StripQRPickupToken(resp *http.Response) error {
	if resp == nil || resp.Body == nil || !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	if canSeeQRPickupToken(resp.Request, payload) {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	stripKey(payload, "qr_pickup_token")
	filtered, err := json.Marshal(payload)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return nil
	}

	resp.Body = io.NopCloser(bytes.NewReader(filtered))
	resp.ContentLength = int64(len(filtered))
	resp.Header.Set("Content-Length", strconv.Itoa(len(filtered)))
	return nil
}

func canSeeQRPickupToken(req *http.Request, payload any) bool {
	if req == nil {
		return false
	}
	if roleFromJWT(req.Header.Get("Authorization")) == "runner" {
		return true
	}

	shopID, ok := findStringKey(payload, "shop_id")
	if !ok || shopID == "" {
		return false
	}
	return AuthorizesShopRole(req.Header.Get("Authorization"), shopID, ShopRoleOwner, ShopRoleManager, ShopRoleStaff)
}

func roleFromJWT(authHeader string) string {
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	tokenParts := strings.Split(parts[1], ".")
	if len(tokenParts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(tokenParts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	role, _ := claims["role"].(string)
	return role
}

func stripKey(value any, key string) {
	switch typed := value.(type) {
	case map[string]any:
		delete(typed, key)
		for _, child := range typed {
			stripKey(child, key)
		}
	case []any:
		for _, child := range typed {
			stripKey(child, key)
		}
	}
}

func findStringKey(value any, key string) (string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if value, ok := typed[key].(string); ok {
			return value, true
		}
		for _, child := range typed {
			if value, ok := findStringKey(child, key); ok {
				return value, true
			}
		}
	case []any:
		for _, child := range typed {
			if value, ok := findStringKey(child, key); ok {
				return value, true
			}
		}
	}
	return "", false
}
