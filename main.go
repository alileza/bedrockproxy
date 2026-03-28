package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bedrockproxy/internal/api"
	"bedrockproxy/internal/config"
	"bedrockproxy/internal/pricing"
	"bedrockproxy/internal/proxy"
	"bedrockproxy/internal/quota"
	"bedrockproxy/internal/store"
	"bedrockproxy/internal/usage"
)

//go:generate sh -c "if command -v pnpm >/dev/null 2>&1; then cd web && pnpm install && pnpm run build && cd .. && rm -rf dist && cp -r web/dist dist; elif [ ! -d dist ]; then mkdir -p dist && echo '<!DOCTYPE html><html><body>frontend not built</body></html>' > dist/index.html; fi"
//go:embed all:dist
var embeddedFrontend embed.FS

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

	// Auto-discover model pricing from AWS (non-blocking on failure).
	go discoverPricing(ctx, cfg, s, tracker)

	quotaEngine := quota.NewEngine(s, cfg.Quotas)

	p, err := proxy.New(ctx, cfg.AWS.Region, tracker, proxy.WithQuotaEngine(quotaEngine))
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}

	router := api.NewRouter(s, p, events, api.WithQuotaEngine(quotaEngine))

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

// discoverPricing fetches model pricing from AWS and merges it with config.
// Config-specified models always take precedence over auto-discovered ones.
func discoverPricing(ctx context.Context, cfg *config.Config, s *store.Store, tracker *usage.Tracker) {
	discovered := pricing.Fetch(ctx, cfg.AWS.Region)
	if len(discovered) == 0 {
		slog.Info("pricing unavailable, cost tracking uses config only")
		return
	}

	// Build a set of config model IDs for precedence check.
	configIDs := make(map[string]struct{}, len(cfg.Models))
	for _, m := range cfg.Models {
		configIDs[m.ID] = struct{}{}
	}

	// Convert discovered pricing to config.ModelConfig and store.Model slices.
	var newTrackerModels []config.ModelConfig
	var allStoreModels []store.Model
	now := time.Now().UTC().Format(time.RFC3339)

	// Start with config models (they take precedence).
	for _, m := range cfg.Models {
		allStoreModels = append(allStoreModels, store.Model{
			ID:                    m.ID,
			Name:                  m.Name,
			InputPricePerMillion:  m.InputPricePerMillion,
			OutputPricePerMillion: m.OutputPricePerMillion,
			Enabled:               m.Enabled,
			CreatedAt:             now,
		})
	}

	// Add discovered models that are not in config.
	for id, mp := range discovered {
		if _, inConfig := configIDs[id]; inConfig {
			continue
		}
		newTrackerModels = append(newTrackerModels, config.ModelConfig{
			ID:                    id,
			Name:                  mp.Name,
			InputPricePerMillion:  mp.InputPricePerMillion,
			OutputPricePerMillion: mp.OutputPricePerMillion,
			Enabled:               true,
		})
		allStoreModels = append(allStoreModels, store.Model{
			ID:                    id,
			Name:                  mp.Name,
			InputPricePerMillion:  mp.InputPricePerMillion,
			OutputPricePerMillion: mp.OutputPricePerMillion,
			Enabled:               true,
			CreatedAt:             now,
		})
	}

	s.UpdateModels(allStoreModels)
	tracker.UpdatePrices(newTrackerModels)

	slog.Info("fetched pricing for models",
		"discovered", len(discovered),
		"from_config", len(cfg.Models),
		"total", len(allStoreModels),
	)
}
