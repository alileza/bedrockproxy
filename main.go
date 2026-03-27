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

	"github.com/jackc/pgx/v5/pgxpool"

	"bedrockproxy/internal/api"
	"bedrockproxy/internal/config"
	"bedrockproxy/internal/database"
	"bedrockproxy/internal/proxy"
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

	pool, err := database.Connect(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	if err := seedModels(ctx, pool, cfg.Models); err != nil {
		return fmt.Errorf("seed models: %w", err)
	}

	tracker := usage.NewTracker(pool, cfg.Models)

	p, err := proxy.New(ctx, cfg.AWS.Region, tracker)
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}

	router := api.NewRouter(pool, p)

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

func seedModels(ctx context.Context, pool *pgxpool.Pool, models []config.ModelConfig) error {
	for _, m := range models {
		_, err := pool.Exec(ctx, `
			INSERT INTO models (id, name, input_price_per_million, output_price_per_million, enabled)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				input_price_per_million = EXCLUDED.input_price_per_million,
				output_price_per_million = EXCLUDED.output_price_per_million,
				enabled = EXCLUDED.enabled,
				updated_at = NOW()
		`, m.ID, m.Name, m.InputPricePerMillion, m.OutputPricePerMillion, m.Enabled)
		if err != nil {
			return fmt.Errorf("seed model %s: %w", m.ID, err)
		}
	}
	return nil
}
