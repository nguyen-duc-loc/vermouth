// Command notifications is the service that owns delivery: sending email,
// digest run records and alert delivery (spec 0001). It publishes nothing, so
// it owns no topic and runs no relay.
//
// The 6:00 digest scheduler belongs to feature 17, and STK-23 keeps this
// service at exactly one replica while that scheduler is an in process ticker.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/nguyen-duc-loc/vermouth/pkg/vermouth"
	"golang.org/x/sync/errgroup"

	"github.com/nguyen-duc-loc/vermouth/services/notifications/internal/consumer"
	notificationshttp "github.com/nguyen-duc-loc/vermouth/services/notifications/internal/http"
)

const service = "notifications"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

func run() error {
	// The empty publish topic says this service publishes nothing.
	cfg, err := vermouth.LoadConfig(service, "")
	if err != nil {
		return err
	}
	logger := vermouth.NewLogger(cfg.Service, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := vermouth.OpenPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	producer, err := vermouth.NewProducer(cfg.BrokerSeeds, cfg.Service)
	if err != nil {
		return err
	}
	defer producer.Close()

	health := vermouth.Health{
		Service: cfg.Service,
		Logger:  logger,
		Checks: map[string]vermouth.Check{
			"database": func(ctx context.Context) error { return pool.Ping(ctx) },
			"broker":   func(ctx context.Context) error { return producer.Ping(ctx) },
		},
	}

	mux := notificationshttp.Mux(notificationshttp.Deps{Pool: pool, Logger: logger, Health: health})

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return vermouth.Serve(groupCtx, logger, cfg.HTTPAddr, mux) })
	group.Go(func() error {
		return vermouth.RunConsumer(groupCtx, cfg, pool, logger, consumer.Recipients())
	})
	err = group.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
