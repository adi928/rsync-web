package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Load saved transfer settings (source, destination, SSH key) from settings.json
	if err := cfg.LoadTransferSettings(); err != nil {
		log.Warn().Err(err).Msg("could not load saved settings")
	}

	if cfg.TransferConfigured() {
		log.Info().Str("source", cfg.SourcePath).Msg("source configured")
		log.Info().Str("dest", cfg.RemoteHost+":"+cfg.RemotePath).Msg("destination configured")
	} else {
		log.Info().Msg("transfer settings not yet configured — use the web UI to set them")
	}
	log.Info().Str("schedule", cfg.Schedule).Msg("schedule configured")
	log.Info().Str("addr", cfg.ListenAddr).Msg("listen address configured")

	executor := NewBackupExecutor(cfg)

	scheduler, err := NewScheduler(executor, cfg.Schedule)
	if err != nil {
		log.Fatal().Err(err).Str("schedule", cfg.Schedule).Msg("invalid cron schedule")
	}
	scheduler.Start()

	srv := NewServer(cfg, executor, scheduler)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	httpServer := &http.Server{
		Addr:    cfg.ListenAddr,
		Handler: mux,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Info().Str("url", "http://localhost"+cfg.ListenAddr).Msg("dashboard available")
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("http server error")
		}
	}()

	<-done
	log.Info().Msg("shutting down...")

	scheduler.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("http shutdown error")
	}

	log.Info().Msg("stopped")
}
