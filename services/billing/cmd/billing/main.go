// Command billing is the service that owns money: dated rate history, the
// billable session projection, the invoice profile and bank details, invoices,
// invoice lines, invoice numbers, voids, paid state and the rendered invoice
// PDF (spec 0001).
//
// Its consumers arrive with feature 4, which decides the tables they write.
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

	billinghttp "github.com/nguyen-duc-loc/vermouth/services/billing/internal/http"
)

const service = "billing"

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := vermouth.LoadConfig(service, vermouth.TopicBilling)
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

	err = vermouth.EnsureTopics(ctx, cfg.BrokerSeeds, cfg.TopicPartitions, cfg.PublishTopic)
	if err != nil {
		return err
	}

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
	mux := billinghttp.Mux(billinghttp.Deps{Logger: logger, Health: health})

	relay := vermouth.NewRelay(pool, producer, logger, cfg.RelayInterval)

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error { return vermouth.Serve(groupCtx, logger, cfg.HTTPAddr, mux) })
	group.Go(func() error { return relay.Run(groupCtx) })
	err = group.Wait()
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}
