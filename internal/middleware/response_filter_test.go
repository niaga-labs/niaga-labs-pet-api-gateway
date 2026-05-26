package middleware

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestStripsQRTokenForNonMerchantNonRunner(t *testing.T) {
	resp := jsonResponse(`{"id":"booking-1","shop_id":"shop-1","qr_pickup_token":"secret"}`, "")
	if err := StripQRPickupToken(resp); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "qr_pickup_token") {
		t.Fatalf("qr token still present: %s", body)
	}
}

func TestRetainsQRTokenForAssignedRunner(t *testing.T) {
	resp := jsonResponse(`{"id":"booking-1","shop_id":"shop-1","qr_pickup_token":"secret"}`, bearerToken(map[string]any{"role": "runner"}))
	if err := StripQRPickupToken(resp); err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "qr_pickup_token") {
		t.Fatalf("qr token stripped for runner: %s", body)
	}
}

func jsonResponse(body, token string) *http.Response {
	req, _ := http.NewRequest(http.MethodGet, "/api/v1/bookings/booking-1", nil)
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
