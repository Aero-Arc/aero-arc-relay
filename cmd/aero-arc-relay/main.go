package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/makinje/aero-arc-relay/internal/config"
	"github.com/makinje/aero-arc-relay/internal/control"
	"github.com/makinje/aero-arc-relay/internal/relay"
	"github.com/makinje/aero-arc-relay/pkg/controlpb"
	"google.golang.org/grpc"
)

func main() {
	var configPath string
	var grpcAddr string

	flag.StringVar(&configPath, "config", "configs/config.yaml", "Path to configuration file")
	flag.StringVar(&grpcAddr, "grpc-listen", ":50051", "gRPC listen address")
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

	// Start gRPC server (control-plane + agent gateway) on a single server instance.
	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		slog.LogAttrs(context.Background(), slog.LevelError, "Failed to listen for gRPC", slog.String("error", err.Error()))
		os.Exit(1)
	}

	grpcServer := grpc.NewServer()

	// Stub AgentGateway implementation; can be expanded as the gateway API is defined.
	var agentGatewayImpl controlpb.AgentGatewayServer = struct{}{}
	relayControlImpl := control.NewRelayControlServer(cfg, relayInstance, time.Now().UTC())

	controlpb.RegisterAgentGatewayServer(grpcServer, agentGatewayImpl)
	controlpb.RegisterRelayControlServer(grpcServer, relayControlImpl)

	// Serve gRPC in a separate goroutine so it runs alongside the relay.
	go func() {
		slog.LogAttrs(context.Background(), slog.LevelInfo, "Starting gRPC server", slog.String("addr", grpcAddr))
		if err := grpcServer.Serve(listener); err != nil {
			slog.LogAttrs(context.Background(), slog.LevelError, "gRPC server exited with error", slog.String("error", err.Error()))
			cancel()
		}
	}()

	// Watch for OS signals and trigger a graceful shutdown.
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

	// When the context is cancelled, gracefully stop the gRPC server.
	<-ctx.Done()
	grpcServer.GracefulStop()
}
