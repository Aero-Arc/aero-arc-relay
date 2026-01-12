package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/relay"
	"github.com/urfave/cli/v3"
)

var relayCommand = cli.Command{
	Usage:  "run the relay process",
	Action: RunRelay,

	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "config-path",
			Usage: "path to the configuration file",
			Value: "configs/config.yaml",
		},
		&cli.StringFlag{
			Name:  "tls-cert-path",
			Usage: "path to tls cert file",
			Value: fmt.Sprintf("~/%s", relay.DebugTLSCertPath),
		},
		&cli.StringFlag{
			Name:  "tls-key-path",
			Usage: "path to tls key file",
			Value: fmt.Sprintf("~/%s", relay.DebugTLSKeyPath),
		},
		&cli.BoolFlag{
			Name:  "debug",
			Usage: "run the relay in debug mode. Useful for local testing",
			Value: false,
		},
	},
}

func RunRelay(ctx context.Context, cmd *cli.Command) error {
	// Load configuration
	cfg, err := config.Load(cmd.String("config-path"))
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to load configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}

	cfg.Debug = cmd.Bool("debug")
	cfg.TLSCertPath = cmd.String("tls-cert-path")
	cfg.TLSKeyPath = cmd.String("tls-key-path")

	// Create relay instance
	relayInstance, err := relay.New(cfg)
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to create relay", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
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

	return nil
}

func main() {
	if err := relayCommand.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}
