package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/relay"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "configs/config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Create relay instance
	relayInstance, err := relay.New(cfg)
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to create relay", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Start the relay
	if err := relayInstance.Start(ctx); err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to start relay", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
