package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestRouteForPathMapsNewServices(t *testing.T) {
	tests := []struct {
		path        string
		want        string
		wantRewrite string
	}{
		{path: "/api/v1/threads/thread-1/messages", want: upstreamChat, wantRewrite: ""},
		{path: "/v1/threads/thread-1/messages", want: upstreamChat, wantRewrite: "/api/v1/threads/thread-1/messages"},
		{path: "/api/v1/incidents", want: upstreamIncident, wantRewrite: "/v1/incidents"},
		{path: "/v1/incidents/incident-1", want: upstreamIncident, wantRewrite: ""},
		{path: "/api/v1/quests/quest-1/redeem", want: upstreamLoyalty, wantRewrite: "/v1/quests/quest-1/redeem"},
		{path: "/api/v1/zones/at", want: upstreamZones, wantRewrite: "/v1/zones/at"},
		{path: "/api/v1/shops/shop-1/products", want: upstreamShop, wantRewrite: ""},
		{path: "/api/v1/staff-invites/token/preview", want: upstreamShop, wantRewrite: ""},
		{path: "/api/v1/identity/shops/me", want: upstreamIdentity, wantRewrite: ""},
		{path: "/api/v1/payments/shops/shop-1/wallet", want: upstreamPayment, wantRewrite: ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, ok := RouteForPath(tt.path)
			if !ok {
				t.Fatalf("RouteForPath(%q) returned no match", tt.path)
			}
			if got.Upstream != tt.want {
				t.Fatalf("upstream = %q, want %q", got.Upstream, tt.want)
			}
			if got.RewritePath != tt.wantRewrite {
				t.Fatalf("rewrite = %q, want %q", got.RewritePath, tt.wantRewrite)
			}
		})
	}
}

func TestNoRouteProxiesIncidentWithPathRewrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
	}))
	defer upstream.Close()

	router := gin.New()
	router.NoRoute(NewNoRouteHandler(map[string]string{
		upstreamIncident: upstream.URL,
	}, zap.NewNop()))

	rec := newCloseNotifyRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/incidents", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if upstreamPath != "/v1/incidents" {
		t.Fatalf("upstream path = %q, want /v1/incidents", upstreamPath)
	}
}

func TestNoRouteProxiesChatV1ToServiceChatAPIPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var upstreamPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	router := gin.New()
	router.NoRoute(NewNoRouteHandler(map[string]string{
		upstreamChat: upstream.URL,
	}, zap.NewNop()))

	rec := newCloseNotifyRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/threads/thread-1/messages", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if upstreamPath != "/api/v1/threads/thread-1/messages" {
		t.Fatalf("upstream path = %q, want /api/v1/threads/thread-1/messages", upstreamPath)
	}
}

type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	ch chan bool
}

func newCloseNotifyRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		ch:               make(chan bool, 1),
	}
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.ch
}
