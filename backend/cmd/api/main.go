package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/tiofani03/artracker/backend/internal/auth"
	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/db"
	"github.com/tiofani03/artracker/backend/internal/handler"
	"github.com/tiofani03/artracker/backend/internal/ingest"
	"github.com/tiofani03/artracker/backend/internal/storage"
)

func main() {
	_ = godotenv.Load("../.env", ".env")

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx := context.Background()

	var pool *pgxpool.Pool
	if cfg.SkipDB {
		log.Warn("database disabled (SKIP_DB=true), using in-memory store")
	} else {
		var err error
		pool, err = db.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns)
		if err != nil {
			log.Error("database connect failed", "err", err)
			os.Exit(1)
		}
		defer pool.Close()

		if err := db.Migrate(ctx, pool); err != nil {
			log.Error("migration failed", "err", err)
			os.Exit(1)
		}
	}

	ingestSvc := ingest.NewService(ingest.Config{
		Workers:         cfg.IngestWorkers,
		QueueSize:       cfg.IngestQueueSize,
		BatchSize:       cfg.IngestBatchSize,
		FlushIntervalMs: cfg.IngestFlushIntervalMs,
		ProjectRPS:      cfg.IngestProjectRPS,
		ProjectEventsPS: cfg.IngestProjectEventsPerSec,
		KeyCacheTTLSec:  cfg.IngestKeyCacheTTLSec,
	}, pool)

	app := fiber.New(fiber.Config{
		AppName:               "artracker-api",
		ReadTimeout:           10 * time.Second,
		WriteTimeout:          15 * time.Second,
		IdleTimeout:           60 * time.Second,
		DisableStartupMessage: true,
		ErrorHandler:          handler.ErrorHandler,
	})

	app.Use(recover.New())
	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "${time} ${status} ${latency} ${method} ${path}\n",
	}))

	// SDK-specific CORS (allows client SDKs from any origin)
	app.Use("/api/v1/ingest", cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Content-Encoding, Accept, X-Artracker-Key",
		AllowMethods: "POST, OPTIONS",
	}))
	app.Use("/api/v1/replay", cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Content-Encoding, Accept, X-Artracker-Key",
		AllowMethods: "POST, OPTIONS",
	}))
	app.Use("/api/v1/sdk", cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, X-Artracker-Key",
		AllowMethods: "GET, OPTIONS",
	}))

	if cfg.CORSOrigins != "" {
		app.Use(cors.New(cors.Config{
			AllowOrigins:     cfg.CORSOrigins,
			AllowCredentials: cfg.CORSOrigins != "*",
			AllowHeaders:     "Origin, Content-Type, Accept, Authorization",
			AllowMethods:     "GET, POST, PATCH, DELETE, OPTIONS",
		}))
	}

	var replayStore storage.ReplayStore
	if cfg.R2BucketName != "" {
		store, err := storage.NewR2(storage.Config{
			AccountID:       cfg.R2AccountID,
			AccessKeyID:     cfg.R2AccessKeyID,
			AccessKeySecret: cfg.R2AccessKeySecret,
			BucketName:      cfg.R2BucketName,
			Endpoint:        cfg.R2Endpoint,
			Region:          cfg.R2Region,
		})
		if err != nil {
			log.Warn("r2 storage init failed", "err", err)
		} else {
			replayStore = store
			log.Info("session replay storage configured", "bucket", cfg.R2BucketName)
		}
	} else {
		log.Warn("r2 storage not configured (R2_BUCKET_NAME is empty)")
	}

	providers := map[string]handler.OAuthProvider{}
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		providers["google"] = handler.NewGoogleProvider(cfg.GoogleClientID, cfg.GoogleClientSecret)
	}

	h := handler.New(pool, cfg, auth.NewIssuer(cfg.JWTSecret, 15*time.Minute), providers).
		WithIngest(ingestSvc).
		WithReplayStore(replayStore)
	ingestSvc.WithIssueProcessor(h.IssueService())
	app.Get("/healthz", h.Health)
	app.Get("/auth/callback/:provider", h.OAuthCallback)
	h.Routes(app.Group("/api/v1"))

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Info("http server stopped", "err", err)
		}
	}()
	log.Info("api listening", "port", cfg.Port, "env", cfg.Env)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
	}
	if err := ingestSvc.Shutdown(shutdownCtx); err != nil {
		log.Error("ingest drain failed", "err", err)
	}
	log.Info("bye")
}
