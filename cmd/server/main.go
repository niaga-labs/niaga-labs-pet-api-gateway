package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	gwconfig "github.com/Kilat-Pet-Delivery/api-gateway/internal/config"
	"github.com/Kilat-Pet-Delivery/api-gateway/internal/proxy"
	"github.com/Kilat-Pet-Delivery/api-gateway/internal/ratelimit"
	"github.com/Kilat-Pet-Delivery/api-gateway/internal/routes"
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
		ratelimit.PolicyMiddleware(),
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
	router.NoRoute(routes.NewNoRouteHandler(cfg.Upstreams, zapLogger))

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
