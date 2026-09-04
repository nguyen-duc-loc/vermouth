// Command gateway is the only caller allowed to reach a service (INV-1).
//
// It owns no data: routing, token verification, request id creation, one error
// shape, and read only aggregation for screens (spec 0001, the service map).
// Routing and TLS at the cluster edge belong to feature 5.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"

	"github.com/nguyen-duc-loc/vermouth/gateway/internal/aggregate"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/ratelimit"
	"github.com/nguyen-duc-loc/vermouth/gateway/internal/route"
)

const service = "gateway"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := vermouth.LoadGatewayConfig(service)
	if err != nil {
		return err
	}
	logger := vermouth.NewLogger(cfg.Service, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	verifier, err := vermouth.NewVerifier(cfg.PublicKeys)
	if err != nil {
		return err
	}
	upstreams, err := route.UpstreamsFromEnv()
	if err != nil {
		return err
	}
	rateConfig, err := ratelimit.ConfigFromEnv()
	if err != nil {
		return err
	}

	handler := route.Mux(route.Deps{
		Client:        aggregate.NewClient(upstreams),
		Verifier:      verifier,
		Logger:        logger,
		Service:       cfg.Service,
		AuthRateGuard: ratelimit.NewGuard(rateConfig, logger, nil),
	})

	logger.Info("Starting", slog.String("identity", upstreams.Identity))
	err = vermouth.Serve(ctx, logger, cfg.HTTPAddr, handler)
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
