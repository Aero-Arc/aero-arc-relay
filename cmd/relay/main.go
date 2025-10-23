package main

import (
	"context"
	"flag"
	"fmt"
	"log"

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
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create and start the relay
	r, err := relay.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create relay: %v", err)
	}

	// Start the relay
	if err := r.Start(context.Background()); err != nil {
		log.Fatalf("Failed to start relay: %v", err)
	}

	fmt.Println("Aero Arc Relay started successfully")

	// Wait for shutdown signal
	select {}
}
