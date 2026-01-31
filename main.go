package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	log.Printf("source:   %s", cfg.SourcePath)
	log.Printf("dest:     %s:%s", cfg.RemoteHost, cfg.RemotePath)
	log.Printf("schedule: %s", cfg.Schedule)
	log.Printf("listen:   %s", cfg.ListenAddr)

	executor := NewBackupExecutor(cfg)

	scheduler, err := NewScheduler(executor, cfg.Schedule)
	if err != nil {
		log.Fatalf("invalid cron schedule %q: %v", cfg.Schedule, err)
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
		log.Printf("dashboard available at http://localhost%s", cfg.ListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-done
	log.Println("shutting down...")

	scheduler.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}

	log.Println("stopped")
}
