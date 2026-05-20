package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	gwconfig "github.com/Kilat-Pet-Delivery/api-gateway/internal/config"
	"github.com/Kilat-Pet-Delivery/api-gateway/internal/proxy"
	"github.com/Kilat-Pet-Delivery/lib-common/auth"
	"github.com/Kilat-Pet-Delivery/lib-common/logger"
	"github.com/Kilat-Pet-Delivery/lib-common/middleware"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func main() {
	// 1. Load configuration
	cfg, err := gwconfig.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 2. Initialize logger
	env := cfg.AppEnv
	if env == "" {
		env = "development"
	}
	zapLogger, err := logger.NewNamed(env, "api-gateway")
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer func() { _ = zapLogger.Sync() }()

	zapLogger.Info("upstream configuration",
		zap.Any("upstreams", cfg.Upstreams),
		zap.String("port", cfg.Port),
		zap.Int("rate_limit_per_min", cfg.RateLimitPerMin),
	)

	// 3. Create Gin router with global middleware
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(
		middleware.RecoveryMiddleware(zapLogger),
		middleware.LoggerMiddleware(zapLogger),
		middleware.RequestIDMiddleware(),
		middleware.CORSMiddleware(),
		middleware.SecurityHeadersMiddleware(),
		middleware.RateLimitMiddleware(cfg.RateLimitPerMin, time.Minute),
	)

	// 4. Register aggregated health endpoint
	router.GET("/health", proxy.HealthAggregator(cfg.Upstreams, zapLogger))

	// 5. WebSocket route (must be explicit — WebSocket needs raw hijacking)
	router.GET("/ws/tracking/:bookingId", proxy.NewWebSocketProxy(cfg.Upstreams["tracking"], zapLogger))

	jwtManager := auth.NewJWTManager(
		cfg.JWTConfig.Secret,
		15*time.Minute,
		7*24*time.Hour,
	)
	chatRealtime := proxy.NewChatRealtime(
		cfg.Upstreams["chat"],
		cfg.KafkaConfig.Brokers,
		cfg.GatewayID,
		jwtManager,
		zapLogger,
	)
	chatRealtime.Start(appCtx)
	router.GET("/ws/chat", chatRealtime.HandleChatWS)
	router.GET("/ws/presence", chatRealtime.HandlePresenceWS)

	// 6. Proxy all /api/v1/* routes via NoRoute — avoids Gin trailing-slash redirects
	identityProxy := proxy.NewHTTPProxy(cfg.Upstreams["identity"], zapLogger)
	runnerProxy := proxy.NewHTTPProxy(cfg.Upstreams["runner"], zapLogger)
	bookingProxy := proxy.NewHTTPProxy(cfg.Upstreams["booking"], zapLogger)
	paymentProxy := proxy.NewHTTPProxy(cfg.Upstreams["payment"], zapLogger)
	trackingProxy := proxy.NewHTTPProxy(cfg.Upstreams["tracking"], zapLogger)
	notificationProxy := proxy.NewHTTPProxy(cfg.Upstreams["notification"], zapLogger)
	reviewProxy := proxy.NewHTTPProxy(cfg.Upstreams["review"], zapLogger)
	chatProxy := proxy.NewHTTPProxy(cfg.Upstreams["chat"], zapLogger)

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		switch {
		case strings.HasPrefix(path, "/api/v1/auth"):
			identityProxy(c)
		case strings.HasPrefix(path, "/api/v1/referrals"):
			identityProxy(c)
		case strings.HasPrefix(path, "/api/v1/runners"):
			runnerProxy(c)
		case strings.HasPrefix(path, "/api/v1/bookings"):
			bookingProxy(c)
		case strings.HasPrefix(path, "/api/v1/pets"):
			bookingProxy(c)
		case strings.HasPrefix(path, "/api/v1/payments"):
			paymentProxy(c)
		case strings.HasPrefix(path, "/api/v1/tracking"):
			trackingProxy(c)
		case strings.HasPrefix(path, "/api/v1/notifications"):
			notificationProxy(c)
		case strings.HasPrefix(path, "/api/v1/petshops"):
			runnerProxy(c)
		case strings.HasPrefix(path, "/api/v1/reviews"):
			reviewProxy(c)
		case strings.HasPrefix(path, "/api/v1/chat"):
			trackingProxy(c)
		case strings.HasPrefix(path, "/api/v1/threads"),
			strings.HasPrefix(path, "/api/v1/quick-replies"),
			strings.HasPrefix(path, "/api/v1/presence"):
			chatProxy(c)
		case strings.HasPrefix(path, "/api/v1/promos"):
			paymentProxy(c)
		case strings.HasPrefix(path, "/api/v1/subscriptions"):
			paymentProxy(c)
		case strings.HasPrefix(path, "/api/v1/admin/users"),
			strings.HasPrefix(path, "/api/v1/admin/stats/users"):
			identityProxy(c)
		case strings.HasPrefix(path, "/api/v1/admin/bookings"),
			strings.HasPrefix(path, "/api/v1/admin/stats/bookings"):
			bookingProxy(c)
		case strings.HasPrefix(path, "/api/v1/admin/payments"),
			strings.HasPrefix(path, "/api/v1/admin/promos"),
			strings.HasPrefix(path, "/api/v1/admin/stats/payments"):
			paymentProxy(c)
		default:
			c.JSON(http.StatusNotFound, gin.H{"error": "route not found", "success": false})
		}
	})

	// 7. Start HTTP server with extended timeouts for proxy/WebSocket
	srv := &http.Server{
		Addr:         cfg.Port,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		zapLogger.Info("starting api-gateway", zap.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			zapLogger.Fatal("server failed", zap.Error(err))
		}
	}()

	// 8. Graceful shutdown on SIGINT/SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	zapLogger.Info("shutting down api-gateway...")
	appCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		zapLogger.Fatal("server forced to shutdown", zap.Error(err))
	}

	zapLogger.Info("api-gateway stopped gracefully")
}
