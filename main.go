package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bedrockproxy/internal/api"
	"bedrockproxy/internal/auth"
	"bedrockproxy/internal/config"
	"bedrockproxy/internal/proxy"
	"bedrockproxy/internal/store"
	"bedrockproxy/internal/usage"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	if err := run(*configPath); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	s := store.New(cfg.Models)

	flusher, err := store.NewS3Flusher(ctx, s, cfg.S3.Bucket, cfg.S3.Prefix, cfg.S3.FlushInterval, cfg.AWS.Region)
	if err != nil {
		return fmt.Errorf("create s3 flusher: %w", err)
	}
	flusher.Start(ctx)

	events := api.NewEventBus()

	tracker := usage.NewTracker(s, cfg.Models)
	tracker.Notify = events.NotifyFunc()

	resolver, err := auth.NewResolver(ctx, cfg.AWS.Region, s)
	if err != nil {
		return fmt.Errorf("create resolver: %w", err)
	}

	p, err := proxy.New(ctx, cfg.AWS.Region, tracker, resolver)
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}

	router := api.NewRouter(s, p, resolver, events)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down server")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	slog.Info("starting bedrockproxy", "port", cfg.Server.Port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}
