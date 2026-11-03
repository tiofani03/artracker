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
	"github.com/joho/godotenv"

	"github.com/tiofani03/artracker/backend/internal/config"
	"github.com/tiofani03/artracker/backend/internal/db"
	"github.com/tiofani03/artracker/backend/internal/handler"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		log.Error("migration failed", "err", err)
		os.Exit(1)
	}

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
	if cfg.CORSOrigins != "" {
		app.Use(cors.New(cors.Config{
			AllowOrigins: cfg.CORSOrigins,
			AllowHeaders: "Origin, Content-Type, Accept, Authorization",
			AllowMethods: "GET, POST, PATCH, DELETE, OPTIONS",
		}))
	}

	h := handler.New(pool)
	app.Get("/healthz", h.Health)

	api := app.Group("/api/v1")
	api.Get("/artworks", h.ListArtworks)
	api.Post("/artworks", h.CreateArtwork)
	api.Get("/artworks/:id", h.GetArtwork)
	api.Patch("/artworks/:id", h.UpdateArtwork)
	api.Delete("/artworks/:id", h.DeleteArtwork)

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
	log.Info("bye")
}
